package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type gatewayUsageBillingAvailableCreditCacheStub struct {
	BillingCache

	invalidateCalls int
	invalidateErr   error
}

func (s *gatewayUsageBillingAvailableCreditCacheStub) GetAvailableCredit(context.Context, int64) (float64, error) {
	return 0, errors.New("cache miss")
}

func (s *gatewayUsageBillingAvailableCreditCacheStub) SetAvailableCredit(context.Context, int64, float64, time.Duration) error {
	return nil
}

func (s *gatewayUsageBillingAvailableCreditCacheStub) InvalidateAvailableCredit(context.Context, int64) error {
	s.invalidateCalls++
	return s.invalidateErr
}

type gatewayUsageBillingRepositoryStub struct {
	UsageBillingRepository

	result *UsageBillingApplyResult
	err    error
}

func (s *gatewayUsageBillingRepositoryStub) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.result, nil
}

func TestApplyUsageBillingInvalidatesAvailableCreditAfterCommittedTemporaryOnlyCharge(t *testing.T) {
	newDeps := func(cache *gatewayUsageBillingAvailableCreditCacheStub) (*billingDeps, func()) {
		cacheService := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
		return &billingDeps{
			billingCacheService: cacheService,
			deferredService:     &DeferredService{},
		}, cacheService.Stop
	}
	params := func() *postUsageBillingParams {
		return &postUsageBillingParams{
			Cost:    &CostBreakdown{ActualCost: 1},
			User:    &User{ID: 42},
			APIKey:  &APIKey{ID: 43},
			Account: &Account{ID: 44},
		}
	}

	t.Run("committed temporary-only charge invalidates", func(t *testing.T) {
		cache := &gatewayUsageBillingAvailableCreditCacheStub{}
		deps, stop := newDeps(cache)
		defer stop()
		zero := 0.0

		_, err := applyUsageBilling(context.Background(), "gateway-openai-request", nil, params(), deps, &gatewayUsageBillingRepositoryStub{
			result: &UsageBillingApplyResult{Applied: true, PermanentBalanceDeduction: &zero},
		})

		require.NoError(t, err)
		require.Equal(t, 1, cache.invalidateCalls)
	})

	t.Run("duplicate and failed billing do not invalidate", func(t *testing.T) {
		cache := &gatewayUsageBillingAvailableCreditCacheStub{}
		deps, stop := newDeps(cache)
		defer stop()

		_, err := applyUsageBilling(context.Background(), "duplicate-request", nil, params(), deps, &gatewayUsageBillingRepositoryStub{
			result: &UsageBillingApplyResult{Applied: false},
		})
		require.NoError(t, err)
		require.Equal(t, 0, cache.invalidateCalls)

		_, err = applyUsageBilling(context.Background(), "failed-request", nil, params(), deps, &gatewayUsageBillingRepositoryStub{err: errors.New("transaction rolled back")})
		require.Error(t, err)
		require.Equal(t, 0, cache.invalidateCalls)
	})

	t.Run("cache failure does not reverse committed billing", func(t *testing.T) {
		cache := &gatewayUsageBillingAvailableCreditCacheStub{invalidateErr: errors.New("redis unavailable")}
		deps, stop := newDeps(cache)
		defer stop()
		zero := 0.0

		_, err := applyUsageBilling(context.Background(), "cache-failure-request", nil, params(), deps, &gatewayUsageBillingRepositoryStub{
			result: &UsageBillingApplyResult{Applied: true, PermanentBalanceDeduction: &zero},
		})

		require.NoError(t, err)
		require.Equal(t, 1, cache.invalidateCalls)
	})
}
