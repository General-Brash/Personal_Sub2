//go:build integration

package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdminTemporaryCreditAtomicGrantAndReplayWithPostgres(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	creditService := service.NewTemporaryCreditService(NewTemporaryCreditRepository(integrationDB))
	adminService := service.NewAdminTemporaryCreditService(integrationDB, creditService, NewTemporaryCreditAuditRepository(integrationDB))
	idempotencyRepo := NewIdempotencyRepository(nil, integrationDB)
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	coordinator := service.NewIdempotencyCoordinator(idempotencyRepo, cfg)
	key := "admin-grant-" + uuid.NewString()
	opts := service.IdempotencyExecuteOptions{
		Scope:          "admin.users.temporary_credits.grant",
		ActorScope:     "admin:" + strconv.FormatInt(user.ID, 10),
		Method:         "POST",
		Route:          "/api/v1/admin/users/:id/temporary-credits",
		IdempotencyKey: key,
		Payload:        map[string]any{"user_id": user.ID, "amount": "1.25000000", "notes": "integration"},
		RequireKey:     true,
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM idempotency_records WHERE operation_scope = $1 AND actor_scope = $2 AND idempotency_key_hash = $3", opts.Scope, opts.ActorScope, service.HashIdempotencyKey(key))
	})

	execute := func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return adminService.GrantAtomic(ctx, user.ID, user.ID, 1.25, "integration", claim)
	}
	first, err := coordinator.ExecuteAtomic(ctx, opts, execute)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	second, err := coordinator.ExecuteAtomic(ctx, opts, execute)
	require.NoError(t, err)
	require.True(t, second.Replayed)

	var count int
	var grantedBy int64
	var expiresAt time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*), MAX(granted_by), MAX(expires_at)
FROM temporary_credit_grants
WHERE user_id = $1 AND source = 'admin_grant'`, user.ID).Scan(&count, &grantedBy, &expiresAt))
	require.Equal(t, 1, count)
	require.Equal(t, user.ID, grantedBy)
	require.Equal(t, 0, expiresAt.Minute())
	require.Equal(t, 0, expiresAt.Second())
}

func TestAdminTemporaryCreditAtomicGrantRollsBackWhenDTOCannotPersist(t *testing.T) {
	ctx := context.Background()
	user := newTemporaryCreditTestUser(t)
	creditService := service.NewTemporaryCreditService(NewTemporaryCreditRepository(integrationDB))
	adminService := service.NewAdminTemporaryCreditService(integrationDB, creditService, NewTemporaryCreditAuditRepository(integrationDB))
	idempotencyRepo := NewIdempotencyRepository(nil, integrationDB)
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	cfg.MaxStoredResponseLen = 1
	coordinator := service.NewIdempotencyCoordinator(idempotencyRepo, cfg)
	key := "admin-grant-rollback-" + uuid.NewString()
	opts := service.IdempotencyExecuteOptions{
		Scope:          "admin.users.temporary_credits.grant",
		ActorScope:     "admin:" + strconv.FormatInt(user.ID, 10),
		Method:         "POST",
		Route:          "/api/v1/admin/users/:id/temporary-credits",
		IdempotencyKey: key,
		Payload:        map[string]any{"user_id": user.ID, "amount": "1.25000000", "notes": "rollback"},
		RequireKey:     true,
	}
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM idempotency_records WHERE operation_scope = $1 AND actor_scope = $2 AND idempotency_key_hash = $3", opts.Scope, opts.ActorScope, service.HashIdempotencyKey(key))
	})

	_, err := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return adminService.GrantAtomic(ctx, user.ID, user.ID, 1.25, "rollback", claim)
	})
	require.ErrorIs(t, err, service.ErrIdempotencyStoreUnavail)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM temporary_credit_grants
WHERE user_id = $1 AND source = 'admin_grant'`, user.ID).Scan(&count))
	require.Zero(t, count)
}
