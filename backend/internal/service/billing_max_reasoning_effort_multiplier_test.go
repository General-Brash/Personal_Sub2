package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMaxReasoningEffortBillingMultiplierOnlyAppliesToFinalMax(t *testing.T) {
	for _, effort := range []string{"", "low", "xhigh", "medium"} {
		require.Equal(t, 1.0, maxReasoningEffortBillingMultiplier("model", effort, nil), effort)
	}
	one := 1.0
	require.Equal(t, 1.0, maxReasoningEffortBillingMultiplier("model", "max", &ModelPricing{MaxReasoningEffortMultiplier: &one}))
	three := 3.0
	require.Equal(t, 3.0, maxReasoningEffortBillingMultiplier("model", "max", &ModelPricing{MaxReasoningEffortMultiplier: &three}))
}

func TestMaxReasoningEffortBillingMultiplierRejectsInvalidRuntimeValues(t *testing.T) {
	for _, value := range []float64{0, -1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.Equal(t, 1.0, maxReasoningEffortBillingMultiplier("model", "max", &ModelPricing{MaxReasoningEffortMultiplier: &value}))
	}
}
