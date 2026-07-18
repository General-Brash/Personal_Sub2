package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUsageBillingCommandUsesFloat64Amounts(t *testing.T) {
	commandType := reflect.TypeOf(UsageBillingCommand{})
	floatType := reflect.TypeOf(float64(0))

	for _, fieldName := range []string{
		"BalanceCost",
		"SubscriptionCost",
		"APIKeyQuotaCost",
		"APIKeyRateLimitCost",
		"AccountQuotaCost",
	} {
		field, ok := commandType.FieldByName(fieldName)
		if !ok {
			t.Fatalf("UsageBillingCommand missing %s", fieldName)
		}
		require.Equalf(t, floatType, field.Type, "UsageBillingCommand.%s must use float64", fieldName)
	}
}

func TestUsageBillingCommandNormalizesAmountsToEightDecimals(t *testing.T) {
	command := UsageBillingCommand{
		BalanceCost:         1.234567896,
		SubscriptionCost:    2.000000004,
		APIKeyQuotaCost:     3.111111114,
		APIKeyRateLimitCost: 4.222222226,
		AccountQuotaCost:    5.333333334,
	}

	err := command.Normalize()

	require.NoError(t, err)
	require.Equal(t, "1.23456790", formatLedgerAmount(command.BalanceCost))
	require.Equal(t, "2.00000000", formatLedgerAmount(command.SubscriptionCost))
}

func TestUsageBillingLedgerAmountFromFloat64RoundsOnceBeforeCommand(t *testing.T) {
	amount := usageBillingLedgerAmountFromFloat64(1.234567891)

	require.Equal(t, "1.23456789", formatLedgerAmount(amount))
}

func TestBuildUsageBillingCommandQuantizesLegacyCostBeforeLedger(t *testing.T) {
	command := buildUsageBillingCommand("legacy-float-cost", nil, &postUsageBillingParams{
		Cost: &CostBreakdown{
			TotalCost:  1.234567891,
			ActualCost: 1.234567891,
		},
		User:    &User{ID: 1},
		APIKey:  &APIKey{ID: 2},
		Account: &Account{ID: 3},
	})

	require.NotNil(t, command)
	require.Equal(t, "1.23456789", formatLedgerAmount(command.BalanceCost))
}

func TestBatchImageBalanceHoldCommandNormalizesAmountsToEightDecimals(t *testing.T) {
	command := BatchImageBalanceHoldCommand{
		RequestID:    " batch_image_hold:batch-precision ",
		UserID:       1,
		APIKeyID:     2,
		BatchID:      " batch-precision ",
		HoldAmount:   1.123456789,
		ActualAmount: 0.876543215,
	}

	err := command.Normalize()

	require.NoError(t, err)
	require.Equal(t, "batch_image_hold:batch-precision", command.RequestID)
	require.Equal(t, "batch-precision", command.BatchID)
	require.Equal(t, "1.12345679", formatLedgerAmount(command.HoldAmount))
	require.Equal(t, "0.87654322", formatLedgerAmount(command.ActualAmount))
	require.NotEmpty(t, command.RequestFingerprint)
}

func TestBatchImageBalanceHoldCommandRejectsInvalidAmounts(t *testing.T) {
	for _, amount := range []float64{-0.000000001, -0.00000001, math.NaN(), math.Inf(1), math.Inf(-1), maxLedgerAmount} {
		command := BatchImageBalanceHoldCommand{HoldAmount: amount}
		err := command.Normalize()
		require.ErrorIs(t, err, ErrUsageBillingAmountInvalid, "amount=%v", amount)
	}
}

func TestBatchImageBalanceHoldFingerprintAcceptsLegacyTenDecimalRetry(t *testing.T) {
	command := BatchImageBalanceHoldCommand{
		UserID:             11,
		APIKeyID:           22,
		BatchID:            "batch-legacy-fingerprint",
		HoldAmount:         1.234567891,
		ActualAmount:       0.765432109,
		RequestPayloadHash: "payload",
	}
	legacyRaw := fmt.Sprintf("%d|%d|%s|%0.10f|%0.10f|%s", command.UserID, command.APIKeyID, command.BatchID, command.HoldAmount, command.ActualAmount, command.RequestPayloadHash)
	legacySum := sha256.Sum256([]byte(legacyRaw))
	legacyFingerprint := hex.EncodeToString(legacySum[:])
	require.NoError(t, command.Normalize())

	require.True(t, MatchesBatchImageBalanceHoldFingerprint(command.RequestFingerprint, &command))
	require.True(t, MatchesBatchImageBalanceHoldFingerprint(legacyFingerprint, &command))
	require.False(t, MatchesBatchImageBalanceHoldFingerprint("different", &command))
}
