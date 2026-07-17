package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type availableCreditCacheStub struct {
	BillingCache

	getValue float64
	getErr   error

	setValue float64
	setTTL   time.Duration
	setCalls int

	deductCalls     int
	invalidateCalls int
}

func (s *availableCreditCacheStub) GetAvailableCredit(context.Context, int64) (float64, error) {
	return s.getValue, s.getErr
}

func (s *availableCreditCacheStub) SetAvailableCredit(_ context.Context, _ int64, value float64, ttl time.Duration) error {
	s.setValue = value
	s.setTTL = ttl
	s.setCalls++
	return nil
}

func (s *availableCreditCacheStub) DeductUserBalance(context.Context, int64, float64) error {
	s.deductCalls++
	return nil
}

func (s *availableCreditCacheStub) InvalidateAvailableCredit(context.Context, int64) error {
	s.invalidateCalls++
	return nil
}

type availableCreditSnapshotRepoStub struct {
	UserRepository

	snapshot AvailableCreditSnapshot
	err      error
	calls    int
}

func (s *availableCreditSnapshotRepoStub) GetAvailableCreditSnapshot(context.Context, int64) (AvailableCreditSnapshot, error) {
	s.calls++
	return s.snapshot, s.err
}

func TestAvailableCreditCacheMissLoadsSnapshotAndUsesEarliestExpiryTTL(t *testing.T) {
	now := time.Now().UTC()
	earliestExpiry := now.Add(90 * time.Second)
	cache := &availableCreditCacheStub{getErr: errors.New("cache miss")}
	userRepo := &availableCreditSnapshotRepoStub{snapshot: AvailableCreditSnapshot{
		PermanentBalance:              0,
		TemporaryCredit:               1.25,
		EarliestTemporaryCreditExpiry: &earliestExpiry,
	}}
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckAvailableCreditEligibility(context.Background(), 42)

	require.NoError(t, err)
	require.Equal(t, 1, userRepo.calls)
	require.Equal(t, 1, cache.setCalls)
	require.Equal(t, 1.25, cache.setValue)
	require.LessOrEqual(t, cache.setTTL, 90*time.Second)
	require.Greater(t, cache.setTTL, time.Duration(0))
}

func TestAvailableCreditEligibilityRejectsWhenPermanentAndTemporaryCreditAreZero(t *testing.T) {
	cache := &availableCreditCacheStub{getErr: errors.New("cache miss")}
	userRepo := &availableCreditSnapshotRepoStub{snapshot: AvailableCreditSnapshot{}}
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckAvailableCreditEligibility(context.Background(), 42)

	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestAvailableCreditEligibilityRejectsNonFiniteCachedValue(t *testing.T) {
	cache := &availableCreditCacheStub{getValue: math.NaN()}
	userRepo := &availableCreditSnapshotRepoStub{snapshot: AvailableCreditSnapshot{}}
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckAvailableCreditEligibility(context.Background(), 42)

	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Equal(t, 1, userRepo.calls)
}

func TestAvailableCreditCacheHitReturnsCachedPrecheckWithoutDatabaseRead(t *testing.T) {
	cache := &availableCreditCacheStub{getValue: 5}
	userRepo := &availableCreditSnapshotRepoStub{snapshot: AvailableCreditSnapshot{
		PermanentBalance: 0,
		TemporaryCredit:  0,
	}}
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckAvailableCreditEligibility(context.Background(), 42)

	require.NoError(t, err)
	require.Zero(t, userRepo.calls)
	require.Zero(t, cache.setCalls)
}

func TestAvailableCreditSnapshotTotalAllowsAggregateAboveSingleAmountLimit(t *testing.T) {
	snapshot := AvailableCreditSnapshot{
		PermanentBalance: 999999999999.5,
		TemporaryCredit:  250000000000.625,
	}

	require.Equal(t, 1250000000000.125, snapshot.Total())
	require.True(t, math.IsNaN(AvailableCreditSnapshot{PermanentBalance: math.NaN()}.Total()))
}

func TestAvailableCreditInvalidationRemovesIndependentCacheEntry(t *testing.T) {
	cache := &availableCreditCacheStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.InvalidateAvailableCredit(context.Background(), 42)

	require.NoError(t, err)
	require.Equal(t, 1, cache.invalidateCalls)
}

func TestAvailableCreditDeductBalanceCacheAlsoInvalidatesIndependentEntry(t *testing.T) {
	cache := &availableCreditCacheStub{}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)

	err := svc.DeductBalanceCache(context.Background(), 42, 0.25)

	require.NoError(t, err)
	require.Equal(t, 1, cache.deductCalls)
	require.Equal(t, 1, cache.invalidateCalls)
}
