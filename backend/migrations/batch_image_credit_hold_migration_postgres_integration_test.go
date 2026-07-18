//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const batchImageCreditHoldMigration = "183_personal_batch_image_credit_holds.sql"

func TestBatchImageCreditHoldMigration_PostgresForwardConstraintsAndReplay(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	db := newMigrationTestPostgres(t, ctx)
	require.NoError(t, createBatchImageCreditHoldPrerequisites(ctx, db))
	fixture := seedBatchImageCreditHoldFixture(t, ctx, db)
	require.NoError(t, seedMigrationHistoryExcept(ctx, db, batchImageCreditHoldMigration))

	require.NoError(t, repository.ApplyMigrations(ctx, db))

	// The legacy active job remains untouched. Its aggregate frozen balance has
	// no reliable per-grant provenance and must be handled by business fallback.
	var legacyHoldRows int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM batch_image_credit_holds
WHERE batch_id = $1`, fixture.legacyBatchID).Scan(&legacyHoldRows))
	require.Zero(t, legacyHoldRows)

	var legacyStatus, legacyHoldID string
	var legacyHoldAmount float64
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT status, hold_id, hold_amount
FROM batch_image_jobs
WHERE batch_id = $1`, fixture.legacyBatchID).Scan(&legacyStatus, &legacyHoldID, &legacyHoldAmount))
	require.Equal(t, "submitted", legacyStatus)
	require.Equal(t, "legacy-hold", legacyHoldID)
	require.InDelta(t, 5, legacyHoldAmount, 0.00000001)

	holdID := insertReservedBatchImageCreditHold(t, ctx, db, fixture)
	insertReservedBatchImageAllocation(t, ctx, db, holdID, fixture.batchID, fixture.grantIDs[0], 4)
	insertReservedBatchImageAllocation(t, ctx, db, holdID, fixture.batchID, fixture.grantIDs[1], 2)

	_, err := db.ExecContext(ctx, `
INSERT INTO batch_image_credit_holds (
    batch_id, user_id, api_key_id, status, hold_amount,
    temporary_reserved_amount, permanent_reserved_amount, reserve_fingerprint
) VALUES ($1, $2, $3, 'reserved', 10, 5, 4, 'bad-conservation')`,
		fixture.invalidBatchID, fixture.userID, fixture.apiKeyID)
	requirePostgresCode(t, err, "23514")

	_, err = db.ExecContext(ctx, `
INSERT INTO batch_image_credit_hold_allocations (
    hold_id, batch_id, grant_id, grant_expires_at, reserved_amount
) SELECT $1, $2, $3, expires_at, 1
  FROM temporary_credit_grants
 WHERE id = $3`, holdID, fixture.invalidBatchID, fixture.grantIDs[2])
	requirePostgresCode(t, err, "23503")

	_, err = db.ExecContext(ctx, `
INSERT INTO batch_image_credit_hold_allocations (
    hold_id, batch_id, grant_id, grant_expires_at, reserved_amount
) SELECT $1, $2, $3, expires_at, 1
  FROM temporary_credit_grants
 WHERE id = $3`, holdID, fixture.batchID, fixture.grantIDs[0])
	requirePostgresCode(t, err, "23505")

	_, err = db.ExecContext(ctx, `
UPDATE batch_image_credit_hold_allocations
SET captured_amount = 1, updated_at = NOW()
WHERE hold_id = $1 AND grant_id = $2`, holdID, fixture.grantIDs[0])
	requirePostgresCode(t, err, "23514")

	_, err = db.ExecContext(ctx, `
UPDATE batch_image_credit_holds
SET status = 'released',
    captured_amount = 1,
    permanent_captured_amount = 1,
    terminal_fingerprint = 'invalid-release',
    settled_at = NOW(),
    updated_at = NOW()
WHERE id = $1`, holdID)
	requirePostgresCode(t, err, "23514")

	_, err = db.ExecContext(ctx, `
UPDATE batch_image_credit_hold_allocations
SET captured_amount = 3, refunded_amount = 1, updated_at = NOW()
WHERE hold_id = $1 AND grant_id = $2`, holdID, fixture.grantIDs[0])
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
UPDATE batch_image_credit_hold_allocations
SET captured_amount = 1, expired_amount = 1, updated_at = NOW()
WHERE hold_id = $1 AND grant_id = $2`, holdID, fixture.grantIDs[1])
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
UPDATE batch_image_credit_holds
SET status = 'captured',
    captured_amount = 7,
    temporary_captured_amount = 4,
    permanent_captured_amount = 3,
    expired_unrestored_amount = 1,
    terminal_fingerprint = 'capture-fingerprint',
    settled_at = NOW(),
    updated_at = NOW()
WHERE id = $1`, holdID)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `DELETE FROM groups WHERE id = $1`, fixture.groupID)
	require.NoError(t, err)
	var groupID sql.NullInt64
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT group_id
FROM batch_image_credit_holds
WHERE id = $1`, holdID).Scan(&groupID))
	require.False(t, groupID.Valid, "group deletion must preserve the hold and clear only its snapshot FK")

	content, err := fs.ReadFile(migrations.FS, batchImageCreditHoldMigration)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(content))
	require.NoError(t, err, "migration must be safe to replay directly")
	require.NoError(t, repository.ApplyMigrations(ctx, db))

	var applied int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schema_migrations
WHERE filename = $1`, batchImageCreditHoldMigration).Scan(&applied))
	require.Equal(t, 1, applied)
}

type batchImageCreditHoldFixture struct {
	userID         int64
	apiKeyID       int64
	groupID        int64
	grantIDs       [3]int64
	batchID        string
	invalidBatchID string
	legacyBatchID  string
}

func createBatchImageCreditHoldPrerequisites(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY
);

CREATE TABLE api_keys (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT
);

CREATE TABLE groups (
    id BIGSERIAL PRIMARY KEY
);

CREATE TABLE batch_image_jobs (
    id BIGSERIAL PRIMARY KEY,
    batch_id VARCHAR(64) NOT NULL UNIQUE,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT,
    status VARCHAR(32) NOT NULL,
    hold_amount NUMERIC(20,10),
    hold_id VARCHAR(128)
);

CREATE TABLE temporary_credit_grants (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    expires_at TIMESTAMPTZ NOT NULL
);`)
	return err
}

func seedBatchImageCreditHoldFixture(t *testing.T, ctx context.Context, db *sql.DB) batchImageCreditHoldFixture {
	t.Helper()
	fixture := batchImageCreditHoldFixture{
		batchID:        "imgbatch_new_hold",
		invalidBatchID: "imgbatch_invalid_hold",
		legacyBatchID:  "imgbatch_legacy_hold",
	}
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users DEFAULT VALUES RETURNING id`).Scan(&fixture.userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO api_keys (user_id) VALUES ($1) RETURNING id`, fixture.userID).Scan(&fixture.apiKeyID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO groups DEFAULT VALUES RETURNING id`).Scan(&fixture.groupID))

	for i := range fixture.grantIDs {
		require.NoError(t, db.QueryRowContext(ctx, `
INSERT INTO temporary_credit_grants (user_id, expires_at)
VALUES ($1, $2)
RETURNING id`, fixture.userID, time.Now().UTC().Add(time.Duration(i+1)*time.Hour)).Scan(&fixture.grantIDs[i]))
	}

	for _, batchID := range []string{fixture.batchID, fixture.invalidBatchID} {
		_, err := db.ExecContext(ctx, `
INSERT INTO batch_image_jobs (batch_id, user_id, api_key_id, status)
VALUES ($1, $2, $3, 'created')`, batchID, fixture.userID, fixture.apiKeyID)
		require.NoError(t, err)
	}
	_, err := db.ExecContext(ctx, `
INSERT INTO batch_image_jobs (batch_id, user_id, api_key_id, status, hold_amount, hold_id)
VALUES ($1, $2, $3, 'submitted', 5, 'legacy-hold')`, fixture.legacyBatchID, fixture.userID, fixture.apiKeyID)
	require.NoError(t, err)
	return fixture
}

func insertReservedBatchImageCreditHold(t *testing.T, ctx context.Context, db *sql.DB, fixture batchImageCreditHoldFixture) int64 {
	t.Helper()
	var holdID int64
	require.NoError(t, db.QueryRowContext(ctx, `
INSERT INTO batch_image_credit_holds (
    batch_id, user_id, api_key_id, group_id, status, hold_amount,
    temporary_reserved_amount, permanent_reserved_amount, reserve_fingerprint
) VALUES ($1, $2, $3, $4, 'reserved', 10, 6, 4, 'reserve-fingerprint')
RETURNING id`, fixture.batchID, fixture.userID, fixture.apiKeyID, fixture.groupID).Scan(&holdID))
	return holdID
}

func insertReservedBatchImageAllocation(t *testing.T, ctx context.Context, db *sql.DB, holdID int64, batchID string, grantID int64, amount float64) {
	t.Helper()
	_, err := db.ExecContext(ctx, `
INSERT INTO batch_image_credit_hold_allocations (
    hold_id, batch_id, grant_id, grant_expires_at, reserved_amount
) SELECT $1, $2, id, expires_at, $4
  FROM temporary_credit_grants
 WHERE id = $3`, holdID, batchID, grantID, amount)
	require.NoError(t, err)
}

func requirePostgresCode(t *testing.T, err error, code pq.ErrorCode) {
	t.Helper()
	require.Error(t, err)
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, code, pqErr.Code)
}
