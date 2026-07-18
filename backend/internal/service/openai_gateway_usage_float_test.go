package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestOpenAIGatewayUsageTokenCostUsesLegacyFloatMultiplier(t *testing.T) {
	t.Parallel()

	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}
	multiplier := 1.0 / 3.0
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}

	want, err := svc.billingService.CalculateCostWithServiceTier("gpt-5.1", tokens, multiplier, "")
	require.NoError(t, err)

	got, err := svc.calculateOpenAIRecordUsageTokenCost(context.Background(), &APIKey{}, "gpt-5.1", multiplier, tokens, "", false)
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestOpenAIGatewayUsageImageCostUsesLegacyFloatMultiplier(t *testing.T) {
	t.Parallel()

	unitPrice := 0.1
	multiplier := 1.0 / 3.0
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}

	got, err := svc.calculateOpenAIImageCost(
		context.Background(),
		"gpt-image-1",
		&APIKey{Group: &Group{ImagePrice1K: &unitPrice}},
		&OpenAIForwardResult{ImageCount: 3, ImageSize: "1K"},
		multiplier,
	)

	require.NoError(t, err)
	require.InDelta(t, 0.3, got.TotalCost, 1e-12)
	require.InDelta(t, 0.1, got.ActualCost, 1e-12)
}

func TestOpenAIGatewayUsageVideoCostUsesLegacyFloatMultiplier(t *testing.T) {
	t.Parallel()

	unitPrice := 0.1
	multiplier := 1.0 / 3.0
	svc := &OpenAIGatewayService{billingService: NewBillingService(&config.Config{}, nil)}

	got, err := svc.calculateOpenAIVideoCost(
		context.Background(),
		"grok-imagine-video",
		&APIKey{Group: &Group{VideoPrice480P: &unitPrice}},
		&OpenAIForwardResult{VideoCount: 1, VideoResolution: VideoBillingResolution480P, VideoDurationSeconds: 5},
		multiplier,
	)

	require.NoError(t, err)
	require.InDelta(t, 0.5, got.TotalCost, 1e-12)
	require.InDelta(t, 1.0/6.0, got.ActualCost, 1e-12)
}

func TestOpenAIUsageDefaultRateMultiplierUsesLegacyFloatConfig(t *testing.T) {
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)

	err := svc.RecordUsage(context.Background(), &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "legacy-float-default-rate",
			Usage:     OpenAIUsage{InputTokens: 8, OutputTokens: 4},
			Model:     "gpt-5.1",
			Duration:  time.Second,
		},
		APIKey:  &APIKey{ID: 91},
		User:    &User{ID: 92},
		Account: &Account{ID: 93},
	})

	require.NoError(t, err)
	require.NotNil(t, usageRepo.lastLog)
	require.InDelta(t, 1.1, usageRepo.lastLog.RateMultiplier, 1e-12)
}

func TestOpenAIUsageBillingRoundsFloatAtLedgerBoundary(t *testing.T) {
	require.Equal(t, "0.33333333", formatLedgerAmount(usageBillingLedgerAmountFromFloat64(1.0/3.0)))
}
