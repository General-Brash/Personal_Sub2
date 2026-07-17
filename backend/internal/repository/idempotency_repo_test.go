package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIdempotencyRepoMarkSucceededUsesProcessingCAS(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rowsAffected int64
		wantErr      error
	}{
		{name: "transitioned", rowsAffected: 1},
		{name: "ownership lost", rowsAffected: 0, wantErr: sql.ErrNoRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &idempotencyRepository{sql: db}
			expiresAt := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)

			mock.ExpectExec(`(?s)UPDATE idempotency_records.*WHERE id = \$1.*AND status = \$6`).
				WithArgs(
					int64(17),
					service.IdempotencyStatusSucceeded,
					200,
					`{"ok":true}`,
					expiresAt,
					service.IdempotencyStatusProcessing,
				).
				WillReturnResult(sqlmock.NewResult(0, tc.rowsAffected))

			err = repo.MarkSucceeded(context.Background(), 17, 200, `{"ok":true}`, expiresAt)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestIdempotencyRepoMarkFailedRetryableUsesProcessingCAS(t *testing.T) {
	for _, tc := range []struct {
		name         string
		rowsAffected int64
		wantErr      error
	}{
		{name: "transitioned", rowsAffected: 1},
		{name: "late failure", rowsAffected: 0, wantErr: sql.ErrNoRows},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			repo := &idempotencyRepository{sql: db}
			lockedUntil := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
			expiresAt := lockedUntil.Add(time.Hour)

			mock.ExpectExec(`(?s)UPDATE idempotency_records.*WHERE id = \$1.*AND status = \$6`).
				WithArgs(
					int64(23),
					service.IdempotencyStatusFailedRetryable,
					"COMMIT_FAILED",
					lockedUntil,
					expiresAt,
					service.IdempotencyStatusProcessing,
				).
				WillReturnResult(sqlmock.NewResult(0, tc.rowsAffected))

			err = repo.MarkFailedRetryable(context.Background(), 23, "COMMIT_FAILED", lockedUntil, expiresAt)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestIdempotencyRepoTryReclaimOwnedComparesLockPayloadAndActor(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &idempotencyRepository{sql: db}
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	expectedLockedUntil := now.Add(-time.Second)
	newLockedUntil := now.Add(time.Minute)
	expiresAt := now.Add(time.Hour)

	mock.ExpectExec(`(?s)UPDATE idempotency_records.*AND status = \$5.*AND actor_scope = \$6.*AND request_fingerprint = \$7.*AND locked_until IS NOT DISTINCT FROM \$8`).
		WithArgs(
			int64(31),
			service.IdempotencyStatusProcessing,
			newLockedUntil,
			expiresAt,
			service.IdempotencyStatusProcessing,
			"user:42",
			"request-fingerprint",
			expectedLockedUntil,
			now,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	reclaimed, err := repo.TryReclaimOwned(
		context.Background(),
		31,
		service.IdempotencyStatusProcessing,
		"user:42",
		"request-fingerprint",
		&expectedLockedUntil,
		now,
		newLockedUntil,
		expiresAt,
	)
	require.NoError(t, err)
	require.True(t, reclaimed)
	require.NoError(t, mock.ExpectationsWereMet())
}
