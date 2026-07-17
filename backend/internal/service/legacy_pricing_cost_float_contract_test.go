package service

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLegacyPricingCostCoreUsesFloat64(t *testing.T) {
	floatType := reflect.TypeOf(float64(0))
	floatPtrType := reflect.TypeOf((*float64)(nil))

	for _, fieldName := range []string{
		"InputCost",
		"OutputCost",
		"ImageOutputCost",
		"CacheCreationCost",
		"CacheReadCost",
		"TotalCost",
		"ActualCost",
	} {
		field, ok := reflect.TypeOf(CostBreakdown{}).FieldByName(fieldName)
		require.Truef(t, ok, "CostBreakdown.%s is missing", fieldName)
		require.Equalf(t, floatType, field.Type, "CostBreakdown.%s must remain float64", fieldName)
	}

	for _, fieldName := range []string{
		"InputPricePerToken",
		"OutputPricePerToken",
		"CacheCreationPricePerToken",
		"CacheReadPricePerToken",
		"LongContextInputMultiplier",
		"LongContextOutputMultiplier",
		"ImageOutputPricePerToken",
	} {
		field, ok := reflect.TypeOf(ModelPricing{}).FieldByName(fieldName)
		require.Truef(t, ok, "ModelPricing.%s is missing", fieldName)
		require.Equalf(t, floatType, field.Type, "ModelPricing.%s must remain float64", fieldName)
	}

	for _, typ := range []reflect.Type{reflect.TypeOf(ChannelModelPricing{}), reflect.TypeOf(PricingInterval{})} {
		for _, fieldName := range []string{"InputPrice", "OutputPrice", "CacheWritePrice", "CacheReadPrice", "PerRequestPrice"} {
			field, ok := typ.FieldByName(fieldName)
			require.Truef(t, ok, "%s.%s is missing", typ.Name(), fieldName)
			require.Equalf(t, floatPtrType, field.Type, "%s.%s must remain *float64", typ.Name(), fieldName)
		}
	}

	imageOutputPrice, ok := reflect.TypeOf(ChannelModelPricing{}).FieldByName("ImageOutputPrice")
	require.True(t, ok)
	require.Equal(t, floatPtrType, imageOutputPrice.Type)

	inputRate, ok := reflect.TypeOf(CostInput{}).FieldByName("RateMultiplier")
	require.True(t, ok)
	require.Equal(t, floatType, inputRate.Type)

	defaultPerRequestPrice, ok := reflect.TypeOf(ResolvedPricing{}).FieldByName("DefaultPerRequestPrice")
	require.True(t, ok)
	require.Equal(t, floatType, defaultPerRequestPrice.Type)

	var _ func(*BillingService, string, UsageTokens, float64) (*CostBreakdown, error) = (*BillingService).CalculateCost
	var _ func(*BillingService, string, UsageTokens, float64, string) (*CostBreakdown, error) = (*BillingService).CalculateCostWithServiceTier
	var _ func(*BillingService, string, UsageTokens, float64, int, float64) (*CostBreakdown, error) = (*BillingService).CalculateCostWithLongContext
}

func TestLegacyPricingCostCoreCalculatesWithFloat64(t *testing.T) {
	billing := NewBillingService(nil, nil)
	cost, err := billing.CalculateCost("claude-sonnet-4", UsageTokens{
		InputTokens:  2,
		OutputTokens: 1,
	}, 1.5)

	require.NoError(t, err)
	require.InDelta(t, 21e-6, cost.TotalCost, 1e-12)
	require.InDelta(t, 31.5e-6, cost.ActualCost, 1e-12)
}

func TestLegacyPricingCostCoreNilConfigUsesDefaultMultiplier(t *testing.T) {
	billing := NewBillingService(nil, nil)
	tokens := UsageTokens{InputTokens: 2, OutputTokens: 1}

	var cost *CostBreakdown
	var err error
	require.NotPanics(t, func() {
		cost, err = billing.CalculateCostWithConfig("claude-sonnet-4", tokens)
	})
	require.NoError(t, err)
	require.InDelta(t, 21e-6, cost.TotalCost, 1e-12)
	require.InDelta(t, cost.TotalCost, cost.ActualCost, 1e-12)

	var unknownCost *CostBreakdown
	require.NotPanics(t, func() {
		unknownCost, err = billing.CalculateCostWithConfig("gpt-unknown-model", tokens)
	})
	require.Nil(t, unknownCost)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}
