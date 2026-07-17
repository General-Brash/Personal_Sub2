package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseStrictSignedLedgerAmount_RejectsScientificNotation(t *testing.T) {
	amount, err := ParseStrictSignedLedgerAmount("-0.125")
	require.NoError(t, err)
	require.InDelta(t, -0.125, amount, ledgerAmountEpsilon)

	_, err = ParseStrictSignedLedgerAmount("-1e-3")
	require.Error(t, err)
}

func TestNormalizeLedgerAmount_RoundsAndClampsResiduals(t *testing.T) {
	amount, err := normalizeLedgerAmount(0.30000000000000004)
	require.NoError(t, err)
	require.Equal(t, 0.3, amount)

	amount, err = normalizeLedgerAmount(0.000000004)
	require.NoError(t, err)
	require.Zero(t, amount)
	require.Equal(t, "0.30000000", formatLedgerAmount(0.30000000000000004))
}

func TestNormalizeDerivedLedgerAmount_AllowsTotalsAboveSingleAmountLimit(t *testing.T) {
	const aggregate = 1250000000000.125

	normalized, err := normalizeDerivedLedgerAmount(aggregate)

	require.NoError(t, err)
	require.Equal(t, aggregate, normalized)
	require.Equal(t, "1250000000000.12500000", formatLedgerAmount(aggregate))
	_, err = normalizeLedgerAmount(aggregate)
	require.Error(t, err)
}

func TestNormalizeLedgerAmount_RejectsNonFiniteAndNumericOverflow(t *testing.T) {
	for _, amount := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), maxLedgerAmount} {
		_, err := normalizeLedgerAmount(amount)
		require.Error(t, err)
	}
	require.Equal(t, "NaN", formatLedgerAmount(math.NaN()))

	_, err := ParseStrictLedgerAmount("999999999999.99999999")
	require.Error(t, err)
}
