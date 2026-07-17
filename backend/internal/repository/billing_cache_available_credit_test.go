//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBillingCacheAvailableCreditUsesSeparateFixedKeyAndInvalidates(t *testing.T) {
	ctx := context.Background()
	cache, redisServer := newMiniRedisCache(t)
	userID := int64(42)
	key := billingAvailableCreditKey(userID)

	require.NoError(t, cache.SetAvailableCredit(ctx, userID, 1.25, 90*time.Second))
	raw, err := redisServer.Get(key)
	require.NoError(t, err)
	require.Equal(t, "1.25000000", raw)
	require.NotEqual(t, billingBalanceKey(userID), key)

	available, err := cache.GetAvailableCredit(ctx, userID)
	require.NoError(t, err)
	require.InDelta(t, 1.25, available, 1e-12)

	require.NoError(t, cache.InvalidateAvailableCredit(ctx, userID))
	require.False(t, redisServer.Exists(key))
}

func TestBillingCacheAvailableCreditNonPositiveTTLRemovesPriorCandidate(t *testing.T) {
	ctx := context.Background()
	cache, redisServer := newMiniRedisCache(t)
	userID := int64(43)
	key := billingAvailableCreditKey(userID)

	require.NoError(t, cache.SetAvailableCredit(ctx, userID, 1, time.Minute))
	require.True(t, redisServer.Exists(key))

	require.NoError(t, cache.SetAvailableCredit(ctx, userID, 1, 0))
	require.False(t, redisServer.Exists(key))
}

func TestBillingCacheAvailableCreditRoundTripsCumulativeValueAboveSingleAmountLimit(t *testing.T) {
	ctx := context.Background()
	cache, redisServer := newMiniRedisCache(t)
	const (
		userID     = int64(44)
		cumulative = 1_250_000_000_000.125
	)

	require.NoError(t, cache.SetAvailableCredit(ctx, userID, cumulative, time.Minute))
	raw, err := redisServer.Get(billingAvailableCreditKey(userID))
	require.NoError(t, err)
	require.Equal(t, "1250000000000.12500000", raw)

	available, err := cache.GetAvailableCredit(ctx, userID)
	require.NoError(t, err)
	require.InDelta(t, cumulative, available, 0.001)
}

func TestBillingCacheAvailableCreditRoundTripsNegativePermanentOverdraft(t *testing.T) {
	ctx := context.Background()
	cache, redisServer := newMiniRedisCache(t)
	const (
		userID    = int64(45)
		overdraft = -12.5
	)

	require.NoError(t, cache.SetAvailableCredit(ctx, userID, overdraft, time.Minute))
	raw, err := redisServer.Get(billingAvailableCreditKey(userID))
	require.NoError(t, err)
	require.Equal(t, "-12.50000000", raw)

	available, err := cache.GetAvailableCredit(ctx, userID)
	require.NoError(t, err)
	require.InDelta(t, overdraft, available, 1e-12)
}
