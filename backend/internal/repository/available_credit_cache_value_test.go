package repository

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailableCreditCacheValueUsesFixedEightDecimalPlaces(t *testing.T) {
	raw, err := formatAvailableCreditCacheValue(1.25)
	require.NoError(t, err)
	require.Equal(t, "1.25000000", raw)

	value, err := parseAvailableCreditCacheValue(raw)
	require.NoError(t, err)
	require.InDelta(t, 1.25, value, 1e-12)
}

func TestParseAvailableCreditCacheValueRejectsNonFrozenValues(t *testing.T) {
	for _, raw := range []string{"1.25", "1.000000001", "1e0", "+1.00000000", " 1.00000000"} {
		t.Run(raw, func(t *testing.T) {
			_, err := parseAvailableCreditCacheValue(raw)
			require.Error(t, err)
		})
	}
}

func TestFormatAvailableCreditCacheValueRejectsInvalidValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := formatAvailableCreditCacheValue(value)
		require.Error(t, err)
	}
}

func TestAvailableCreditCacheValueRoundTripsCumulativeValueAboveSingleAmountLimit(t *testing.T) {
	const cumulative = 1_250_000_000_000.125

	raw, err := formatAvailableCreditCacheValue(cumulative)
	require.NoError(t, err)
	require.Equal(t, "1250000000000.12500000", raw)

	value, err := parseAvailableCreditCacheValue(raw)
	require.NoError(t, err)
	require.InDelta(t, cumulative, value, 0.001)
}
