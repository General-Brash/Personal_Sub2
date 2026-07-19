package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type permanentBalanceEligibilityCheckerStub struct {
	err    error
	calls  int
	userID int64
}

func (s *permanentBalanceEligibilityCheckerStub) CheckPermanentBalanceEligibility(_ context.Context, userID int64) error {
	s.calls++
	s.userID = userID
	return s.err
}

func TestCheckBillingEligibilityRejectsNegativePermanentBalanceBeforeBillingMode(t *testing.T) {
	for _, tc := range []struct {
		name         string
		group        *Group
		subscription *UserSubscription
	}{
		{name: "balance mode"},
		{
			name:         "subscription mode",
			group:        &Group{ID: 7, SubscriptionType: SubscriptionTypeSubscription},
			subscription: &UserSubscription{Status: SubscriptionStatusActive},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checker := &permanentBalanceEligibilityCheckerStub{err: ErrInsufficientBalance}
			svc := NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeStandard}, nil)
			t.Cleanup(svc.Stop)
			svc.SetPermanentBalanceEligibilityChecker(checker)

			err := svc.CheckBillingEligibility(context.Background(), &User{ID: 42}, nil, tc.group, tc.subscription, "")

			require.ErrorIs(t, err, ErrInsufficientBalance)
			require.Equal(t, 1, checker.calls)
			require.Equal(t, int64(42), checker.userID)
		})
	}
}

func TestCheckBillingEligibilitySimpleModeDoesNotApplyBankEligibility(t *testing.T) {
	checker := &permanentBalanceEligibilityCheckerStub{err: ErrInsufficientBalance}
	svc := NewBillingCacheService(nil, nil, nil, nil, nil, nil, &config.Config{RunMode: config.RunModeSimple}, nil)
	t.Cleanup(svc.Stop)
	svc.SetPermanentBalanceEligibilityChecker(checker)

	require.NoError(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 42}, nil, nil, nil, ""))
	require.Zero(t, checker.calls)
}
