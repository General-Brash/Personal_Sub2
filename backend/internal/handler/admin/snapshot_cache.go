package admin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	snapshotCacheMaxEntries      = 256
	snapshotCacheCleanupInterval = 5 * time.Second
	snapshotCacheLoadTimeout     = 30 * time.Second
)

type snapshotCacheEntry struct {
	ETag      string
	Payload   any
	ExpiresAt time.Time
}

type snapshotCache struct {
	mu          sync.Mutex
	ttl         time.Duration
	items       map[string]snapshotCacheEntry
	nextCleanup time.Time
	sf          singleflight.Group
}

type snapshotCacheLoadResult struct {
	Entry snapshotCacheEntry
	Hit   bool
}

func newSnapshotCache(ttl time.Duration) *snapshotCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &snapshotCache{
		ttl:   ttl,
		items: make(map[string]snapshotCacheEntry),
	}
}

func (c *snapshotCache) Get(key string) (snapshotCacheEntry, bool) {
	if c == nil || key == "" {
		return snapshotCacheEntry{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	c.cleanupExpiredLocked(now, false)
	entry, ok := c.items[key]
	if !ok {
		return snapshotCacheEntry{}, false
	}
	if !now.Before(entry.ExpiresAt) {
		delete(c.items, key)
		return snapshotCacheEntry{}, false
	}
	return entry, true
}

func (c *snapshotCache) Set(key string, payload any) snapshotCacheEntry {
	return c.setContext(context.Background(), key, payload)
}

func (c *snapshotCache) setContext(ctx context.Context, key string, payload any) snapshotCacheEntry {
	if c == nil {
		return snapshotCacheEntry{}
	}
	entry := snapshotCacheEntry{
		ETag:      buildETagFromAny(payload),
		Payload:   payload,
		ExpiresAt: time.Now().Add(c.ttl),
	}
	if key == "" || entry.ETag == "" {
		return entry
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if ctx.Err() != nil {
		return entry
	}
	c.cleanupExpiredLocked(time.Now(), len(c.items) >= snapshotCacheMaxEntries)
	if _, exists := c.items[key]; !exists && len(c.items) >= snapshotCacheMaxEntries {
		var oldestKey string
		var oldestExpiry time.Time
		for itemKey, item := range c.items {
			if oldestExpiry.IsZero() || item.ExpiresAt.Before(oldestExpiry) {
				oldestKey, oldestExpiry = itemKey, item.ExpiresAt
			}
		}
		delete(c.items, oldestKey)
	}
	c.items[key] = entry
	return entry
}

// Operation-driven sweeps keep short-lived cache instances free of background goroutines.
func (c *snapshotCache) cleanupExpiredLocked(now time.Time, force bool) {
	if !force && now.Before(c.nextCleanup) {
		return
	}
	for key, entry := range c.items {
		if !now.Before(entry.ExpiresAt) {
			delete(c.items, key)
		}
	}
	c.nextCleanup = now.Add(snapshotCacheCleanupInterval)
}

func (c *snapshotCache) GetOrLoad(key string, load func() (any, error)) (snapshotCacheEntry, bool, error) {
	if load == nil {
		return snapshotCacheEntry{}, false, nil
	}
	return c.GetOrLoadContext(context.Background(), key, func(context.Context) (any, error) {
		return load()
	})
}

func (c *snapshotCache) GetOrLoadContext(ctx context.Context, key string, load func(context.Context) (any, error)) (snapshotCacheEntry, bool, error) {
	if load == nil {
		return snapshotCacheEntry{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return snapshotCacheEntry{}, false, err
	}
	if entry, ok := c.Get(key); ok {
		return entry, true, nil
	}

	var uncached singleflight.Group
	group := &uncached
	if c != nil && key != "" {
		group = &c.sf
	}
	// The shared loader must not retain the first waiter's request context or values.
	resultCh := group.DoChan(key, func() (any, error) {
		if entry, ok := c.Get(key); ok {
			return snapshotCacheLoadResult{Entry: entry, Hit: true}, nil
		}
		loadCtx, cancel := context.WithTimeout(context.Background(), snapshotCacheLoadTimeout)
		defer cancel()
		payload, err := load(loadCtx)
		if err != nil {
			return nil, err
		}
		if err := loadCtx.Err(); err != nil {
			return nil, err
		}
		entry := snapshotCacheEntry{Payload: payload}
		if c != nil {
			entry = c.setContext(loadCtx, key, payload)
		}
		if err := loadCtx.Err(); err != nil {
			return nil, err
		}
		return snapshotCacheLoadResult{Entry: entry, Hit: false}, nil
	})
	select {
	case <-ctx.Done():
		return snapshotCacheEntry{}, false, ctx.Err()
	case value := <-resultCh:
		if err := ctx.Err(); err != nil {
			return snapshotCacheEntry{}, false, err
		}
		if value.Err != nil {
			return snapshotCacheEntry{}, false, value.Err
		}
		result, ok := value.Val.(snapshotCacheLoadResult)
		if !ok {
			return snapshotCacheEntry{}, false, nil
		}
		return result.Entry, result.Hit, nil
	}
}

func buildETagFromAny(payload any) string {
	raw, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "\"" + hex.EncodeToString(sum[:]) + "\""
}

func parseBoolQueryWithDefault(raw string, def bool) bool {
	value := strings.TrimSpace(strings.ToLower(raw))
	if value == "" {
		return def
	}
	switch value {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}
