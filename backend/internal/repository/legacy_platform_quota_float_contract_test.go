package repository

import (
	"context"
	"testing"
	"time"
)

type legacyPlatformQuotaUsagePort interface {
	IncrementUsageWithReset(context.Context, int64, string, float64, time.Time) error
}

func requireLegacyPlatformQuotaFloat64(float64) {}

func requireLegacyPlatformQuotaFloat64Pointer(*float64) {}

func TestLegacyPlatformQuotaRecordsAndUsagePortRemainFloat64(t *testing.T) {
	limit := 10.25
	record := UserPlatformQuotaRecord{
		DailyLimitUSD:   &limit,
		DailyUsageUSD:   1.25,
		WeeklyUsageUSD:  2.5,
		MonthlyUsageUSD: 3.75,
	}

	requireLegacyPlatformQuotaFloat64(record.DailyUsageUSD)
	requireLegacyPlatformQuotaFloat64Pointer(record.DailyLimitUSD)
	var _ legacyPlatformQuotaUsagePort = (UserPlatformQuotaRepository)(nil)
}
