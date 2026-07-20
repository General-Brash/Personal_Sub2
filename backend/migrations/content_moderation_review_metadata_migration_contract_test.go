package migrations

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContentModerationReviewMetadataMigrationIsAdditiveAndIdempotent(t *testing.T) {
	content, err := fs.ReadFile(FS, "187_content_moderation_review_metadata.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS review_metadata JSONB NOT NULL DEFAULT '{}'::jsonb")
	require.NotContains(t, sql, "DROP COLUMN")
	require.NotContains(t, sql, "UPDATE content_moderation_logs")
}
