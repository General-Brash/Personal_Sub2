package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestValidationHarness_FourOfficialSQLContractsPresent(t *testing.T) {
	contracts := map[string][]string{
		"232_add_usage_log_upstream_request_id.sql": {
			"ALTER TABLE usage_logs", "upstream_request_id VARCHAR(128)",
		},
		"233_add_usage_log_upstream_request_id_index_notx.sql": {
			"CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_usage_logs_upstream_request_id", "WHERE upstream_request_id IS NOT NULL",
		},
		"234_channel_max_reasoning_effort_multiplier.sql": {
			"max_reasoning_effort_multiplier NUMERIC(10,4)", "chk_channel_model_pricing_max_reasoning_effort_multiplier_positive",
		},
		"234_group_codex_models_manifest_config.sql": {
			"codex_models_manifest_config JSONB NOT NULL DEFAULT '{}'::jsonb", "groups",
		},
	}
	for name, fragments := range contracts {
		t.Run(name, func(t *testing.T) {
			content, err := migrations.FS.ReadFile(name)
			require.NoError(t, err)
			for _, fragment := range fragments {
				require.Contains(t, string(content), fragment)
			}
		})
	}
}
