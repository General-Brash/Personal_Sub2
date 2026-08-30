package migrations

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelPricingMultipliersNaNMigrationAddsFiniteConstraints(t *testing.T) {
	content, err := FS.ReadFile("231_channel_pricing_multipliers_reject_nan.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")
	for _, table := range []string{"channel_model_pricing", "channel_pricing_intervals"} {
		require.Contains(t, sql, "UPDATE "+table+" SET")
	}
	for _, column := range []string{
		"fast_multiplier", "flex_multiplier", "input_multiplier", "output_multiplier",
		"cache_write_multiplier", "cache_read_multiplier",
	} {
		require.Contains(t, sql, "CASE WHEN "+column+"::text IN ('NaN', 'Infinity', '-Infinity') OR "+column+" <= 0 THEN NULL")
		require.Contains(t, sql, column+"::text NOT IN ('NaN', 'Infinity', '-Infinity') AND "+column+" > 0")
	}
	updateEnd := strings.Index(sql, "DO $$")
	require.Greater(t, updateEnd, 0, "constraint block must remain after invalid-value normalization")
	require.Less(t, strings.Index(sql, "UPDATE channel_model_pricing"), updateEnd)
	require.Less(t, strings.Index(sql, "UPDATE channel_pricing_intervals"), updateEnd)

	constraints := []struct {
		table  string
		name   string
		column string
	}{
		{"channel_model_pricing", "channel_model_pricing_fast_multiplier_finite_positive", "fast_multiplier"},
		{"channel_model_pricing", "channel_model_pricing_flex_multiplier_finite_positive", "flex_multiplier"},
		{"channel_pricing_intervals", "channel_pricing_intervals_input_multiplier_finite_positive", "input_multiplier"},
		{"channel_pricing_intervals", "channel_pricing_intervals_output_multiplier_finite_positive", "output_multiplier"},
		{"channel_pricing_intervals", "channel_pricing_intervals_cache_write_multiplier_finite_positive", "cache_write_multiplier"},
		{"channel_pricing_intervals", "channel_pricing_intervals_cache_read_multiplier_finite_positive", "cache_read_multiplier"},
	}
	for _, constraint := range constraints {
		require.Contains(t, sql, "conname = '"+constraint.name+"' AND conrelid = '"+constraint.table+"'::regclass")
	}
}

func TestChannelPricingMultipliersPublished228RemainsByteStable(t *testing.T) {
	content, err := FS.ReadFile("228_channel_pricing_multipliers.sql")
	require.NoError(t, err)
	sum := sha256.Sum256(bytes.TrimSpace(content))
	require.Equal(t, "3d7c36d49e509b69f479e0a5b05d4074c49ea7ba8117f12f82764f09d6e3b542", hex.EncodeToString(sum[:]))
}
