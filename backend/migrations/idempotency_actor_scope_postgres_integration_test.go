//go:build integration

package migrations_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	idempotencyActorScopeMigration = "176_idempotency_actor_scope.sql"
	postgresImage                  = "postgres:18.1-alpine3.23"
)

func TestIdempotencyActorScopeMigration_PostgresLegacyContract(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	db := newMigrationTestPostgres(t, ctx)
	require.NoError(t, createLegacyIdempotencyTable(ctx, db))

	firstID, err := insertLegacyIdempotencyRecord(ctx, db, "user.checkin", "shared-legacy-key")
	require.NoError(t, err)
	secondID, err := insertLegacyIdempotencyRecord(ctx, db, "user.checkin", "second-legacy-key")
	require.NoError(t, err)

	require.NoError(t, seedMigrationHistoryExcept(ctx, db, idempotencyActorScopeMigration))
	require.NoError(t, repository.ApplyMigrations(ctx, db))

	requireLegacyActorScope(t, ctx, db, firstID, "user.checkin")
	requireLegacyActorScope(t, ctx, db, secondID, "user.checkin")
	requireLegacyBinaryShapeRemoved(t, ctx, db)

	require.NoError(t, insertScopedIdempotencyRecord(ctx, db, "user.checkin", "user:101", "shared-legacy-key"))
	require.NoError(t, insertScopedIdempotencyRecord(ctx, db, "user.checkin", "user:202", "shared-legacy-key"))
	require.NoError(t, insertScopedIdempotencyRecord(ctx, db, "admin.temporary-credit.grant", "user:101", "shared-legacy-key"))
	requireWrongTwoDimensionalIndexRejectsDifferentOperation(t, ctx, db)
	duplicateErr := insertScopedIdempotencyRecord(ctx, db, "user.checkin", "user:101", "shared-legacy-key")
	require.Error(t, duplicateErr)
	var pqErr *pq.Error
	require.True(t, errors.As(duplicateErr, &pqErr))
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)

	// A second call exercises the production runner's recorded-migration path;
	// it must not directly re-run the ALTER statements.
	require.NoError(t, repository.ApplyMigrations(ctx, db))
	var applied int
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM schema_migrations
		WHERE filename = $1
	`, idempotencyActorScopeMigration).Scan(&applied))
	require.Equal(t, 1, applied)
}

func newMigrationTestPostgres(t *testing.T, ctx context.Context) *sql.DB {
	t.Helper()

	container, err := tcpostgres.Run(
		ctx,
		postgresImage,
		tcpostgres.WithDatabase("sub2api_migrations_test"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
	)
	testcontainers.CleanupContainer(t, container)
	require.NoError(t, err)

	dsn, err := container.ConnectionString(ctx, "sslmode=disable", "TimeZone=UTC")
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.Eventually(t, func() bool {
		return db.PingContext(ctx) == nil
	}, 30*time.Second, 250*time.Millisecond)
	return db
}

func createLegacyIdempotencyTable(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE idempotency_records (
			id BIGSERIAL PRIMARY KEY,
			scope VARCHAR(128) NOT NULL,
			idempotency_key_hash VARCHAR(64) NOT NULL,
			request_fingerprint VARCHAR(64) NOT NULL,
			status VARCHAR(32) NOT NULL,
			response_status INTEGER,
			response_body TEXT,
			error_reason VARCHAR(128),
			locked_until TIMESTAMPTZ,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `
		CREATE UNIQUE INDEX idx_idempotency_records_scope_key
		ON idempotency_records (scope, idempotency_key_hash)
	`)
	return err
}

func insertLegacyIdempotencyRecord(ctx context.Context, db *sql.DB, scope, keyHash string) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx, `
		INSERT INTO idempotency_records (
			scope, idempotency_key_hash, request_fingerprint, status, expires_at
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, scope, keyHash, "fingerprint-"+keyHash, "succeeded", time.Now().UTC().Add(24*time.Hour)).Scan(&id)
	return id, err
}

func insertScopedIdempotencyRecord(ctx context.Context, db *sql.DB, operationScope, actorScope, keyHash string) error {
	_, err := db.ExecContext(ctx, `
		INSERT INTO idempotency_records (
			operation_scope, actor_scope, idempotency_key_hash, request_fingerprint, status, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, operationScope, actorScope, keyHash, "fingerprint-"+actorScope, "succeeded", time.Now().UTC().Add(24*time.Hour))
	return err
}

func requireWrongTwoDimensionalIndexRejectsDifferentOperation(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	_, err := db.ExecContext(ctx, `
		CREATE TABLE idempotency_records_wrong_index_control (
			operation_scope VARCHAR(128) NOT NULL,
			actor_scope VARCHAR(128) NOT NULL,
			idempotency_key_hash VARCHAR(64) NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE UNIQUE INDEX idx_idempotency_records_wrong_index_control
		ON idempotency_records_wrong_index_control (actor_scope, idempotency_key_hash)
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		INSERT INTO idempotency_records_wrong_index_control (
			operation_scope, actor_scope, idempotency_key_hash
		) VALUES ($1, $2, $3)
	`, "user.checkin", "user:101", "shared-legacy-key")
	require.NoError(t, err)

	// This is the negative control for the successful insertion above: omitting
	// operation_scope from the unique index incorrectly rejects another operation.
	_, err = db.ExecContext(ctx, `
		INSERT INTO idempotency_records_wrong_index_control (
			operation_scope, actor_scope, idempotency_key_hash
		) VALUES ($1, $2, $3)
	`, "admin.temporary-credit.grant", "user:101", "shared-legacy-key")
	require.Error(t, err)
	var pqErr *pq.Error
	require.True(t, errors.As(err, &pqErr))
	require.Equal(t, pq.ErrorCode("23505"), pqErr.Code)
}

func requireLegacyActorScope(t *testing.T, ctx context.Context, db *sql.DB, id int64, operationScope string) {
	t.Helper()

	var actualOperationScope, actorScope string
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT operation_scope, actor_scope
		FROM idempotency_records
		WHERE id = $1
	`, id).Scan(&actualOperationScope, &actorScope))
	require.Equal(t, operationScope, actualOperationScope)
	require.Equal(t, fmt.Sprintf("legacy:%d", id), actorScope)
}

func requireLegacyBinaryShapeRemoved(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()

	var scopeColumnExists bool
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_schema = 'public'
				AND table_name = 'idempotency_records'
				AND column_name = 'scope'
		)
	`).Scan(&scopeColumnExists))
	require.False(t, scopeColumnExists)

	var legacyIndex sql.NullString
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT to_regclass('public.idx_idempotency_records_scope_key')
	`).Scan(&legacyIndex))
	require.False(t, legacyIndex.Valid)

	var unique bool
	require.NoError(t, db.QueryRowContext(ctx, `
		SELECT idx.indisunique
		FROM pg_class index_class
		JOIN pg_index idx ON idx.indexrelid = index_class.oid
		JOIN pg_class table_class ON table_class.oid = idx.indrelid
		JOIN pg_namespace namespace ON namespace.oid = table_class.relnamespace
		WHERE namespace.nspname = 'public'
			AND table_class.relname = 'idempotency_records'
			AND index_class.relname = 'idx_idempotency_records_operation_actor_key'
	`).Scan(&unique))
	require.True(t, unique)
}

func seedMigrationHistoryExcept(ctx context.Context, db *sql.DB, excluded ...string) error {
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE schema_migrations (
			filename TEXT PRIMARY KEY,
			checksum TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`); err != nil {
		return err
	}

	excludedSet := make(map[string]struct{}, len(excluded))
	for _, name := range excluded {
		excludedSet[name] = struct{}{}
	}

	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files {
		// The catalog contains same-numbered and later migrations. Mark all
		// non-target files as applied so this focused fixture executes only the
		// migration contract under test.
		if _, skip := excludedSet[name]; skip {
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
			VALUES ($1, $2)
		`, name, hex.EncodeToString(sum[:])); err != nil {
			return err
		}
	}
	return nil
}
