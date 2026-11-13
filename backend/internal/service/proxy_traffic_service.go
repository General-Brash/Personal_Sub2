package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/proxytraffic"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
)

type ProxyTrafficSnapshot struct {
	ProxyID       int64
	UsageDate     string
	UploadBytes   int64
	DownloadBytes int64
}

type ProxyTrafficUsage struct {
	TodayBytes  int64
	Days30Bytes int64
}

type ProxyTrafficRepository interface {
	SaveSnapshots(ctx context.Context, sourceID string, snapshots []ProxyTrafficSnapshot) error
	GetUsage(ctx context.Context, proxyIDs []int64, today, since string) (map[int64]ProxyTrafficUsage, error)
}

type proxyTrafficAccountResolver interface {
	ResolveAccountProxy(ctx context.Context, accountID int64) (*Proxy, error)
}

type proxyTrafficKey struct {
	proxyID int64
	date    string
}

type proxyTrafficCounter struct {
	upload        int64
	download      int64
	savedUpload   int64
	savedDownload int64
}

type ProxyTrafficService struct {
	repo      ProxyTrafficRepository
	proxyRepo ProxyRepository
	sourceID  string
	now       func() time.Time

	mu         sync.Mutex
	routes     map[string]int64
	counters   map[proxyTrafficKey]*proxyTrafficCounter
	unresolved map[string]map[string]*proxyTrafficCounter
	refreshMu  sync.Mutex
	flushMu    sync.Mutex
	startOnce  sync.Once
	stopOnce   sync.Once
	cancel     context.CancelFunc
	done       chan struct{}
	unregister func()
}

func NewProxyTrafficService(repo ProxyTrafficRepository, proxyRepo ProxyRepository) *ProxyTrafficService {
	return &ProxyTrafficService{
		repo: repo, proxyRepo: proxyRepo, sourceID: uuid.NewString(), now: time.Now,
		routes: make(map[string]int64), counters: make(map[proxyTrafficKey]*proxyTrafficCounter),
		unresolved: make(map[string]map[string]*proxyTrafficCounter),
	}
}

func (s *ProxyTrafficService) Start() {
	s.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.done = make(chan struct{})
		if err := s.RefreshRoutes(ctx); err != nil {
			logger.LegacyPrintf("service.proxy_traffic", "Load proxy routes failed: %v", err)
		}
		s.unregister = proxytraffic.Register(s.ObserverForContext)
		go func() {
			defer close(s.done)
			flushTicker := time.NewTicker(5 * time.Second)
			refreshTicker := time.NewTicker(30 * time.Second)
			defer flushTicker.Stop()
			defer refreshTicker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-flushTicker.C:
					flushCtx, stop := context.WithTimeout(ctx, 3*time.Second)
					if err := s.Flush(flushCtx); err != nil {
						logger.LegacyPrintf("service.proxy_traffic", "Save proxy traffic failed: %v", err)
					}
					stop()
				case <-refreshTicker.C:
					if err := s.RefreshRoutes(ctx); err != nil {
						logger.LegacyPrintf("service.proxy_traffic", "Refresh proxy routes failed: %v", err)
					}
				}
			}
		}()
	})
}

func (s *ProxyTrafficService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
			<-s.done
		}
		if s.unregister != nil {
			s.unregister()
		}
		proxytraffic.CloseConnections()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Flush(ctx); err != nil {
			logger.LegacyPrintf("service.proxy_traffic", "Final proxy traffic save failed: %v", err)
		}
	})
}

func (s *ProxyTrafficService) RefreshRoutes(ctx context.Context) error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	return s.refreshRoutes(ctx)
}

func (s *ProxyTrafficService) refreshRoutes(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	proxies, err := s.proxyRepo.ListAllForFallback(ctx)
	if err != nil {
		return err
	}
	routes := make(map[string]int64, len(proxies))
	for i := range proxies {
		key := proxytraffic.Key(proxies[i].URL())
		if key == "" {
			continue
		}
		if _, exists := routes[key]; !exists {
			routes[key] = proxies[i].ID
		} else {
			routes[key] = -1
		}
	}
	s.mu.Lock()
	s.routes = routes
	cutoff := s.now().In(timezone.Location()).AddDate(0, 0, -29).Format(time.DateOnly)
	for route, days := range s.unresolved {
		for date := range days {
			if date < cutoff {
				delete(days, date)
			}
		}
		if len(days) == 0 {
			delete(s.unresolved, route)
			continue
		}
		proxyID := routes[route]
		if proxyID <= 0 {
			continue
		}
		for date, pending := range days {
			key := proxyTrafficKey{proxyID: proxyID, date: date}
			counter := s.counters[key]
			if counter == nil {
				counter = &proxyTrafficCounter{}
				s.counters[key] = counter
			}
			counter.upload += pending.upload
			counter.download += pending.download
		}
		delete(s.unresolved, route)
	}
	s.mu.Unlock()
	return nil
}

func (s *ProxyTrafficService) ObserverForProxyURL(raw string) proxytraffic.Observer {
	return s.ObserverForContext(context.Background(), raw)
}

func (s *ProxyTrafficService) IsAmbiguousRoute(raw string) bool {
	key := proxytraffic.Key(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.routes[key] == -1
}

func (s *ProxyTrafficService) BindAccount(ctx context.Context, raw string, accountID int64) (context.Context, int64) {
	key := proxytraffic.Key(raw)
	ctx = proxytraffic.WithAccountID(ctx, accountID)
	if key == "" {
		return ctx, 0
	}
	s.mu.Lock()
	proxyID := s.routes[key]
	s.mu.Unlock()
	if proxyID == -1 && accountID > 0 {
		if resolver, ok := s.repo.(proxyTrafficAccountResolver); ok {
			resolveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			proxy, err := resolver.ResolveAccountProxy(resolveCtx, accountID)
			cancel()
			if err == nil && proxy != nil && proxytraffic.Key(proxy.URL()) == key {
				proxyID = proxy.ID
			}
		}
	}
	if proxyID > 0 {
		ctx = proxytraffic.WithProxyID(ctx, proxyID, raw)
		return ctx, proxyID
	}
	return ctx, 0
}

func (s *ProxyTrafficService) ObserverForContext(ctx context.Context, raw string) proxytraffic.Observer {
	key := proxytraffic.Key(raw)
	if key == "" {
		return nil
	}
	if proxyID := proxytraffic.ProxyID(ctx, raw); proxyID > 0 {
		return func(upload, download int64) { s.record(proxyID, upload, download) }
	}
	s.mu.Lock()
	proxyID := s.routes[key]
	s.mu.Unlock()
	if proxyID <= 0 && proxytraffic.AccountID(ctx) > 0 {
		if resolver, ok := s.repo.(proxyTrafficAccountResolver); ok {
			resolveCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			proxy, err := resolver.ResolveAccountProxy(resolveCtx, proxytraffic.AccountID(ctx))
			cancel()
			if err == nil && proxy != nil && proxytraffic.Key(proxy.URL()) == key {
				proxyID = proxy.ID
			}
		}
	}
	if proxyID <= 0 {
		return func(upload, download int64) { s.recordUnresolved(key, upload, download) }
	}
	return func(upload, download int64) { s.record(proxyID, upload, download) }
}

func (s *ProxyTrafficService) recordUnresolved(route string, upload, download int64) {
	date := s.now().In(timezone.Location()).Format(time.DateOnly)
	s.mu.Lock()
	proxyID := s.routes[route]
	if proxyID > 0 {
		s.mu.Unlock()
		s.record(proxyID, upload, download)
		return
	}
	days := s.unresolved[route]
	if days == nil {
		if len(s.unresolved) >= 5000 {
			s.mu.Unlock()
			return
		}
		days = make(map[string]*proxyTrafficCounter)
		s.unresolved[route] = days
	}
	counter := days[date]
	if counter == nil {
		counter = &proxyTrafficCounter{}
		days[date] = counter
	}
	if upload > 0 {
		counter.upload += upload
	}
	if download > 0 {
		counter.download += download
	}
	s.mu.Unlock()
}

func (s *ProxyTrafficService) record(proxyID, upload, download int64) {
	if proxyID <= 0 || (upload <= 0 && download <= 0) {
		return
	}
	key := proxyTrafficKey{proxyID: proxyID, date: s.now().In(timezone.Location()).Format(time.DateOnly)}
	s.mu.Lock()
	defer s.mu.Unlock()
	counter := s.counters[key]
	if counter == nil {
		counter = &proxyTrafficCounter{}
		s.counters[key] = counter
	}
	if upload > 0 {
		counter.upload += upload
	}
	if download > 0 {
		counter.download += download
	}
}

// Flush writes absolute per-process totals; retrying an uncertain commit is idempotent.
func (s *ProxyTrafficService) Flush(ctx context.Context) error {
	s.flushMu.Lock()
	defer s.flushMu.Unlock()
	s.mu.Lock()
	snapshots := make([]ProxyTrafficSnapshot, 0, len(s.counters))
	for key, counter := range s.counters {
		if counter.upload != counter.savedUpload || counter.download != counter.savedDownload {
			snapshots = append(snapshots, ProxyTrafficSnapshot{
				ProxyID: key.proxyID, UsageDate: key.date,
				UploadBytes: counter.upload, DownloadBytes: counter.download,
			})
		}
	}
	s.mu.Unlock()
	if len(snapshots) == 0 {
		return nil
	}
	sort.Slice(snapshots, func(i, j int) bool {
		if snapshots[i].ProxyID == snapshots[j].ProxyID {
			return snapshots[i].UsageDate < snapshots[j].UsageDate
		}
		return snapshots[i].ProxyID < snapshots[j].ProxyID
	})
	if err := s.repo.SaveSnapshots(ctx, s.sourceID, snapshots); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, snapshot := range snapshots {
		counter := s.counters[proxyTrafficKey{proxyID: snapshot.ProxyID, date: snapshot.UsageDate}]
		counter.savedUpload = snapshot.UploadBytes
		counter.savedDownload = snapshot.DownloadBytes
	}
	cutoff := s.now().In(timezone.Location()).AddDate(0, 0, -30).Format(time.DateOnly)
	for key, counter := range s.counters {
		if key.date < cutoff && counter.upload == counter.savedUpload && counter.download == counter.savedDownload {
			delete(s.counters, key)
		}
	}
	return nil
}

func (s *ProxyTrafficService) GetUsage(ctx context.Context, proxyIDs []int64) (map[int64]ProxyTrafficUsage, error) {
	if len(proxyIDs) == 0 {
		return map[int64]ProxyTrafficUsage{}, nil
	}
	if err := s.Flush(ctx); err != nil {
		return nil, err
	}
	today := timezone.StartOfDay(s.now())
	return s.repo.GetUsage(ctx, proxyIDs, today.Format(time.DateOnly), today.AddDate(0, 0, -29).Format(time.DateOnly))
}
