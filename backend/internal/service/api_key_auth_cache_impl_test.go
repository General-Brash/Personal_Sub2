package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthSnapshotUsesLegacyFloat64BillingDTO(t *testing.T) {
	snapshot := APIKeyAuthSnapshot{
		User:  APIKeyAuthUserSnapshot{},
		Group: &APIKeyAuthGroupSnapshot{},
	}

	require.IsType(t, float64(0), snapshot.Quota)
	require.IsType(t, float64(0), snapshot.QuotaUsed)
	require.IsType(t, float64(0), snapshot.RateLimit5h)
	require.IsType(t, float64(0), snapshot.RateLimit1d)
	require.IsType(t, float64(0), snapshot.RateLimit7d)
	require.IsType(t, float64(0), snapshot.User.Balance)
	require.IsType(t, (*float64)(nil), snapshot.User.BalanceNotifyThreshold)
	require.IsType(t, float64(0), snapshot.User.TotalRecharged)
	require.IsType(t, float64(0), snapshot.Group.RateMultiplier)
	require.IsType(t, (*float64)(nil), snapshot.Group.DailyLimitUSD)
	require.IsType(t, (*float64)(nil), snapshot.Group.WeeklyLimitUSD)
	require.IsType(t, (*float64)(nil), snapshot.Group.MonthlyLimitUSD)
	require.IsType(t, float64(0), snapshot.Group.ImageRateMultiplier)
	require.IsType(t, (*float64)(nil), snapshot.Group.ImagePrice1K)
	require.IsType(t, (*float64)(nil), snapshot.Group.ImagePrice2K)
	require.IsType(t, (*float64)(nil), snapshot.Group.ImagePrice4K)
	require.IsType(t, float64(0), snapshot.Group.VideoRateMultiplier)
	require.IsType(t, (*float64)(nil), snapshot.Group.VideoPrice480P)
	require.IsType(t, (*float64)(nil), snapshot.Group.VideoPrice720P)
	require.IsType(t, (*float64)(nil), snapshot.Group.VideoPrice1080P)
	require.IsType(t, (*float64)(nil), snapshot.Group.WebSearchPricePerCall)
	require.IsType(t, float64(0), snapshot.Group.PeakRateMultiplier)
}

func TestAPIKeyAuthSnapshotRoundTripsLegacyBillingValues(t *testing.T) {
	dailyLimit := 16.25
	weeklyLimit := 32.5
	monthlyLimit := 64.75
	imagePrice1K := 0.5
	imagePrice4K := 1.5
	videoPrice480P := 2.25
	videoPrice720P := 2.5
	videoPrice1080P := 2.75
	webSearchPrice := 3.5
	notifyThreshold := 1.25

	source := &APIKey{
		ID:          11,
		UserID:      22,
		Key:         "source-key",
		Name:        "source-name",
		Status:      StatusActive,
		Quota:       10.5,
		QuotaUsed:   2.25,
		RateLimit5h: 3.5,
		RateLimit1d: 4.5,
		RateLimit7d: 5.5,
		User: &User{
			ID:                         22,
			Status:                     StatusActive,
			Balance:                    6.5,
			BalanceNotifyThreshold:     &notifyThreshold,
			TotalRecharged:             7.5,
			BalanceNotifyThresholdType: "fixed",
		},
		Group: &Group{
			ID:                    33,
			Status:                StatusActive,
			RateMultiplier:        1.5,
			DailyLimitUSD:         &dailyLimit,
			WeeklyLimitUSD:        &weeklyLimit,
			MonthlyLimitUSD:       &monthlyLimit,
			ImageRateMultiplier:   1.25,
			ImagePrice1K:          &imagePrice1K,
			ImagePrice2K:          nil,
			ImagePrice4K:          &imagePrice4K,
			VideoRateMultiplier:   1.75,
			VideoPrice480P:        &videoPrice480P,
			VideoPrice720P:        &videoPrice720P,
			VideoPrice1080P:       &videoPrice1080P,
			WebSearchPricePerCall: &webSearchPrice,
			PeakRateMultiplier:    2,
		},
	}

	service := &APIKeyService{}
	snapshot := service.snapshotFromAPIKey(context.Background(), source)
	require.NotNil(t, snapshot)
	require.Equal(t, 15, snapshot.Version)
	require.Equal(t, source.Quota, snapshot.Quota)
	require.Equal(t, source.QuotaUsed, snapshot.QuotaUsed)
	require.Equal(t, source.RateLimit5h, snapshot.RateLimit5h)
	require.Equal(t, source.RateLimit1d, snapshot.RateLimit1d)
	require.Equal(t, source.RateLimit7d, snapshot.RateLimit7d)
	require.Equal(t, source.User.Balance, snapshot.User.Balance)
	require.Equal(t, source.User.BalanceNotifyThreshold, snapshot.User.BalanceNotifyThreshold)
	require.Equal(t, source.User.TotalRecharged, snapshot.User.TotalRecharged)
	require.NotNil(t, snapshot.Group)
	require.Equal(t, source.Group.RateMultiplier, snapshot.Group.RateMultiplier)
	require.Equal(t, source.Group.ImageRateMultiplier, snapshot.Group.ImageRateMultiplier)
	require.Equal(t, source.Group.VideoRateMultiplier, snapshot.Group.VideoRateMultiplier)
	require.Equal(t, source.Group.PeakRateMultiplier, snapshot.Group.PeakRateMultiplier)

	restored := service.snapshotToAPIKey("cached-key", snapshot)
	require.NotNil(t, restored)
	require.Equal(t, "cached-key", restored.Key)
	require.Equal(t, source.Quota, restored.Quota)
	require.Equal(t, source.QuotaUsed, restored.QuotaUsed)
	require.Equal(t, source.RateLimit5h, restored.RateLimit5h)
	require.Equal(t, source.RateLimit1d, restored.RateLimit1d)
	require.Equal(t, source.RateLimit7d, restored.RateLimit7d)
	require.Equal(t, source.User.Balance, restored.User.Balance)
	require.Equal(t, source.User.BalanceNotifyThreshold, restored.User.BalanceNotifyThreshold)
	require.Equal(t, source.User.TotalRecharged, restored.User.TotalRecharged)
	require.NotNil(t, restored.Group)
	require.Equal(t, source.Group.RateMultiplier, restored.Group.RateMultiplier)
	require.Equal(t, source.Group.ImageRateMultiplier, restored.Group.ImageRateMultiplier)
	require.Equal(t, source.Group.VideoRateMultiplier, restored.Group.VideoRateMultiplier)
	require.Equal(t, source.Group.PeakRateMultiplier, restored.Group.PeakRateMultiplier)

	requireCachedFloat64Pointer(t, source.Group.DailyLimitUSD, snapshot.Group.DailyLimitUSD, restored.Group.DailyLimitUSD)
	requireCachedFloat64Pointer(t, source.Group.WeeklyLimitUSD, snapshot.Group.WeeklyLimitUSD, restored.Group.WeeklyLimitUSD)
	requireCachedFloat64Pointer(t, source.Group.MonthlyLimitUSD, snapshot.Group.MonthlyLimitUSD, restored.Group.MonthlyLimitUSD)
	requireCachedFloat64Pointer(t, source.Group.ImagePrice1K, snapshot.Group.ImagePrice1K, restored.Group.ImagePrice1K)
	requireCachedFloat64Pointer(t, source.Group.ImagePrice2K, snapshot.Group.ImagePrice2K, restored.Group.ImagePrice2K)
	requireCachedFloat64Pointer(t, source.Group.ImagePrice4K, snapshot.Group.ImagePrice4K, restored.Group.ImagePrice4K)
	requireCachedFloat64Pointer(t, source.Group.VideoPrice480P, snapshot.Group.VideoPrice480P, restored.Group.VideoPrice480P)
	requireCachedFloat64Pointer(t, source.Group.VideoPrice720P, snapshot.Group.VideoPrice720P, restored.Group.VideoPrice720P)
	requireCachedFloat64Pointer(t, source.Group.VideoPrice1080P, snapshot.Group.VideoPrice1080P, restored.Group.VideoPrice1080P)
	requireCachedFloat64Pointer(t, source.Group.WebSearchPricePerCall, snapshot.Group.WebSearchPricePerCall, restored.Group.WebSearchPricePerCall)
}

func requireCachedFloat64Pointer(t *testing.T, want, cached, restored *float64) {
	t.Helper()
	if want == nil {
		require.Nil(t, cached)
		require.Nil(t, restored)
		return
	}

	require.NotNil(t, cached)
	require.NotNil(t, restored)
	require.Equal(t, *want, *cached)
	require.Equal(t, *want, *restored)
}
