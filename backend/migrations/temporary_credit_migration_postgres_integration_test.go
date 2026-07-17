//go:build integration

package migrations_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const dailyCheckinMigration = "175_daily_checkin_temporary_credits.sql"

func TestDailyCheckinAndActorScopeMigrations_PostgresForwardAndReentrant(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	db := newMigrationTestPostgres(t, ctx)
	require.NoError(t, createDailyCheckinMigrationPrerequisites(ctx, db))
	require.NoError(t, createLegacyIdempotencyTable(ctx, db))
	legacyID, err := insertLegacyIdempotencyRecord(ctx, db, "user.daily_checkin.create", "legacy-checkin-key")
	require.NoError(t, err)
	require.NoError(t, seedMigrationHistoryBefore(ctx, db, dailyCheckinMigration))

	require.NoError(t, repository.ApplyMigrations(ctx, db), "forward apply must run 175 then 176")
	requireDailyCheckinMigrationShape(t, ctx, db)
	requireLegacyActorScope(t, ctx, db, legacyID, "user.daily_checkin.create")

	for _, name := range []string{dailyCheckinMigration, idempotencyActorScopeMigration} {
		content, readErr := fs.ReadFile(migrations.FS, name)
		require.NoError(t, readErr)
		_, execErr := db.ExecContext(ctx, string(content))
		require.NoError(t, execErr, "%s must be safe to replay directly", name)
	}
	requireDailyCheckinMigrationShape(t, ctx, db)
	requireLegacyActorScope(t, ctx, db, legacyID, "user.daily_checkin.create")

	require.NoError(t, repository.ApplyMigrations(ctx, db), "recorded migrations must be safe on startup re-entry")
	var applied int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM schema_migrations
WHERE filename IN ($1, $2)`, dailyCheckinMigration, idempotencyActorScopeMigration).Scan(&applied))
	require.Equal(t, 2, applied)
}

func createDailyCheckinMigrationPrerequisites(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY
);

CREATE TABLE usage_logs (
    id BIGSERIAL PRIMARY KEY
);

CREATE TABLE settings (
    key VARCHAR(255) PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);`)
	return err
}

func seedMigrationHistoryBefore(ctx context.Context, db *sql.DB, stopBefore string) error {
	if _, err := db.ExecContext(ctx, `
CREATE TABLE schema_migrations (
    filename TEXT PRIMARY KEY,
    checksum TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
)`); err != nil {
		return err
	}

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files {
		if name >= stopBefore {
			continue
		}
		contents, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return err
		}
		content := strings.TrimSpace(string(contents))
		if content == "" {
			continue
		}
		sum := sha256.Sum256([]byte(content))
		if _, err := db.ExecContext(ctx, `
INSERT INTO schema_migrations (filename, checksum)
VALUES ($1, $2)`, name, hex.EncodeToString(sum[:])); err != nil {
			return err
		}
	}
	return nil
}

func requireDailyCheckinMigrationShape(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	var tableCount int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = 'public'
  AND table_name IN ('daily_checkins', 'temporary_credit_grants', 'temporary_credit_consumptions')`).Scan(&tableCount))
	require.Equal(t, 3, tableCount)

	var triggerCount int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM pg_trigger trigger
JOIN pg_class table_class ON table_class.oid = trigger.tgrelid
JOIN pg_namespace namespace ON namespace.oid = table_class.relnamespace
WHERE namespace.nspname = 'public'
  AND table_class.relname = 'temporary_credit_consumptions'
  AND trigger.tgname = 'temporary_credit_consumptions_request_id_immutable'
  AND NOT trigger.tgisinternal`).Scan(&triggerCount))
	require.Equal(t, 1, triggerCount)

	var settingCount int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM settings
WHERE key IN ('daily_checkin_enabled', 'daily_checkin_max_reward_day', 'daily_checkin_reward_tiers')`).Scan(&settingCount))
	require.Equal(t, 3, settingCount)
}
