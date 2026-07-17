package repository

import (
	"context"
	"testing"
	"time"
)

type legacyPlatformQuotaUsagePort interface {
	IncrementUsageWithReset(context.Context, int64, string, float64, time.Time) error
}

func TestLegacyPlatformQuotaRecordsAndUsagePortRemainFloat64(t *testing.T) {
	limit := 10.25
	record := UserPlatformQuotaRecord{
		DailyLimitUSD:   &limit,
		DailyUsageUSD:   1.25,
		WeeklyUsageUSD:  2.5,
		MonthlyUsageUSD: 3.75,
	}

	var _ float64 = record.DailyUsageUSD
	var _ *float64 = record.DailyLimitUSD
	var _ legacyPlatformQuotaUsagePort = (UserPlatformQuotaRepository)(nil)
}
