package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBatchImageHoldCommandNormalizesLedgerAmounts(t *testing.T) {
	apiKeyID := int64(42)
	job := &BatchImageJob{
		BatchID:       "batch-float-hold",
		UserID:        7,
		APIKeyID:      &apiKeyID,
		EstimatedCost: 0.123456789,
	}

	cmd, err := buildBatchImageHoldCommand(job, BatchImageCaptureRequestID(job.BatchID), 0.123456789, "payload")

	require.NoError(t, err)
	require.Equal(t, 0.12345679, cmd.HoldAmount)
	require.Equal(t, 0.12345679, cmd.ActualAmount)
}

func TestBuildBatchImageReleaseCommandNormalizesLedgerHold(t *testing.T) {
	apiKeyID := int64(42)
	job := &BatchImageJob{
		BatchID:       "batch-float-release",
		UserID:        7,
		APIKeyID:      &apiKeyID,
		EstimatedCost: 0.123456789,
	}

	cmd, err := buildBatchImageHoldCommand(job, BatchImageReleaseRequestID(job.BatchID), 0, "payload")

	require.NoError(t, err)
	require.Equal(t, 0.12345679, cmd.HoldAmount)
	require.Equal(t, 0.0, cmd.ActualAmount)
}

func TestBuildBatchImageHoldCommandRejectsInvalidLedgerAmounts(t *testing.T) {
	apiKeyID := int64(42)
	validHold := 1.0
	tests := []struct {
		name   string
		hold   float64
		actual float64
	}{
		{name: "negative hold that would round to zero", hold: -0.000000001},
		{name: "negative actual that would round to zero", hold: validHold, actual: -0.000000001},
		{name: "NaN hold", hold: math.NaN()},
		{name: "positive infinite hold", hold: math.Inf(1)},
		{name: "negative infinite actual", hold: validHold, actual: math.Inf(-1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &BatchImageJob{BatchID: "batch-invalid", UserID: 7, APIKeyID: &apiKeyID, HoldAmount: &tt.hold}

			_, err := buildBatchImageHoldCommand(job, BatchImageCaptureRequestID(job.BatchID), tt.actual, "payload")

			require.ErrorIs(t, err, ErrUsageBillingAmountInvalid)
		})
	}
}
