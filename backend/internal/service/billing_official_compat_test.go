package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntervalToModelPricingWithBaseAppliesOfficialMultipliersAndTTL(t *testing.T) {
	base := &ModelPricing{
		InputPricePerToken:         2,
		OutputPricePerToken:        4,
		CacheCreationPricePerToken: 3,
		CacheCreation5mPrice:       3,
		CacheCreation1hPrice:       3,
		CacheReadPricePerToken:     5,
	}
	inputMultiplier := 2.0
	outputMultiplier := 0.5
	cacheWriteMultiplier := 3.0
	cacheReadMultiplier := 4.0
	cacheWrite1h := 11.0
	got := intervalToModelPricingWithBase(&PricingInterval{
		InputMultiplier:      &inputMultiplier,
		OutputMultiplier:     &outputMultiplier,
		CacheWriteMultiplier: &cacheWriteMultiplier,
		CacheWrite1hPrice:    &cacheWrite1h,
		CacheReadMultiplier:  &cacheReadMultiplier,
	}, base, nil)

	require.InDelta(t, 4.0, got.InputPricePerToken, 1e-12)
	require.InDelta(t, 2.0, got.OutputPricePerToken, 1e-12)
	require.InDelta(t, 9.0, got.CacheCreationPricePerToken, 1e-12)
	require.InDelta(t, 9.0, got.CacheCreation5mPrice, 1e-12)
	require.InDelta(t, 11.0, got.CacheCreation1hPrice, 1e-12)
	require.InDelta(t, 20.0, got.CacheReadPricePerToken, 1e-12)
	require.True(t, got.SupportsCacheBreakdown)
}

func TestBillingServiceUsesConfiguredFastAndFlexMultipliers(t *testing.T) {
	fast := 0.25
	flex := 0.75
	svc := &BillingService{fallbackPrices: map[string]*ModelPricing{
		"claude-sonnet-4": {InputPricePerToken: 1},
	}}

	fastCost, err := svc.calculateCostInternal("claude-sonnet-4", UsageTokens{InputTokens: 2}, 1, "fast", &ChannelModelPricing{FastMultiplier: &fast})
	require.NoError(t, err)
	require.InDelta(t, 0.5, fastCost.ActualCost, 1e-12)

	flexCost, err := svc.calculateCostInternal("claude-sonnet-4", UsageTokens{InputTokens: 2}, 1, "flex", &ChannelModelPricing{FlexMultiplier: &flex})
	require.NoError(t, err)
	require.InDelta(t, 1.5, flexCost.ActualCost, 1e-12)
}

func TestChannelTimePricingWeekdaysOnly(t *testing.T) {
	config := &ChannelTimePricing{
		Timezone:     "UTC",
		WeekdaysOnly: true,
		Periods: []ChannelTimePricingPeriod{{
			StartTime: "09:00", EndTime: "17:00", Multiplier: 2,
		}},
	}
	require.Equal(t, 2.0, config.MultiplierAt(time.Date(2026, time.June, 29, 10, 0, 0, 0, time.UTC))) // Monday
	require.Equal(t, 1.0, config.MultiplierAt(time.Date(2026, time.June, 27, 10, 0, 0, 0, time.UTC))) // Saturday
	clone := (ChannelModelPricing{TimePricing: config}).Clone()
	require.True(t, clone.TimePricing.WeekdaysOnly)
}
