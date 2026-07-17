package ent

import "testing"

func TestLegacyQuotaAndSubscriptionFieldsRemainFloat64(t *testing.T) {
	t.Helper()

	limit := 1.25
	quota := UserPlatformQuota{
		DailyLimitUsd:   &limit,
		WeeklyLimitUsd:  &limit,
		MonthlyLimitUsd: &limit,
		DailyUsageUsd:   1.5,
		WeeklyUsageUsd:  2.5,
		MonthlyUsageUsd: 3.5,
	}
	subscription := UserSubscription{
		DailyUsageUsd:   1.5,
		WeeklyUsageUsd:  2.5,
		MonthlyUsageUsd: 3.5,
	}

	assertFloat64(t, quota.DailyUsageUsd)
	assertFloat64(t, quota.WeeklyUsageUsd)
	assertFloat64(t, quota.MonthlyUsageUsd)
	assertFloat64(t, subscription.DailyUsageUsd)
	assertFloat64(t, subscription.WeeklyUsageUsd)
	assertFloat64(t, subscription.MonthlyUsageUsd)
}

func assertFloat64(t *testing.T, value float64) {
	t.Helper()
	if value < 0 {
		t.Fatal("unexpected negative compatibility fixture")
	}
}
