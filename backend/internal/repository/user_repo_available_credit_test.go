package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryGetAvailableCreditSnapshotUsesClockTimestampFilteredTemporaryCredit(t *testing.T) {
	repo, mock := newRedeemAdjustmentRepoMock(t)
	earliestExpiry := time.Now().UTC().Add(90 * time.Second)
	mock.ExpectQuery(`(?s)SELECT u\.balance.*g\.remaining_amount > 0.*g\.expires_at > clock_timestamp\(\).*WHERE u\.id = \$1.*u\.deleted_at IS NULL.*GROUP BY u\.id, u\.balance`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit", "earliest_expiry"}).
			AddRow("2.50000000", "1.25000000", earliestExpiry))

	snapshot, err := repo.GetAvailableCreditSnapshot(context.Background(), 42)

	require.NoError(t, err)
	require.InDelta(t, 2.5, snapshot.PermanentBalance, 1e-12)
	require.InDelta(t, 1.25, snapshot.TemporaryCredit, 1e-12)
	require.NotNil(t, snapshot.EarliestTemporaryCreditExpiry)
	require.WithinDuration(t, earliestExpiry, *snapshot.EarliestTemporaryCreditExpiry, time.Second)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestNewUserRepositoryWithNilDBKeepsSQLExecutorNil(t *testing.T) {
	repo, ok := NewUserRepository(nil, nil).(*userRepository)
	require.True(t, ok)
	require.Nil(t, repo.sql)

	_, err := repo.GetAvailableCreditSnapshot(context.Background(), 42)
	require.ErrorContains(t, err, "sql executor is nil")
}
