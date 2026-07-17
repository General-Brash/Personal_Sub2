//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

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
