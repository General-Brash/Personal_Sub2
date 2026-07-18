//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestLockBatchImageTemporaryCreditCandidatesUsesDatabaseClockAtLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	expiresAt := time.Now().Add(time.Hour)
	mock.ExpectQuery(`(?s)SELECT id, remaining_amount, expires_at.*expires_at > clock_timestamp\(\).*FOR UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount", "expires_at"}).AddRow(int64(7), 1.0, expiresAt))
	mock.ExpectRollback()

	candidates, err := lockBatchImageTemporaryCreditCandidates(context.Background(), tx, 42, 1)

	require.NoError(t, err)
	require.Equal(t, []batchImageTemporaryCreditCandidate{{GrantID: 7, Available: 1, ExpiresAt: expiresAt}}, candidates)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSplitBatchImageCapturedAmountsUsesTemporaryCreditFirst(t *testing.T) {
	tests := []struct {
		name          string
		hold          batchImageCreditHoldRecord
		actual        float64
		wantTemporary float64
		wantPermanent float64
	}{
		{
			name:          "temporary-only",
			hold:          batchImageCreditHoldRecord{HoldAmount: 1, TemporaryReservedAmount: 1},
			actual:        0.6,
			wantTemporary: 0.6,
		},
		{
			name:          "mixed uses all temporary before permanent",
			hold:          batchImageCreditHoldRecord{HoldAmount: 1, TemporaryReservedAmount: 0.8, PermanentReservedAmount: 0.2},
			actual:        1,
			wantTemporary: 0.8,
			wantPermanent: 0.2,
		},
		{
			name:          "partial mixed capture does not touch permanent",
			hold:          batchImageCreditHoldRecord{HoldAmount: 1, TemporaryReservedAmount: 0.8, PermanentReservedAmount: 0.2},
			actual:        0.5,
			wantTemporary: 0.5,
		},
		{
			name:          "permanent-only",
			hold:          batchImageCreditHoldRecord{HoldAmount: 1, PermanentReservedAmount: 1},
			actual:        0.6,
			wantPermanent: 0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			temporary, permanent, err := splitBatchImageCapturedAmounts(tt.actual, &tt.hold)
			require.NoError(t, err)
			require.Equal(t, tt.wantTemporary, temporary)
			require.Equal(t, tt.wantPermanent, permanent)
		})
	}
}

func TestSplitBatchImageCapturedAmountsRejectsCostOverHold(t *testing.T) {
	hold := &batchImageCreditHoldRecord{HoldAmount: 1, TemporaryReservedAmount: 0.8, PermanentReservedAmount: 0.2}

	_, _, err := splitBatchImageCapturedAmounts(1.00000001, hold)

	require.ErrorIs(t, err, service.ErrBatchImageSettlementCostExceedsHold)
}

func TestBatchImageCreditHoldResultDerivesTerminalTemporaryRefund(t *testing.T) {
	result := batchImageCreditHoldResult(&batchImageCreditHoldRecord{
		Status:                  "captured",
		TemporaryReservedAmount: 1,
		TemporaryCapturedAmount: 0.4,
		ExpiredUnrestoredAmount: 0.1,
	}, false)

	require.False(t, result.Applied)
	require.Equal(t, 0.4, result.TemporaryCapturedAmount)
	require.Equal(t, 0.5, result.TemporaryRefundedAmount)
	require.Equal(t, 0.1, result.TemporaryExpiredAmount)
}
