package service

import (
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
