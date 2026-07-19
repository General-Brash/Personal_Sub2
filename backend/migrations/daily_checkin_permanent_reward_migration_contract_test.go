package migrations

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDailyCheckinPermanentRewardMigrationPreservesExistingRows(t *testing.T) {
	content, err := fs.ReadFile(FS, "186_daily_checkin_permanent_rewards.sql")
	require.NoError(t, err)
	sql := string(content)

	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS permanent_reward_amount NUMERIC(20,8) NOT NULL DEFAULT 0")
	require.Contains(t, sql, "daily_checkins_permanent_reward_nonnegative")
	require.Contains(t, sql, "CHECK (permanent_reward_amount >= 0)")
	require.NotContains(t, sql, "total_recharged")
}
