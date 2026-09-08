package repository

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newOpenAIResponsesSessionWindowCaches(t *testing.T) (*miniredis.Miniredis, *redis.Client, service.OpenAIWSSessionPreemptionCache, service.OpenAIWSSessionPreemptionCache) {
	t.Helper()
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})
	cacheA, ok := NewGatewayCache(clientA).(service.OpenAIWSSessionPreemptionCache)
	require.True(t, ok, "gateway cache must expose the WS session-window capability")
	cacheB, ok := NewGatewayCache(clientB).(service.OpenAIWSSessionPreemptionCache)
	require.True(t, ok, "a second gateway cache must expose the WS session-window capability")
	return server, clientA, cacheA, cacheB
}

func TestGatewayCacheOpenAIResponsesSessionWindow_AtomicOwnerLifecycle(t *testing.T) {
	server, client, cacheA, cacheB := newOpenAIResponsesSessionWindowCaches(t)
	ctx := context.Background()
	groupID := int64(7)
	sessionHash := "wspreempt:11:session-lifecycle"
	key := buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)

	previous, err := cacheA.ClaimOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-a"), 2*time.Minute)
	require.NoError(t, err)
	require.Empty(t, previous)

	previous, err = cacheB.ClaimOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-b"), 2*time.Minute)
	require.NoError(t, err)
	require.Equal(t, []byte("owner-a"), previous)

	refreshed, err := cacheA.CompareAndRefreshOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-a"), 5*time.Minute)
	require.NoError(t, err)
	require.False(t, refreshed, "the stale owner must not refresh the replacement")

	refreshed, err = cacheB.CompareAndRefreshOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-b"), 5*time.Minute)
	require.NoError(t, err)
	require.True(t, refreshed)
	ttl := server.TTL(key)
	require.Greater(t, ttl, 4*time.Minute, "the successful compare-refresh must keep the requested TTL")

	deleted, err := cacheA.CompareAndDeleteOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-a"))
	require.NoError(t, err)
	require.False(t, deleted, "a late cleanup from the old owner must not delete the replacement")
	require.Equal(t, "owner-b", client.Get(ctx, key).Val())

	deleted, err = cacheB.CompareAndDeleteOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-b"))
	require.NoError(t, err)
	require.True(t, deleted)
	require.Equal(t, int64(0), client.Exists(ctx, key).Val())

	previous, err = cacheA.ClaimOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-after-expiry"), 2*time.Second)
	require.NoError(t, err)
	require.Empty(t, previous)
	server.FastForward(3 * time.Second)
	previous, err = cacheB.ClaimOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte("owner-after-expired"), time.Minute)
	require.NoError(t, err)
	require.Empty(t, previous, "an expired owner must not be reported as current")
}

func TestGatewayCacheOpenAIResponsesSessionWindow_IsolatedByGroupAPIKeyAndSession(t *testing.T) {
	_, client, cacheA, _ := newOpenAIResponsesSessionWindowCaches(t)
	ctx := context.Background()

	groupOneAPIKeyOne := "wspreempt:11:session-scope"
	groupTwoAPIKeyOne := "wspreempt:11:session-scope"
	groupOneAPIKeyTwo := "wspreempt:12:session-scope"

	previous, err := cacheA.ClaimOpenAIResponsesSessionWindow(ctx, 7, groupOneAPIKeyOne, []byte("group-one"), time.Minute)
	require.NoError(t, err)
	require.Empty(t, previous)
	previous, err = cacheA.ClaimOpenAIResponsesSessionWindow(ctx, 8, groupTwoAPIKeyOne, []byte("group-two"), time.Minute)
	require.NoError(t, err)
	require.Empty(t, previous)
	previous, err = cacheA.ClaimOpenAIResponsesSessionWindow(ctx, 7, groupOneAPIKeyTwo, []byte("api-key-two"), time.Minute)
	require.NoError(t, err)
	require.Empty(t, previous)

	previous, err = cacheA.ClaimOpenAIResponsesSessionWindow(ctx, 7, groupOneAPIKeyOne, []byte("group-one-replacement"), time.Minute)
	require.NoError(t, err)
	require.Equal(t, []byte("group-one"), previous)
	require.Equal(t, "group-two", client.Get(ctx, buildOpenAIResponsesSessionWindowKey(8, groupTwoAPIKeyOne)).Val())
	require.Equal(t, "api-key-two", client.Get(ctx, buildOpenAIResponsesSessionWindowKey(7, groupOneAPIKeyTwo)).Val())
}

func TestGatewayCacheOpenAIResponsesSessionWindow_ConcurrentClaimsReturnWholeOwners(t *testing.T) {
	_, client, cacheA, cacheB := newOpenAIResponsesSessionWindowCaches(t)
	ctx := context.Background()
	const (
		groupID     = int64(9)
		sessionHash = "wspreempt:21:concurrent"
		claimCount  = 64
	)

	knownOwners := make(map[string]struct{}, claimCount+1)
	knownOwners[""] = struct{}{}
	for i := 0; i < claimCount; i++ {
		knownOwners[fmt.Sprintf("owner-%02d", i)] = struct{}{}
	}

	type claimResult struct {
		previous string
		err      error
	}
	results := make(chan claimResult, claimCount)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < claimCount; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			cache := cacheA
			if i%2 == 1 {
				cache = cacheB
			}
			previous, err := cache.ClaimOpenAIResponsesSessionWindow(ctx, groupID, sessionHash, []byte(fmt.Sprintf("owner-%02d", i)), time.Minute)
			results <- claimResult{previous: string(previous), err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)

	for result := range results {
		require.NoError(t, result.err)
		_, ok := knownOwners[result.previous]
		require.True(t, ok, "atomic claim returned a partial/unknown previous owner: %q", result.previous)
	}
	finalOwner, err := client.Get(ctx, buildOpenAIResponsesSessionWindowKey(groupID, sessionHash)).Result()
	require.NoError(t, err)
	_, ok := knownOwners[finalOwner]
	require.True(t, ok)
	require.NotEmpty(t, finalOwner)
}

func TestGatewayCacheOpenAIResponsesSessionWindow_RejectsUnavailableOrInvalid(t *testing.T) {
	ctx := context.Background()
	var cache *gatewayCache
	_, err := cache.ClaimOpenAIResponsesSessionWindow(ctx, 1, "session", []byte("owner"), time.Minute)
	require.Error(t, err)

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := &gatewayCache{rdb: client}
	_, err = store.ClaimOpenAIResponsesSessionWindow(ctx, 1, " ", []byte("owner"), time.Minute)
	require.Error(t, err)
	_, err = store.ClaimOpenAIResponsesSessionWindow(ctx, 1, "session", nil, time.Minute)
	require.Error(t, err)
	_, err = store.ClaimOpenAIResponsesSessionWindow(ctx, 1, "session", []byte("owner"), 0)
	require.Error(t, err)
	refreshed, err := store.CompareAndRefreshOpenAIResponsesSessionWindow(ctx, 1, "session", []byte("owner"), time.Minute)
	require.NoError(t, err)
	require.False(t, refreshed, "refreshing an absent/expired owner is a compare miss")
	deleted, err := store.CompareAndDeleteOpenAIResponsesSessionWindow(ctx, 1, "session", nil)
	require.Error(t, err)
	require.False(t, deleted)
}
