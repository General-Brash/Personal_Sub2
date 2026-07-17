package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermanentBalanceCacheDeductionUsesExplicitRemainder(t *testing.T) {
	fullTemporaryCoverage := 0.0
	partialTemporaryCoverage := 0.5

	amount, known := permanentBalanceCacheDeduction(&UsageBillingApplyResult{
		PermanentBalanceDeduction: &fullTemporaryCoverage,
	})
	require.True(t, known)
	require.Zero(t, amount)

	amount, known = permanentBalanceCacheDeduction(&UsageBillingApplyResult{
		PermanentBalanceDeduction: &partialTemporaryCoverage,
	})
	require.True(t, known)
	require.Equal(t, 0.5, amount)

	_, known = permanentBalanceCacheDeduction(&UsageBillingApplyResult{})
	require.False(t, known)
}

func TestPermanentBalanceNotificationInputsUseOnlyPermanentRemainder(t *testing.T) {
	fallbackBalance := 10.0
	fullTemporaryCoverage := 0.0

	_, _, shouldNotify := permanentBalanceNotificationInputs(&UsageBillingApplyResult{
		PermanentBalanceDeduction: &fullTemporaryCoverage,
	}, fallbackBalance)
	require.False(t, shouldNotify)

	_, _, shouldNotify = permanentBalanceNotificationInputs(&UsageBillingApplyResult{}, fallbackBalance)
	require.False(t, shouldNotify)

	newBalance := 9.5
	partialTemporaryCoverage := 0.5
	oldBalance, deduction, shouldNotify := permanentBalanceNotificationInputs(&UsageBillingApplyResult{
		NewBalance:                &newBalance,
		PermanentBalanceDeduction: &partialTemporaryCoverage,
	}, fallbackBalance)
	require.True(t, shouldNotify)
	require.Equal(t, 10.0, oldBalance)
	require.Equal(t, 0.5, deduction)
}
