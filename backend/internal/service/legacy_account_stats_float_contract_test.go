package service

import (
	"context"
	"testing"
)

func TestLegacyAccountStatsFloatInputsRemainFloat64(_ *testing.T) {
	var _ func(
		context.Context,
		*ChannelService,
		*BillingService,
		int64,
		int64,
		string,
		UsageTokens,
		int,
		float64,
	) *float64 = resolveAccountStatsCost

	var _ func(
		context.Context,
		*UsageLog,
		*ChannelService,
		*BillingService,
		int64,
		int64,
		string,
		string,
		UsageTokens,
		float64,
	) = applyAccountStatsCost
}

func TestLegacyAccountStatsFloatModelPricingCalculation(t *testing.T) {
	billingService := &BillingService{
		fallbackPrices: map[string]*ModelPricing{
			"claude-sonnet-4": {
				InputPricePerToken:  0.25,
				OutputPricePerToken: 0.5,
			},
		},
	}

	got := tryModelFilePricing(billingService, "claude-sonnet-4", UsageTokens{
		InputTokens:  2,
		OutputTokens: 3,
	})
	if got == nil {
		t.Fatal("expected float model pricing to produce an account stats cost")
	}

	const want float64 = 2
	if *got != want {
		t.Fatalf("account stats cost = %v, want %v", *got, want)
	}
}
