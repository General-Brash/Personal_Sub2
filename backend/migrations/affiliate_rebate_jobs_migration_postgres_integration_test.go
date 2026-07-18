//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const affiliateRebateJobsMigration = "184_affiliate_rebate_jobs.sql"

func TestAffiliateRebateJobsMigration_Postgres18ConstraintsAndReplay(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	db := newMigrationTestPostgres(t, ctx)
	require.NoError(t, createAffiliateRebateJobsPrerequisites(ctx, db))
	require.NoError(t, seedMigrationHistoryExcept(ctx, db, affiliateRebateJobsMigration))
	require.NoError(t, repository.ApplyMigrations(ctx, db))

	var userID, sourceOne, sourceTwo int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO users DEFAULT VALUES RETURNING id`).Scan(&userID))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes DEFAULT VALUES RETURNING id`).Scan(&sourceOne))
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO redeem_codes DEFAULT VALUES RETURNING id`).Scan(&sourceTwo))

	_, err := db.ExecContext(ctx, `
INSERT INTO affiliate_rebate_jobs (
    invitee_user_id, source_redeem_code_id, source_kind, base_amount
) VALUES ($1, $2, 'redeem', 1)`, userID, sourceOne)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO affiliate_rebate_jobs (
    invitee_user_id, source_redeem_code_id, source_kind, base_amount
) VALUES ($1, $2, 'admin_recharge', 2)`, userID, sourceOne)
	requirePostgresCode(t, err, "23505")

	_, err = db.ExecContext(ctx, `
INSERT INTO user_affiliate_ledger (user_id, action, amount, source_redeem_code_id)
VALUES ($1, 'accrue', 0.5, $2)`, userID, sourceTwo)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
INSERT INTO user_affiliate_ledger (user_id, action, amount, source_redeem_code_id)
VALUES ($1, 'accrue', 0.25, $2)`, userID, sourceTwo)
	requirePostgresCode(t, err, "23505")

	_, err = db.ExecContext(ctx, `
INSERT INTO affiliate_rebate_jobs (
    invitee_user_id, source_redeem_code_id, source_kind, base_amount, status
) VALUES ($1, $2, 'redeem', 1, 'unknown')`, userID, sourceTwo)
	requirePostgresCode(t, err, "23514")

	content, err := fs.ReadFile(migrations.FS, affiliateRebateJobsMigration)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(content))
	require.NoError(t, err, "migration must be safe to replay directly")
	require.NoError(t, repository.ApplyMigrations(ctx, db))

	var applied int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM schema_migrations WHERE filename = $1`, affiliateRebateJobsMigration).Scan(&applied))
	require.Equal(t, 1, applied)
}

func createAffiliateRebateJobsPrerequisites(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY
);

CREATE TABLE redeem_codes (
    id BIGSERIAL PRIMARY KEY
);

CREATE TABLE user_affiliate_ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action VARCHAR(32) NOT NULL,
    amount NUMERIC(20,8) NOT NULL
);`)
	return err
}
