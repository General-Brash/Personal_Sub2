//go:build integration

package migrations_test

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/repository"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

const channelPricingMultipliersNaNMigration = "231_channel_pricing_multipliers_reject_nan.sql"

func TestChannelPricingMultipliersNaNMigration_PostgresNormalizesAndRejectsInvalidValues(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	db := newMigrationTestPostgres(t, ctx)
	require.NoError(t, createChannelPricingMultiplierPrerequisites(ctx, db))
	require.NoError(t, seedMigrationHistoryExcept(ctx, db, channelPricingMultipliersNaNMigration))

	_, err := db.ExecContext(ctx, `
INSERT INTO channel_model_pricing (fast_multiplier, flex_multiplier)
VALUES ('NaN'::numeric, 'Infinity'::numeric), ('-Infinity'::numeric, 0), (2.5, 1.25);
INSERT INTO channel_pricing_intervals (
    input_multiplier, output_multiplier, cache_write_multiplier, cache_read_multiplier
) VALUES ('NaN'::numeric, 'Infinity'::numeric, '-Infinity'::numeric, 0), (1, 2, 3, 4);`)
	require.NoError(t, err)

	require.NoError(t, repository.ApplyMigrations(ctx, db))

	checks := []struct {
		table  string
		column string
		id     int
	}{
		{"channel_model_pricing", "fast_multiplier", 3},
		{"channel_model_pricing", "flex_multiplier", 3},
		{"channel_pricing_intervals", "input_multiplier", 2},
		{"channel_pricing_intervals", "output_multiplier", 2},
		{"channel_pricing_intervals", "cache_write_multiplier", 2},
		{"channel_pricing_intervals", "cache_read_multiplier", 2},
	}
	for _, check := range checks {
		var remaining int
		query := "SELECT COUNT(*) FROM " + check.table + " WHERE " + check.column + " IS NOT NULL AND (" + check.column + "::text IN ('NaN', 'Infinity', '-Infinity') OR " + check.column + " <= 0)"
		require.NoError(t, db.QueryRowContext(ctx, query).Scan(&remaining), check.column)
		require.Zero(t, remaining, check.column)
	}

	var preserved int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM channel_model_pricing
WHERE fast_multiplier = 2.5 AND flex_multiplier = 1.25`).Scan(&preserved))
	require.Equal(t, 1, preserved)
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM channel_pricing_intervals
WHERE input_multiplier = 1 AND output_multiplier = 2
  AND cache_write_multiplier = 3 AND cache_read_multiplier = 4`).Scan(&preserved))
	require.Equal(t, 1, preserved)

	var constraints int
	require.NoError(t, db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM pg_constraint
WHERE conname IN (
    'channel_model_pricing_fast_multiplier_finite_positive',
    'channel_model_pricing_flex_multiplier_finite_positive',
    'channel_pricing_intervals_input_multiplier_finite_positive',
    'channel_pricing_intervals_output_multiplier_finite_positive',
    'channel_pricing_intervals_cache_write_multiplier_finite_positive',
    'channel_pricing_intervals_cache_read_multiplier_finite_positive'
)`).Scan(&constraints))
	require.Equal(t, 6, constraints)

	for _, value := range []string{"NaN", "Infinity", "-Infinity", "0", "-1"} {
		for _, check := range checks {
			query := fmt.Sprintf("UPDATE %s SET %s = $1::numeric WHERE id = %d", check.table, check.column, check.id)
			_, err = db.ExecContext(ctx, query, value)
			require.Error(t, err, "%s must reject %s", check.column, value)
		}
	}

	content, err := fs.ReadFile(migrations.FS, channelPricingMultipliersNaNMigration)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(content))
	require.NoError(t, err, "the migration must be safe to replay after normalization")
	require.NoError(t, repository.ApplyMigrations(ctx, db), "recorded migration must be safe on startup re-entry")
}

func createChannelPricingMultiplierPrerequisites(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `
CREATE TABLE channel_model_pricing (
    id BIGSERIAL PRIMARY KEY,
    fast_multiplier NUMERIC(12,6),
    flex_multiplier NUMERIC(12,6)
);
CREATE TABLE channel_pricing_intervals (
    id BIGSERIAL PRIMARY KEY,
    input_multiplier NUMERIC(12,6),
    output_multiplier NUMERIC(12,6),
    cache_write_multiplier NUMERIC(12,6),
    cache_read_multiplier NUMERIC(12,6)
);`)
	return err
}
