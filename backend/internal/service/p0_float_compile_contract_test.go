package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCheckPricesNotNegativeRejectsNegativeFloat(t *testing.T) {
	negative := -0.00000001

	err := checkPricesNotNegative(ChannelModelPricing{InputPrice: &negative})

	require.Error(t, err)
	require.ErrorContains(t, err, "input_price must be >= 0")
}

func TestPlanGroupInfoKeepsRateMultipliersAsLegacyFloats(t *testing.T) {
	multiplier := 1.23456789
	info := PlanGroupInfo{
		RateMultiplier:     multiplier,
		PeakRateMultiplier: multiplier,
	}

	require.Equal(t, multiplier, info.RateMultiplier)
	require.Equal(t, multiplier, info.PeakRateMultiplier)
}

func TestCalculateCostWithConfigUsesFloatDefaultRateMultiplier(t *testing.T) {
	svc := NewBillingService(&config.Config{
		Default: config.DefaultConfig{RateMultiplier: 1.5},
	}, nil)

	got, err := svc.CalculateCostWithConfig("claude-sonnet-4", UsageTokens{InputTokens: 1})

	require.NoError(t, err)
	want, err := svc.CalculateCost("claude-sonnet-4", UsageTokens{InputTokens: 1}, 1.5)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestGetEstimatedCostReturnsLegacyFloat(t *testing.T) {
	svc := NewBillingService(nil, nil)
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}

	estimated, err := svc.GetEstimatedCost("claude-sonnet-4", tokens.InputTokens, tokens.OutputTokens)

	require.NoError(t, err)
	expected, err := svc.CalculateCost("claude-sonnet-4", tokens, 1)
	require.NoError(t, err)
	require.InDelta(t, expected.ActualCost, estimated, 1e-12)
}
