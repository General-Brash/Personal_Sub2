//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveVideoRateMultiplier_Float64(t *testing.T) {
	const base = 1.25

	var _ func(*APIKey, float64) float64 = resolveVideoRateMultiplier

	tests := []struct {
		name   string
		apiKey *APIKey
		want   float64
	}{
		{"default keeps base", nil, base},
		{"independent uses group value", &APIKey{Group: &Group{VideoRateIndependent: true, VideoRateMultiplier: 0.5}}, 0.5},
		{"negative independent clamps to zero", &APIKey{Group: &Group{VideoRateIndependent: true, VideoRateMultiplier: -0.5}}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, resolveVideoRateMultiplier(tt.apiKey, base))
		})
	}
}

// TestCalculateCost_RateMultiplier_NegativeClampedToZero 锁定负数倍率被
// 钳制为 0（而非历史上的 1.0），避免配置异常导致静默按标准价扣费。
func TestCalculateCost_RateMultiplier_NegativeClampedToZero(t *testing.T) {
	svc := newTestBillingService()
	tokens := UsageTokens{InputTokens: 1000, OutputTokens: 500}

	tests := []struct {
		name       string
		multiplier float64
		wantRatio  float64 // ActualCost / TotalCost
	}{
		{"negative clamped to 0", -1.5, 0},
		{"zero passes through as 0 (defense in depth)", 0, 0},
		{"positive 2x applied", 2.0, 2.0},
		{"positive 0.5x applied", 0.5, 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost, err := svc.CalculateCost("claude-sonnet-4", tokens, tt.multiplier)
			require.NoError(t, err)
			require.Greater(t, cost.TotalCost, 0.0, "TotalCost should be non-zero")
			require.InDelta(t, tt.wantRatio*cost.TotalCost, cost.ActualCost, 1e-9)
		})
	}
}

// TestCalculateImageCost_RateMultiplier_NegativeClampedToZero 图片按次计费路径
// 同样遵循"负数 → 0"语义。
func TestCalculateImageCost_RateMultiplier_NegativeClampedToZero(t *testing.T) {
	svc := newTestBillingService()
	price := 0.04
	cfg := &ImagePriceConfig{Price1K: &price}

	tests := []struct {
		name       string
		multiplier float64
		wantRatio  float64
	}{
		{"negative clamped to 0", -0.5, 0},
		{"zero passes through", 0, 0},
		{"positive 3x applied", 3.0, 3.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cost := svc.CalculateImageCost("imagen-3", "1K", 2, cfg, tt.multiplier)
			require.NotNil(t, cost)
			require.Greater(t, cost.TotalCost, 0.0)
			require.InDelta(t, tt.wantRatio*cost.TotalCost, cost.ActualCost, 1e-9)
		})
	}
}
