package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildBatchImageHoldCommandPreservesLegacyFloatAmounts(t *testing.T) {
	apiKeyID := int64(42)
	job := &BatchImageJob{
		BatchID:       "batch-float-hold",
		UserID:        7,
		APIKeyID:      &apiKeyID,
		EstimatedCost: 0.123456789,
	}

	cmd, err := buildBatchImageHoldCommand(job, BatchImageCaptureRequestID(job.BatchID), 0.123456789, "payload")

	require.NoError(t, err)
	require.Equal(t, 0.123456789, cmd.HoldAmount)
	require.Equal(t, 0.123456789, cmd.ActualAmount)
}

func TestBuildBatchImageReleaseCommandPreservesLegacyFloatHold(t *testing.T) {
	apiKeyID := int64(42)
	job := &BatchImageJob{
		BatchID:       "batch-float-release",
		UserID:        7,
		APIKeyID:      &apiKeyID,
		EstimatedCost: 0.123456789,
	}

	cmd, err := buildBatchImageHoldCommand(job, BatchImageReleaseRequestID(job.BatchID), 0, "payload")

	require.NoError(t, err)
	require.Equal(t, 0.123456789, cmd.HoldAmount)
	require.Equal(t, 0.0, cmd.ActualAmount)
}
