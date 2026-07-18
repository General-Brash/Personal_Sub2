//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBatchImageZeroPriceHoldOperationsDoNotCallBillingRepository(t *testing.T) {
	apiKeyID := int64(7)
	zero := 0.0
	job := &BatchImageJob{
		BatchID:    "imgbatch_zero_price",
		UserID:     42,
		APIKeyID:   &apiKeyID,
		HoldAmount: &zero,
	}

	t.Run("reserve", func(t *testing.T) {
		billing := &fakeBatchImageBillingRepo{}

		err := reserveBatchImageBalanceHold(context.Background(), billing, job, "request")

		require.NoError(t, err)
		require.Empty(t, billing.reserves)
		require.Empty(t, billing.captures)
		require.Empty(t, billing.releases)
	})

	t.Run("capture", func(t *testing.T) {
		billing := &fakeBatchImageBillingRepo{}

		err := captureBatchImageBalanceHold(context.Background(), billing, job, 0, "manifest")

		require.NoError(t, err)
		require.Empty(t, billing.reserves)
		require.Empty(t, billing.captures)
		require.Empty(t, billing.releases)
	})

	t.Run("release", func(t *testing.T) {
		billing := &fakeBatchImageBillingRepo{}

		err := releaseBatchImageBalanceHold(context.Background(), billing, job, "request")

		require.NoError(t, err)
		require.Empty(t, billing.reserves)
		require.Empty(t, billing.captures)
		require.Empty(t, billing.releases)
	})
}

func TestCaptureBatchImageBalanceHoldRejectsPositiveCostWithoutHold(t *testing.T) {
	apiKeyID := int64(7)
	zero := 0.0
	job := &BatchImageJob{
		BatchID:    "imgbatch_zero_hold_positive_cost",
		UserID:     42,
		APIKeyID:   &apiKeyID,
		HoldAmount: &zero,
	}
	billing := &fakeBatchImageBillingRepo{}

	err := captureBatchImageBalanceHold(context.Background(), billing, job, 0.01, "manifest")

	require.ErrorIs(t, err, ErrBatchImageSettlementCostExceedsHold)
	require.Empty(t, billing.reserves)
	require.Empty(t, billing.captures)
	require.Empty(t, billing.releases)
}

func TestBatchImageHoldOperationsPropagateInvalidAmountsWithoutCallingRepository(t *testing.T) {
	apiKeyID := int64(7)
	invalidAmounts := []struct {
		name   string
		amount float64
	}{
		{name: "negative that rounds to zero", amount: -0.000000001},
		{name: "NaN", amount: math.NaN()},
		{name: "positive infinity", amount: math.Inf(1)},
		{name: "negative infinity", amount: math.Inf(-1)},
	}

	for _, tt := range invalidAmounts {
		t.Run("reserve "+tt.name, func(t *testing.T) {
			billing := &fakeBatchImageBillingRepo{}
			job := &BatchImageJob{BatchID: "imgbatch_invalid_reserve", UserID: 42, APIKeyID: &apiKeyID, HoldAmount: &tt.amount}

			err := reserveBatchImageBalanceHold(context.Background(), billing, job, "request")

			require.ErrorIs(t, err, ErrUsageBillingAmountInvalid)
			require.Empty(t, billing.reserves)
		})

		t.Run("capture "+tt.name, func(t *testing.T) {
			billing := &fakeBatchImageBillingRepo{}
			hold := 1.0
			job := &BatchImageJob{BatchID: "imgbatch_invalid_capture", UserID: 42, APIKeyID: &apiKeyID, HoldAmount: &hold}

			err := captureBatchImageBalanceHold(context.Background(), billing, job, tt.amount, "manifest")

			require.ErrorIs(t, err, ErrUsageBillingAmountInvalid)
			require.Empty(t, billing.captures)
		})

		t.Run("release "+tt.name, func(t *testing.T) {
			billing := &fakeBatchImageBillingRepo{}
			job := &BatchImageJob{BatchID: "imgbatch_invalid_release", UserID: 42, APIKeyID: &apiKeyID, HoldAmount: &tt.amount}

			err := releaseBatchImageBalanceHold(context.Background(), billing, job, "request")

			require.ErrorIs(t, err, ErrUsageBillingAmountInvalid)
			require.Empty(t, billing.releases)
		})
	}
}
