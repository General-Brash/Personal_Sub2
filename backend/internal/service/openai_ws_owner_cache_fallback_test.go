package service

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// r05OpenAIWSSessionOwnerCache is a controlled shared-cache substitute: two
// service values can arbitrate against the same owner map without any network
// dependency. It deliberately embeds GatewayCache because these tests exercise
// only the optional WS preemption capability.
type r05OpenAIWSSessionOwnerCache struct {
	GatewayCache
	mu       sync.Mutex
	owners   map[string][]byte
	claimErr error
}

func (c *r05OpenAIWSSessionOwnerCache) key(groupID int64, sessionHash string) string {
	return fmt.Sprintf("%d:%s", groupID, sessionHash)
}

func (c *r05OpenAIWSSessionOwnerCache) ClaimOpenAIResponsesSessionWindow(_ context.Context, groupID int64, sessionHash string, owner []byte, _ time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.claimErr != nil {
		return nil, c.claimErr
	}
	if c.owners == nil {
		c.owners = make(map[string][]byte)
	}
	key := c.key(groupID, sessionHash)
	previous := append([]byte(nil), c.owners[key]...)
	c.owners[key] = append([]byte(nil), owner...)
	return previous, nil
}

func (c *r05OpenAIWSSessionOwnerCache) CompareAndRefreshOpenAIResponsesSessionWindow(_ context.Context, groupID int64, sessionHash string, expected []byte, _ time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.owners[c.key(groupID, sessionHash)]) == string(expected), nil
}

func (c *r05OpenAIWSSessionOwnerCache) CompareAndDeleteOpenAIResponsesSessionWindow(_ context.Context, groupID int64, sessionHash string, expected []byte) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := c.key(groupID, sessionHash)
	if string(c.owners[key]) != string(expected) {
		return false, nil
	}
	delete(c.owners, key)
	return true, nil
}

func (c *r05OpenAIWSSessionOwnerCache) current(groupID int64, sessionHash string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return string(c.owners[c.key(groupID, sessionHash)])
}

func TestR05OpenAIWSSessionOwner_DualServiceArbitrationAndCacheFailureFallback(t *testing.T) {
	account := &Account{ID: 901, Platform: PlatformOpenAI, Type: AccountTypeOAuth}

	t.Run("shared cache preempts old service and stale cleanup cannot release replacement", func(t *testing.T) {
		cache := &r05OpenAIWSSessionOwnerCache{}
		serviceA := &OpenAIGatewayService{cache: cache}
		serviceB := &OpenAIGatewayService{cache: cache}
		const groupID, apiKeyID = int64(701), int64(801)
		const sessionHash = "r05-dual-service"

		ctxA, cleanupA, armed, preempted := serviceA.beginOpenAIWSSessionPreemptContext(context.Background(), account, groupID, apiKeyID, sessionHash, false)
		require.True(t, armed)
		require.False(t, preempted)

		ctxB, cleanupB, armed, preempted := serviceB.beginOpenAIWSSessionPreemptContext(context.Background(), account, groupID, apiKeyID, sessionHash, false)
		require.True(t, armed)
		require.True(t, preempted)
		require.True(t, isOpenAIWSSessionPreempted(ctxA))
		require.False(t, isOpenAIWSSessionPreempted(ctxB))

		replacement := cache.current(groupID, openAIWSSessionPreemptCacheHash(apiKeyID, sessionHash))
		require.NotEmpty(t, replacement)
		cleanupA()
		require.Equal(t, replacement, cache.current(groupID, openAIWSSessionPreemptCacheHash(apiKeyID, sessionHash)), "late old-owner cleanup must not release service B's owner")
		cleanupB()
		require.Empty(t, cache.current(groupID, openAIWSSessionPreemptCacheHash(apiKeyID, sessionHash)))
	})

	t.Run("cache claim failure retains local same-process preemption", func(t *testing.T) {
		cache := &r05OpenAIWSSessionOwnerCache{claimErr: errors.New("controlled cache unavailable")}
		serviceA := &OpenAIGatewayService{cache: cache}
		serviceB := &OpenAIGatewayService{cache: cache}
		const groupID, apiKeyID = int64(702), int64(802)
		const sessionHash = "r05-cache-failure"

		ctxA, cleanupA, armed, preempted := serviceA.beginOpenAIWSSessionPreemptContext(context.Background(), account, groupID, apiKeyID, sessionHash, false)
		require.True(t, armed, "cache failure must not reject an otherwise eligible local session")
		require.False(t, preempted)
		ctxB, cleanupB, armed, preempted := serviceB.beginOpenAIWSSessionPreemptContext(context.Background(), account, groupID, apiKeyID, sessionHash, false)
		require.True(t, armed)
		require.True(t, preempted, "the process-local registry is the explicit degradation boundary")
		require.True(t, isOpenAIWSSessionPreempted(ctxA))
		require.False(t, isOpenAIWSSessionPreempted(ctxB))
		cleanupA()
		cleanupB()
	})
}
