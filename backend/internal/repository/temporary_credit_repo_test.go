//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectTemporaryCreditUserLock(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectQuery(`(?s)SELECT id\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL\s+FOR UPDATE`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))
}

func TestTemporaryCreditRepositoryCreateGrantNeverRunsGenericDebtOffset(t *testing.T) {
	now := time.Date(2026, time.July, 21, 8, 0, 0, 0, time.UTC)
	expiresAt := time.Date(2026, time.July, 21, 16, 0, 0, 0, time.UTC)
	userID := int64(7)
	checkinID := int64(11)
	adminID := int64(9)
	tests := []struct {
		name      string
		input     service.CreateTemporaryCreditGrantInput
		checkinID any
		grantedBy any
	}{
		{
			name: "bank exchange with outstanding debt",
			input: service.CreateTemporaryCreditGrantInput{
				UserID: userID,
				Source: service.TemporaryCreditSourceBankExchange,
				Amount: 4,
			},
		},
		{
			name: "check-in with outstanding debt",
			input: service.CreateTemporaryCreditGrantInput{
				UserID:    userID,
				Source:    service.TemporaryCreditSourceCheckin,
				CheckinID: &checkinID,
				Amount:    4,
			},
			checkinID: checkinID,
		},
		{
			name: "administrator grant with outstanding debt",
			input: service.CreateTemporaryCreditGrantInput{
				UserID:    userID,
				Source:    service.TemporaryCreditSourceAdminGrant,
				Amount:    4,
				Notes:     "manual credit",
				GrantedBy: &adminID,
			},
			grantedBy: adminID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			// The grant insert is the only expected SQL. Any generic debt, loan,
			// or bank-ledger mutation makes this test fail as an unexpected call.
			mock.ExpectBegin()
			mock.ExpectQuery("INSERT INTO temporary_credit_grants").
				WithArgs(
					userID,
					tt.input.Source,
					tt.checkinID,
					"4.00000000",
					now,
					sqlmock.AnyArg(),
					tt.input.Notes,
					tt.grantedBy,
				).
				WillReturnRows(sqlmock.NewRows([]string{
					"id", "user_id", "source", "checkin_id", "amount", "remaining_amount",
					"available_at", "expires_at", "notes", "granted_by", "created_at", "updated_at",
				}).AddRow(
					int64(21), userID, string(tt.input.Source), tt.checkinID, "4.00000000", "4.00000000",
					now, expiresAt, tt.input.Notes, tt.grantedBy, now, now,
				))
			mock.ExpectCommit()

			repo := NewTemporaryCreditRepository(db)
			grant, err := service.NewTemporaryCreditServiceWithClock(repo, func() time.Time { return now }).CreateGrant(
				context.Background(),
				tt.input,
			)

			require.NoError(t, err)
			require.Equal(t, float64(4), grant.Amount)
			require.Equal(t, float64(4), grant.RemainingAmount)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestTemporaryCreditRepositoryConsumeFEFORejectsNilTransaction(t *testing.T) {
	repo := NewTemporaryCreditRepository(nil)

	_, err := repo.ConsumeFEFO(context.Background(), nil, 7, 1, temporaryCreditRepositoryTestReference())

	require.ErrorIs(t, err, service.ErrTemporaryCreditTransactionRequired)
}

func TestTemporaryCreditRepositoryConsumeFEFOWrapsQueryAndScanFailuresByStage(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		expectTemporaryCreditUserLock(mock, 7)
		mock.ExpectQuery("SELECT id, remaining_amount").WillReturnError(errors.New("query failed"))
		mock.ExpectRollback()

		_, err = NewTemporaryCreditRepository(db).ConsumeFEFO(context.Background(), tx, 7, 1, temporaryCreditRepositoryTestReference())

		require.ErrorContains(t, err, "query FEFO temporary credit grants")
		require.ErrorContains(t, err, "query failed")
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("scan", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		expectTemporaryCreditUserLock(mock, 7)
		mock.ExpectQuery("SELECT id, remaining_amount").WillReturnRows(
			sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow("not-an-id", "1.00000000"),
		)
		mock.ExpectRollback()

		_, err = NewTemporaryCreditRepository(db).ConsumeFEFO(context.Background(), tx, 7, 1, temporaryCreditRepositoryTestReference())

		require.ErrorContains(t, err, "scan FEFO temporary credit grant")
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestTemporaryCreditRepositoryConsumeFEFOWrapsUpdateAndInsertFailuresByStage(t *testing.T) {
	t.Run("update", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		expectTemporaryCreditUserLock(mock, 7)
		mock.ExpectQuery("SELECT id, remaining_amount").WillReturnRows(
			sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(11, "1.00000000"),
		)
		mock.ExpectQuery("UPDATE temporary_credit_grants").
			WithArgs("1.00000000", int64(11)).
			WillReturnError(errors.New("update failed"))
		mock.ExpectRollback()

		_, err = NewTemporaryCreditRepository(db).ConsumeFEFO(context.Background(), tx, 7, 1, temporaryCreditRepositoryTestReference())

		require.ErrorContains(t, err, "update FEFO temporary credit grant 11")
		require.ErrorContains(t, err, "update failed")
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("insert", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		defer func() { _ = db.Close() }()

		mock.ExpectBegin()
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		expectTemporaryCreditUserLock(mock, 7)
		mock.ExpectQuery("SELECT id, remaining_amount").WillReturnRows(
			sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(11, "1.00000000"),
		)
		mock.ExpectQuery("UPDATE temporary_credit_grants").
			WithArgs("1.00000000", int64(11)).
			WillReturnRows(sqlmock.NewRows([]string{"amount"}).AddRow("1.00000000"))
		mock.ExpectExec("INSERT INTO temporary_credit_consumptions").
			WithArgs(int64(11), nil, "request-1", "1.00000000").
			WillReturnError(errors.New("insert failed"))
		mock.ExpectRollback()

		_, err = NewTemporaryCreditRepository(db).ConsumeFEFO(context.Background(), tx, 7, 1, temporaryCreditRepositoryTestReference())

		require.ErrorContains(t, err, "insert FEFO temporary credit consumption for grant 11")
		require.ErrorContains(t, err, "insert failed")
		require.NoError(t, tx.Rollback())
		require.NoError(t, mock.ExpectationsWereMet())
	})
}

func temporaryCreditRepositoryTestReference() service.TemporaryCreditConsumptionReference {
	return service.TemporaryCreditConsumptionReference{RequestID: "request-1"}
}
