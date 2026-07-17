package service

import (
	"context"
	"math"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestLegacyMediaAndRateContractsUseFloat64(t *testing.T) {
	var _ func(*BillingService, int, *float64, float64) *CostBreakdown = (*BillingService).CalculateWebSearchCost
	var _ func(*BillingService, string, string, int, *ImagePriceConfig, float64) *CostBreakdown = (*BillingService).CalculateImageCost
	var _ func(*BillingService, string, string, int, int, *VideoPriceConfig, float64) *CostBreakdown = (*BillingService).CalculateVideoCost
	var _ func(*userGroupRateResolver, context.Context, int64, int64, float64) float64 = (*userGroupRateResolver).Resolve
	var _ func(*APIKey, float64) float64 = resolveImageRateMultiplier
	var _ func(*APIKey, float64) float64 = resolveVideoRateMultiplier

	price := 0.02
	cost := (&BillingService{}).CalculateWebSearchCost(1, &price, 2.5)
	if got := cost.TotalCost; math.Abs(got-0.02) > 1e-12 {
		t.Fatalf("total cost = %v, want 0.02", got)
	}
	if got := cost.ActualCost; math.Abs(got-0.05) > 1e-12 {
		t.Fatalf("actual cost = %v, want 0.05", got)
	}

	svc := NewBillingService(&config.Config{Default: config.DefaultConfig{RateMultiplier: 1.5}}, nil)
	if _, err := svc.CalculateCostWithConfig("claude-sonnet-4", UsageTokens{InputTokens: 1}); err != nil {
		t.Fatalf("float64 default multiplier should remain usable: %v", err)
	}
}

func TestLegacyMediaRateMultiplierResolutionUsesFloat64(t *testing.T) {
	const base = 1.25

	tests := []struct {
		name     string
		resolver func(*APIKey, float64) float64
		apiKey   *APIKey
		want     float64
	}{
		{"image nil API key", resolveImageRateMultiplier, nil, base},
		{"image group default", resolveImageRateMultiplier, &APIKey{Group: &Group{}}, base},
		{"image independent", resolveImageRateMultiplier, &APIKey{Group: &Group{ImageRateIndependent: true, ImageRateMultiplier: 0.5}}, 0.5},
		{"image negative independent", resolveImageRateMultiplier, &APIKey{Group: &Group{ImageRateIndependent: true, ImageRateMultiplier: -0.5}}, 0},
		{"video nil API key", resolveVideoRateMultiplier, nil, base},
		{"video group default", resolveVideoRateMultiplier, &APIKey{Group: &Group{}}, base},
		{"video independent", resolveVideoRateMultiplier, &APIKey{Group: &Group{VideoRateIndependent: true, VideoRateMultiplier: 0.5}}, 0.5},
		{"video negative independent", resolveVideoRateMultiplier, &APIKey{Group: &Group{VideoRateIndependent: true, VideoRateMultiplier: -0.5}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resolver(tt.apiKey, base); got != tt.want {
				t.Fatalf("multiplier = %v, want %v", got, tt.want)
			}
		})
	}
}
