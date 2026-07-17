package service

import (
	"context"
	"testing"
)

type legacySubscriptionUsagePort interface {
	IncrementUsage(context.Context, int64, float64) error
}

func TestLegacySubscriptionUsageBoundaryRemainsFloat64(t *testing.T) {
	limit := 10.25
	subscription := &UserSubscription{DailyUsageUSD: 9.75}
	group := &Group{DailyLimitUSD: &limit}

	if !subscription.CheckDailyLimit(group, 0.5) {
		t.Fatal("float64 subscription usage should remain below the float64 group limit")
	}
	if subscription.CheckDailyLimit(group, 0.51) {
		t.Fatal("float64 subscription usage should respect the float64 group limit")
	}

	var _ float64 = subscription.DailyUsageUSD
	var _ legacySubscriptionUsagePort = (UserSubscriptionRepository)(nil)
	var _ func(*SubscriptionService, context.Context, int64, float64) error = (*SubscriptionService).RecordUsage
}
