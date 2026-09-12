package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCalculateBankExchangeRefundFixed8B01(t *testing.T) {
	result, err := calculateBankExchangeRefundFixed8("10.00000000", "8.00000000", "20.00000000", 1000)
	require.NoError(t, err)
	require.Equal(t, "8.00000000", result.ExpiredRemaining)
	require.Equal(t, "4.00000000", result.RefundablePrincipal)
	require.Equal(t, "0.40000000", result.FeeAmount)
	require.Equal(t, "3.60000000", result.NetRefund)
}

func TestCalculateBankExchangeRefundFixed8Bounds(t *testing.T) {
	depleted, err := calculateBankExchangeRefundFixed8("10.00000000", "0.00000000", "20.00000000", 1000)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", depleted.NetRefund)

	noFee, err := calculateBankExchangeRefundFixed8("10.00000000", "20.00000000", "20.00000000", 0)
	require.NoError(t, err)
	require.Equal(t, "10.00000000", noFee.NetRefund)
	require.Equal(t, "0.00000000", noFee.FeeAmount)

	fullFee, err := calculateBankExchangeRefundFixed8("10.00000000", "20.00000000", "20.00000000", 10000)
	require.NoError(t, err)
	require.Equal(t, "0.00000000", fullFee.NetRefund)
	require.Equal(t, "10.00000000", fullFee.FeeAmount)
}

func TestCalculateBankExchangeRefundFixed8UsesFrozenRatio(t *testing.T) {
	result, err := calculateBankExchangeRefundFixed8("3.00000000", "2.00000000", "6.00000000", 1000)
	require.NoError(t, err)
	require.Equal(t, "1.00000000", result.RefundablePrincipal)
	require.Equal(t, "0.10000000", result.FeeAmount)
	require.Equal(t, "0.90000000", result.NetRefund)
}

func TestNextBankExchangeExpiryUsesShanghaiBoundary(t *testing.T) {
	before, err := nextBankExchangeExpiry(time.Date(2026, 9, 11, 23, 59, 59, 0, time.FixedZone("CST", 8*60*60)), "00:00")
	require.NoError(t, err)
	require.Equal(t, "2026-09-12T00:00:00+08:00", before.Format(time.RFC3339))

	after, err := nextBankExchangeExpiry(time.Date(2026, 9, 12, 0, 0, 0, 0, time.FixedZone("CST", 8*60*60)), "00:00")
	require.NoError(t, err)
	require.Equal(t, "2026-09-13T00:00:00+08:00", after.Format(time.RFC3339))
}

func TestValidateBankExchangeConfirmationRequiresVersionOnlyWhenEnabled(t *testing.T) {
	policy := DefaultBankPolicy()
	expectedExpiry := time.Date(2026, 9, 13, 0, 0, 0, 0, beijingLocation)

	require.NoError(t, validateBankExchangeConfirmation(policy, BankExchangeConfirmation{}, expectedExpiry))

	policy.exchangeExpiryRefundEnabled = true
	require.ErrorIs(t, validateBankExchangeConfirmation(policy, BankExchangeConfirmation{}, expectedExpiry), ErrBankExchangeExpiryPolicyVersionRequired)
	require.ErrorIs(t, validateBankExchangeConfirmation(policy, BankExchangeConfirmation{PolicyVersion: policy.exchangeExpiryPolicyVersion + 1}, expectedExpiry), ErrBankExchangeExpiryPolicyVersionConflict)
	require.NoError(t, validateBankExchangeConfirmation(policy, BankExchangeConfirmation{PolicyVersion: policy.exchangeExpiryPolicyVersion}, expectedExpiry))
}

func TestValidateBankExchangeConfirmationChecksQuoteFeeAndExpiry(t *testing.T) {
	policy := DefaultBankPolicy()
	policy.exchangeExpiryRefundEnabled = true
	expectedExpiry := time.Date(2026, 9, 13, 0, 0, 0, 0, beijingLocation)
	quote := &BankExchangeQuoteConfirmation{
		PolicyVersion: policy.exchangeExpiryPolicyVersion,
		FeeBPS:        policy.exchangeExpiryRefundFeeBPS,
		ExpiresAt:     expectedExpiry.Format(time.RFC3339),
	}
	require.NoError(t, validateBankExchangeConfirmation(policy, BankExchangeConfirmation{Quote: quote}, expectedExpiry))

	quote.FeeBPS = 500
	require.ErrorIs(t, validateBankExchangeConfirmation(policy, BankExchangeConfirmation{Quote: quote}, expectedExpiry), ErrBankExchangeExpiryQuoteConflict)

	quote.FeeBPS = policy.exchangeExpiryRefundFeeBPS
	quote.ExpiresAt = expectedExpiry.Add(time.Hour).Format(time.RFC3339)
	require.ErrorIs(t, validateBankExchangeConfirmation(policy, BankExchangeConfirmation{Quote: quote}, expectedExpiry), ErrBankExchangeExpiryQuoteConflict)
}
