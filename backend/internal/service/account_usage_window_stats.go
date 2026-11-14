package service

import (
	"container/list"
	"context"
	"fmt"
	"sync"
	"time"
)

const (
	openAIWindowStatsTTL       = 30 * time.Second
	openAIWindowStatsBucket    = 30 * time.Second
	openAIWindowStatsTimeout   = 15 * time.Second
	openAIWindowStatsCapacity  = 4096
	openAIWindowStatsLoadLimit = 6
)

type openAIWindowStatsKey struct {
	accountID int64
	start     time.Time
	window    time.Duration
}

type openAIWindowStatsEntry struct {
	key       openAIWindowStatsKey
	stats     WindowStats
	expiresAt time.Time
}

// Flight results and cache entries contain values, never caller-owned progress pointers.
type openAIWindowStatsFlight struct {
	done  chan struct{}
	stats WindowStats
	err   error
}

type openAIWindowStatsCache struct {
	mu          sync.Mutex
	entries     map[openAIWindowStatsKey]*list.Element
	lru         list.List
	flights     map[openAIWindowStatsKey]*openAIWindowStatsFlight
	changed     chan struct{}
	loadSlots   chan struct{}
	nextCleanup time.Time
}

type openAIWindowStatsReservation struct {
	ready    map[openAIWindowStatsKey]WindowStats
	waiting  map[openAIWindowStatsKey]*openAIWindowStatsFlight
	owned    []openAIWindowStatsKey
	deferred []openAIWindowStatsKey
	changed  <-chan struct{}
}

func (s *AccountUsageService) openAIStatsCache() *openAIWindowStatsCache {
	if s.cache != nil {
		return &s.cache.openAIWindowStats
	}
	return &s.openAIWindowStatsFallback
}

// reserve coalesces each account/start/window independently, so overlapping batches
// can share flights even when their complete account sets differ.
func (c *openAIWindowStatsCache) reserve(ctx context.Context, keys []openAIWindowStatsKey) (*openAIWindowStatsReservation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.entries == nil {
		c.entries = make(map[openAIWindowStatsKey]*list.Element)
		c.flights = make(map[openAIWindowStatsKey]*openAIWindowStatsFlight)
		c.changed = make(chan struct{})
		c.loadSlots = make(chan struct{}, openAIWindowStatsLoadLimit)
	}
	now := time.Now()
	if !now.Before(c.nextCleanup) {
		for key, element := range c.entries {
			entry, ok := element.Value.(openAIWindowStatsEntry)
			if !ok || !now.Before(entry.expiresAt) {
				c.lru.Remove(element)
				delete(c.entries, key)
			}
		}
		c.nextCleanup = now.Add(openAIWindowStatsTTL)
	}

	reservation := &openAIWindowStatsReservation{
		ready:   make(map[openAIWindowStatsKey]WindowStats),
		waiting: make(map[openAIWindowStatsKey]*openAIWindowStatsFlight),
		changed: c.changed,
	}
	for _, key := range keys {
		if element := c.entries[key]; element != nil {
			entry, ok := element.Value.(openAIWindowStatsEntry)
			if ok && now.Before(entry.expiresAt) {
				c.lru.MoveToFront(element)
				reservation.ready[key] = entry.stats
				continue
			}
			c.lru.Remove(element)
			delete(c.entries, key)
		}
		if flight := c.flights[key]; flight != nil {
			reservation.waiting[key] = flight
			continue
		}
		if len(c.flights) >= openAIWindowStatsCapacity {
			reservation.deferred = append(reservation.deferred, key)
			continue
		}
		flight := &openAIWindowStatsFlight{done: make(chan struct{})}
		c.flights[key] = flight
		reservation.waiting[key] = flight
		reservation.owned = append(reservation.owned, key)
	}
	return reservation, nil
}

func (c *openAIWindowStatsCache) complete(ctx context.Context, keys []openAIWindowStatsKey, statsByAccount map[int64]*WindowStats, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil {
		err = ctx.Err()
	}
	now := time.Now()
	for _, key := range keys {
		flight := c.flights[key]
		if flight == nil {
			continue
		}
		flight.err = err
		if err == nil {
			if stats := statsByAccount[key.accountID]; stats != nil {
				flight.stats = *stats
			}
			for len(c.entries) >= openAIWindowStatsCapacity {
				oldest := c.lru.Back()
				entry, _ := oldest.Value.(openAIWindowStatsEntry)
				delete(c.entries, entry.key)
				c.lru.Remove(oldest)
			}
			entry := openAIWindowStatsEntry{key: key, stats: flight.stats, expiresAt: now.Add(openAIWindowStatsTTL)}
			c.entries[key] = c.lru.PushFront(entry)
		}
		delete(c.flights, key)
		close(flight.done)
	}
	close(c.changed)
	c.changed = make(chan struct{})
}

func (s *AccountUsageService) loadOpenAIWindowStatsGroups(cache *openAIWindowStatsCache, groups map[time.Time][]openAIWindowStatsKey) {
	if len(groups) == 0 {
		return
	}
	// Never inherit the first waiter's deadline or cancellation for a shared load.
	ctx, cancel := context.WithTimeout(context.Background(), openAIWindowStatsTimeout)
	queue := make(chan []openAIWindowStatsKey, len(groups))
	for _, keys := range groups {
		queue <- keys
	}
	close(queue)
	go func() {
		defer cancel()
		var workers sync.WaitGroup
		for range min(openAIWindowStatsLoadLimit, len(groups)) {
			workers.Add(1)
			go func() {
				defer workers.Done()
				for keys := range queue {
					s.loadOpenAIWindowStatsGroup(ctx, cache, keys)
				}
			}()
		}
		workers.Wait()
	}()
}

func (s *AccountUsageService) loadOpenAIWindowStatsGroup(ctx context.Context, cache *openAIWindowStatsCache, keys []openAIWindowStatsKey) {
	if err := ctx.Err(); err != nil {
		cache.complete(ctx, keys, nil, err)
		return
	}
	select {
	case cache.loadSlots <- struct{}{}:
		defer func() { <-cache.loadSlots }()
	case <-ctx.Done():
		cache.complete(ctx, keys, nil, ctx.Err())
		return
	}
	if err := ctx.Err(); err != nil {
		cache.complete(ctx, keys, nil, err)
		return
	}

	ids := make([]int64, 0, len(keys))
	keysByAccount := make(map[int64][]openAIWindowStatsKey, len(keys))
	for _, key := range keys {
		if len(keysByAccount[key.accountID]) == 0 {
			ids = append(ids, key.accountID)
		}
		keysByAccount[key.accountID] = append(keysByAccount[key.accountID], key)
	}
	start := keys[0].start
	result := make(map[int64]*WindowStats, len(ids))
	if batchReader, ok := s.usageLogRepo.(accountWindowStatsBatchReader); ok {
		if err := ctx.Err(); err != nil {
			cache.complete(ctx, keys, nil, err)
			return
		}
		stats, err := batchReader.GetAccountWindowStatsBatch(ctx, ids, start)
		if ctxErr := ctx.Err(); ctxErr != nil {
			err = ctxErr
		}
		if err != nil {
			cache.complete(ctx, keys, nil, fmt.Errorf("get openai window stats batch starting %s failed: %w", start.Format(time.RFC3339Nano), err))
			return
		}
		for _, id := range ids {
			result[id] = windowStatsFromAccountStats(stats[id])
		}
		cache.complete(ctx, keys, result, nil)
		return
	}

	// Compatibility repositories have no batch reader. Keep individual failures
	// local to their account, and stop issuing SQL when the shared timeout expires.
	for _, id := range ids {
		var stats *WindowStats
		err := ctx.Err()
		if err == nil {
			accountStats, queryErr := s.usageLogRepo.GetAccountWindowStats(ctx, id, start)
			err = queryErr
			if ctxErr := ctx.Err(); ctxErr != nil {
				err = ctxErr
			}
			if err == nil {
				stats = windowStatsFromAccountStats(accountStats)
			}
		}
		if err != nil {
			err = fmt.Errorf("get openai window stats for account %d starting %s failed: %w", id, start.Format(time.RFC3339Nano), err)
		}
		cache.complete(ctx, keysByAccount[id], map[int64]*WindowStats{id: stats}, err)
	}
}

func (s *AccountUsageService) getOpenAIWindowStats(ctx context.Context, keys []openAIWindowStatsKey) (
	map[openAIWindowStatsKey]WindowStats, map[openAIWindowStatsKey]error, error,
) {
	result := make(map[openAIWindowStatsKey]WindowStats, len(keys))
	errorsByKey := make(map[openAIWindowStatsKey]error)
	cache := s.openAIStatsCache()
	for len(keys) > 0 {
		reservation, err := cache.reserve(ctx, keys)
		if err != nil {
			return nil, nil, err
		}
		groups := make(map[time.Time][]openAIWindowStatsKey)
		for _, key := range reservation.owned {
			groups[key.start] = append(groups[key.start], key)
		}
		if err := ctx.Err(); err != nil {
			cache.complete(ctx, reservation.owned, nil, err)
			return nil, nil, err
		}
		s.loadOpenAIWindowStatsGroups(cache, groups)
		for key, stats := range reservation.ready {
			result[key] = stats
		}
		for key, flight := range reservation.waiting {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-flight.done:
				if flight.err != nil {
					errorsByKey[key] = flight.err
				} else {
					result[key] = flight.stats
				}
			}
		}
		if len(reservation.deferred) > 0 && len(reservation.waiting) == 0 {
			// Capacity pressure waits for a flight to finish instead of returning
			// an artificial account error or allocating an unbounded registry.
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-reservation.changed:
			}
		}
		keys = reservation.deferred
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	return result, errorsByKey, nil
}

func (s *AccountUsageService) fillOpenAIWindowStats(ctx context.Context, usageByAccount map[int64]*UsageInfo, now time.Time) (map[int64]error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	errorsByAccount := make(map[int64]error)
	if s.usageLogRepo == nil || len(usageByAccount) == 0 {
		return errorsByAccount, nil
	}
	keys := make([]openAIWindowStatsKey, 0, 2*len(usageByAccount))
	for id, usage := range usageByAccount {
		if usage == nil {
			errorsByAccount[id] = fmt.Errorf("openai usage is missing for account %d", id)
			continue
		}
		// UTC/Round(0) only canonicalizes time identity (including monotonic data);
		// it does not round reset-backed query boundaries.
		keys = append(keys,
			openAIWindowStatsKey{accountID: id, start: codexWindowStatsStart(usage.FiveHour, 5*time.Hour, now).UTC().Round(0), window: 5 * time.Hour},
			openAIWindowStatsKey{accountID: id, start: codexWindowStatsStart(usage.SevenDay, 7*24*time.Hour, now).UTC().Round(0), window: 7 * 24 * time.Hour},
		)
	}
	stats, errorsByKey, err := s.getOpenAIWindowStats(ctx, keys)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		if windowErr := errorsByKey[key]; windowErr != nil {
			if errorsByAccount[key.accountID] == nil {
				errorsByAccount[key.accountID] = fmt.Errorf("openai %s window stats: %w", key.window, windowErr)
			}
		}
	}
	for _, key := range keys {
		if errorsByAccount[key.accountID] != nil {
			continue
		}
		usage := usageByAccount[key.accountID]
		progress := &usage.FiveHour
		if key.window == 7*24*time.Hour {
			progress = &usage.SevenDay
		}
		if *progress == nil {
			*progress = &UsageProgress{}
		}
		statsCopy := stats[key]
		(*progress).WindowStats = &statsCopy
	}
	return errorsByAccount, nil
}
