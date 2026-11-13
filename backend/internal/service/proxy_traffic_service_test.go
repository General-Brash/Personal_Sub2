package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxytraffic"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type trafficFixtureKey struct {
	source, date string
	proxyID      int64
}

type trafficRepoFixture struct {
	mu              sync.Mutex
	rows            map[trafficFixtureKey]ProxyTrafficSnapshot
	saves           int
	failAfterCommit bool
	onSave          func()
	accountProxies  map[int64]*Proxy
}

func (r *trafficRepoFixture) ResolveAccountProxy(_ context.Context, accountID int64) (*Proxy, error) {
	return r.accountProxies[accountID], nil
}

func (r *trafficRepoFixture) SaveSnapshots(_ context.Context, source string, snapshots []ProxyTrafficSnapshot) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.rows == nil {
		r.rows = make(map[trafficFixtureKey]ProxyTrafficSnapshot)
	}
	r.saves++
	for _, snapshot := range snapshots {
		key := trafficFixtureKey{source: source, date: snapshot.UsageDate, proxyID: snapshot.ProxyID}
		previous := r.rows[key]
		if snapshot.UploadBytes < previous.UploadBytes {
			snapshot.UploadBytes = previous.UploadBytes
		}
		if snapshot.DownloadBytes < previous.DownloadBytes {
			snapshot.DownloadBytes = previous.DownloadBytes
		}
		r.rows[key] = snapshot
	}
	if r.onSave != nil {
		r.onSave()
	}
	if r.failAfterCommit {
		r.failAfterCommit = false
		return errors.New("commit acknowledged late")
	}
	return nil
}

func (r *trafficRepoFixture) GetUsage(_ context.Context, ids []int64, today, since string) (map[int64]ProxyTrafficUsage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[int64]ProxyTrafficUsage)
	for _, id := range ids {
		var usage ProxyTrafficUsage
		for key, row := range r.rows {
			if key.proxyID != id || key.date < since || key.date > today {
				continue
			}
			bytes := row.UploadBytes + row.DownloadBytes
			usage.Days30Bytes += bytes
			if key.date == today {
				usage.TodayBytes += bytes
			}
		}
		result[id] = usage
	}
	return result, nil
}

type trafficProxyRepoFixture struct {
	ProxyRepository
	proxies []Proxy
}

func (r *trafficProxyRepoFixture) ListAllForFallback(context.Context) ([]Proxy, error) {
	return r.proxies, nil
}

func trafficTestService(repo *trafficRepoFixture, now time.Time) *ProxyTrafficService {
	s := NewProxyTrafficService(repo, nil)
	s.now = func() time.Time { return now }
	return s
}

func TestProxyTrafficDailyBoundariesAndThirtyDayWindow(t *testing.T) {
	ctx := context.Background()
	repo := &trafficRepoFixture{}
	now := time.Date(2026, 11, 13, 0, 0, 0, 0, timezone.Location())
	s := trafficTestService(repo, now)
	for days, bytes := range map[int]int64{-30: 500, -29: 100, -1: 20, 0: 10, 1: 1000} {
		require.NoError(t, repo.SaveSnapshots(ctx, "older-process", []ProxyTrafficSnapshot{{
			ProxyID: 7, UsageDate: now.AddDate(0, 0, days).Format(time.DateOnly), UploadBytes: bytes,
		}}))
	}
	s.record(7, 5, 10)
	s.record(8, 2, 3)
	usage, err := s.GetUsage(ctx, []int64{7, 8, 9})
	require.NoError(t, err)
	require.Equal(t, ProxyTrafficUsage{TodayBytes: 25, Days30Bytes: 145}, usage[7])
	require.Equal(t, ProxyTrafficUsage{TodayBytes: 5, Days30Bytes: 5}, usage[8])
	require.Zero(t, usage[9])

	s.now = func() time.Time { return now.Add(-time.Nanosecond) }
	s.record(7, 1, 2)
	s.now = func() time.Time { return now }
	usage, err = s.GetUsage(ctx, []int64{7})
	require.NoError(t, err)
	require.Equal(t, int64(25), usage[7].TodayBytes)
	require.Equal(t, int64(148), usage[7].Days30Bytes)
}

func TestProxyTrafficUncertainCommitRetryAndMultipleProcesses(t *testing.T) {
	ctx := context.Background()
	repo := &trafficRepoFixture{failAfterCommit: true}
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, timezone.Location())
	s := trafficTestService(repo, now)
	s.record(1, 10, 20)
	require.Error(t, s.Flush(ctx))
	s.record(1, 5, 5)
	require.NoError(t, s.Flush(ctx))
	saves := repo.saves
	require.NoError(t, s.Flush(ctx))
	require.Equal(t, saves, repo.saves)
	second := trafficTestService(repo, now)
	require.NotEqual(t, s.sourceID, second.sourceID)
	second.record(1, 1, 2)
	require.NoError(t, second.Flush(ctx))
	usage, err := s.GetUsage(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, int64(43), usage[1].TodayBytes)
}

func TestProxyTrafficConcurrentWritesAndWritesDuringFlush(t *testing.T) {
	repo := &trafficRepoFixture{}
	s := trafficTestService(repo, time.Now())
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				s.record(1, 1, 2)
			}
		}()
	}
	wg.Wait()
	repo.onSave = func() { s.record(1, 3, 4) }
	require.NoError(t, s.Flush(context.Background()))
	repo.onSave = nil
	usage, err := s.GetUsage(context.Background(), []int64{1})
	require.NoError(t, err)
	require.Equal(t, int64(15007), usage[1].TodayBytes)
}

func TestProxyTrafficRouteMappingRefreshAndStableConnectionAttribution(t *testing.T) {
	proxy := Proxy{ID: 7, Protocol: "socks5", Host: "NODE.example", Port: 1080}
	proxyRepo := &trafficProxyRepoFixture{proxies: []Proxy{proxy}}
	s := NewProxyTrafficService(&trafficRepoFixture{}, proxyRepo)
	require.NoError(t, s.RefreshRoutes(context.Background()))
	observer := s.ObserverForProxyURL("socks5h://node.example:1080")
	require.NotNil(t, observer)
	require.Nil(t, s.ObserverForProxyURL(""))
	require.Nil(t, s.ObserverForProxyURL("invalid"))
	proxyRepo.proxies = []Proxy{{ID: 8, Protocol: "socks5h", Host: "node.example", Port: 1080}}
	require.NoError(t, s.RefreshRoutes(context.Background()))
	observer(10, 20)
	s.ObserverForProxyURL(proxytraffic.Key(proxy.URL()))(1, 2)
	usage, err := s.GetUsage(context.Background(), []int64{7, 8})
	require.NoError(t, err)
	require.Equal(t, int64(30), usage[7].TodayBytes)
	require.Equal(t, int64(3), usage[8].TodayBytes)
}

func TestProxyTrafficUnresolvedRouteRecoversWithoutReconnecting(t *testing.T) {
	proxyRepo := &trafficProxyRepoFixture{}
	s := NewProxyTrafficService(&trafficRepoFixture{}, proxyRepo)
	require.NoError(t, s.RefreshRoutes(context.Background()))
	observer := s.ObserverForProxyURL("http://new.example:8080")
	require.NotNil(t, observer)
	observer(10, 20)
	proxyRepo.proxies = []Proxy{{ID: 9, Protocol: "http", Host: "new.example", Port: 8080}}
	require.NoError(t, s.RefreshRoutes(context.Background()))
	observer(1, 2)
	usage, err := s.GetUsage(context.Background(), []int64{9})
	require.NoError(t, err)
	require.Equal(t, int64(33), usage[9].TodayBytes)
}

func TestProxyTrafficDuplicateRoutesUseAccountOrExplicitNodeIdentity(t *testing.T) {
	first := Proxy{ID: 7, Protocol: "http", Host: "same.example", Port: 8080}
	second := first
	second.ID = 8
	proxyRepo := &trafficProxyRepoFixture{proxies: []Proxy{first, second}}
	repo := &trafficRepoFixture{accountProxies: map[int64]*Proxy{1: &first, 2: &second}}
	s := NewProxyTrafficService(repo, proxyRepo)
	require.NoError(t, s.RefreshRoutes(context.Background()))
	require.True(t, s.IsAmbiguousRoute(first.URL()))
	s.ObserverForContext(proxytraffic.WithAccountID(context.Background(), 1), first.URL())(10, 20)
	s.ObserverForContext(proxytraffic.WithAccountID(context.Background(), 2), second.URL())(1, 2)
	explicit := proxytraffic.WithProxyID(context.Background(), 8, second.URL())
	s.ObserverForContext(explicit, second.URL())(3, 4)
	usage, err := s.GetUsage(context.Background(), []int64{7, 8})
	require.NoError(t, err)
	require.Equal(t, int64(30), usage[7].TodayBytes)
	require.Equal(t, int64(10), usage[8].TodayBytes)
	ctx, nodeID := s.BindAccount(context.Background(), second.URL(), 2)
	require.Equal(t, int64(8), nodeID)
	require.Equal(t, nodeID, proxytraffic.ProxyID(ctx, second.URL()))
}

func TestProxyTrafficUnresolvedDialDoesNotWaitForRouteRefresh(t *testing.T) {
	s := NewProxyTrafficService(&trafficRepoFixture{}, nil)
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	ready := make(chan bool, 1)
	go func() {
		observer := s.ObserverForProxyURL("http://new.example:8080")
		ready <- observer != nil
	}()
	select {
	case observed := <-ready:
		require.True(t, observed)
	case <-time.After(time.Second):
		t.Fatal("traffic route refresh blocked the connection observer")
	}
}
