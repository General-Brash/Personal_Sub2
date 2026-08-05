//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserRepositoryGetAvailableCreditSnapshotIncludesOnlyCurrentTemporaryCreditAcrossSources(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)

	_, err := integrationDB.ExecContext(ctx, `
INSERT INTO temporary_credit_grants
    (user_id, source, amount, remaining_amount, available_at, expires_at, notes, granted_by)
VALUES
    ($1, 'checkin', 1.25, 1.25, clock_timestamp() - INTERVAL '1 minute', clock_timestamp() + INTERVAL '2 hours', 'active-checkin', $1),
    ($1, 'mall_product', 2.75, 2.75, clock_timestamp() - INTERVAL '1 minute', clock_timestamp() + INTERVAL '1 hour', 'active-mall', $1),
    ($1, 'subscription', 5.00, 5.00, clock_timestamp() + INTERVAL '1 hour', clock_timestamp() + INTERVAL '2 hours', 'future', $1),
    ($1, 'admin_grant', 7.00, 7.00, clock_timestamp() - INTERVAL '2 hours', clock_timestamp() - INTERVAL '1 hour', 'expired', $1),
    ($1, 'bank_advance', 11.00, 0.00, clock_timestamp() - INTERVAL '1 hour', clock_timestamp() + INTERVAL '1 hour', 'depleted', $1)`, user.ID)
	require.NoError(t, err)

	reader, ok := NewUserRepository(testEntClient(t), integrationDB).(service.AvailableCreditSnapshotReader)
	require.True(t, ok)
	snapshot, err := reader.GetAvailableCreditSnapshot(ctx, user.ID)

	require.NoError(t, err)
	require.InDelta(t, 4.0, snapshot.TemporaryCredit, 1e-12)
	require.NotNil(t, snapshot.EarliestTemporaryCreditExpiry)
}
