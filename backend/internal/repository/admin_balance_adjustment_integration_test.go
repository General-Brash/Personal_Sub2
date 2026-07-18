//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const adminBalanceTestScope = "admin.users.balance.update"

func newAdminBalanceTestUser(t *testing.T, balance float64) *service.User {
	t.Helper()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        "admin-balance-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		Balance:      balance,
	})
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM idempotency_records WHERE operation_scope = $1 AND actor_scope = $2", adminBalanceTestScope, adminBalanceActorScope(user.ID))
		_, _ = integrationDB.ExecContext(ctx, `
DELETE FROM affiliate_rebate_jobs
WHERE source_redeem_code_id IN (
    SELECT id FROM redeem_codes WHERE used_by = $1 AND type = $2
)`, user.ID, service.AdjustmentTypeAdminBalance)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM redeem_codes WHERE used_by = $1 AND type = $2", user.ID, service.AdjustmentTypeAdminBalance)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	return user
}

func adminBalanceActorScope(userID int64) string {
	return "admin:" + strconv.FormatInt(userID, 10)
}

func adminBalanceOptions(userID int64, key string, amount float64, operation, notes string) service.IdempotencyExecuteOptions {
	return service.IdempotencyExecuteOptions{
		Scope:          adminBalanceTestScope,
		ActorScope:     adminBalanceActorScope(userID),
		Method:         "POST",
		Route:          "/api/v1/admin/users/:id/balance",
		IdempotencyKey: key,
		Payload: map[string]any{
			"user_id":   userID,
			"balance":   amount,
			"operation": operation,
			"notes":     notes,
		},
		RequireKey: true,
	}
}

func newAdminBalanceCoordinator(maxStoredResponseLen int) *service.IdempotencyCoordinator {
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	if maxStoredResponseLen > 0 {
		cfg.MaxStoredResponseLen = maxStoredResponseLen
	}
	return service.NewIdempotencyCoordinator(NewIdempotencyRepository(nil, integrationDB), cfg)
}

func adminBalanceResponse(user *service.User) any {
	return map[string]any{"id": user.ID, "balance": user.Balance}
}

func applyAdminBalanceRepositoryAtomic(
	ctx context.Context,
	repo *userRepository,
	userID int64,
	adjustment service.AdminBalanceAdjustment,
	claim *service.IdempotencyAtomicClaim,
) (any, error) {
	result, err := repo.ApplyAdminBalanceAdjustmentAtomic(ctx, userID, adjustment, claim, adminBalanceResponse)
	if err != nil {
		return nil, err
	}
	return result.Response, nil
}

func adminBalanceAuditCount(t *testing.T, userID int64, notes string) int {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM redeem_codes
WHERE used_by = $1 AND type = $2 AND notes = $3`, userID, service.AdjustmentTypeAdminBalance, notes).Scan(&count))
	return count
}

func adminBalanceRebateJobCount(t *testing.T, userID int64, notes string) int {
	t.Helper()
	var count int
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM affiliate_rebate_jobs job
JOIN redeem_codes code ON code.id = job.source_redeem_code_id
WHERE code.used_by = $1
  AND code.type = $2
  AND code.notes = $3
  AND job.source_kind = 'admin_recharge'`, userID, service.AdjustmentTypeAdminBalance, notes).Scan(&count))
	return count
}

func persistedBalance(t *testing.T, userID int64) float64 {
	t.Helper()
	var balance float64
	require.NoError(t, integrationDB.QueryRowContext(context.Background(), "SELECT balance FROM users WHERE id = $1", userID).Scan(&balance))
	return balance
}

func TestAdminBalanceAtomicConcurrentWithUsageDebit(t *testing.T) {
	ctx := context.Background()
	user := newAdminBalanceTestUser(t, 10)
	apiKey := mustCreateApiKey(t, integrationEntClient, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-admin-balance-" + uuid.NewString(),
		Name:   "admin-balance-concurrency",
	})
	account := mustCreateAccount(t, integrationEntClient, &service.Account{
		Name: "admin-balance-concurrency-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})
	requestID := uuid.NewString()
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM accounts WHERE id = $1", account.ID)
	})

	userRepo := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
	usageRepo := NewUsageBillingRepository(integrationEntClient, integrationDB)
	coordinator := newAdminBalanceCoordinator(0)
	adjustment := service.AdminBalanceAdjustment{Amount: 5, Operation: "add", Notes: "concurrent-usage"}
	opts := adminBalanceOptions(user.ID, "admin-balance-"+uuid.NewString(), adjustment.Amount, adjustment.Operation, adjustment.Notes)

	start := make(chan struct{})
	adminReady := make(chan struct{})
	adminErr := make(chan error, 1)
	usageErr := make(chan error, 1)
	go func() {
		_, err := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
			close(adminReady)
			<-start
			return applyAdminBalanceRepositoryAtomic(ctx, userRepo, user.ID, adjustment, claim)
		})
		adminErr <- err
	}()
	<-adminReady
	go func() {
		<-start
		_, err := usageRepo.Apply(ctx, &service.UsageBillingCommand{
			RequestID:   requestID,
			APIKeyID:    apiKey.ID,
			UserID:      user.ID,
			AccountID:   account.ID,
			AccountType: service.AccountTypeAPIKey,
			BalanceCost: 3,
		})
		usageErr <- err
	}()
	close(start)
	require.NoError(t, <-adminErr)
	require.NoError(t, <-usageErr)
	require.Equal(t, 12.0, persistedBalance(t, user.ID))
	require.Equal(t, 1, adminBalanceAuditCount(t, user.ID, adjustment.Notes))
	require.Equal(t, 1, adminBalanceRebateJobCount(t, user.ID, adjustment.Notes))
}

func TestAdminBalanceAtomicConcurrentAddsDoNotLoseUpdates(t *testing.T) {
	ctx := context.Background()
	user := newAdminBalanceTestUser(t, 10)
	userRepo := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
	coordinator := newAdminBalanceCoordinator(0)
	adjustments := []service.AdminBalanceAdjustment{
		{Amount: 4, Operation: "add", Notes: "concurrent-add-4"},
		{Amount: 6, Operation: "add", Notes: "concurrent-add-6"},
	}

	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(len(adjustments))
	errs := make(chan error, len(adjustments))
	for i := range adjustments {
		adjustment := adjustments[i]
		opts := adminBalanceOptions(user.ID, "admin-double-add-"+uuid.NewString(), adjustment.Amount, adjustment.Operation, adjustment.Notes)
		go func() {
			_, err := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
				ready.Done()
				<-start
				return applyAdminBalanceRepositoryAtomic(ctx, userRepo, user.ID, adjustment, claim)
			})
			errs <- err
		}()
	}
	ready.Wait()
	close(start)
	for range adjustments {
		require.NoError(t, <-errs)
	}
	require.Equal(t, 20.0, persistedBalance(t, user.ID))
	require.Equal(t, 1, adminBalanceAuditCount(t, user.ID, adjustments[0].Notes))
	require.Equal(t, 1, adminBalanceAuditCount(t, user.ID, adjustments[1].Notes))
	require.Equal(t, 1, adminBalanceRebateJobCount(t, user.ID, adjustments[0].Notes))
	require.Equal(t, 1, adminBalanceRebateJobCount(t, user.ID, adjustments[1].Notes))
}

func TestAdminBalanceAtomicReplaysWithoutSecondAdjustment(t *testing.T) {
	ctx := context.Background()
	user := newAdminBalanceTestUser(t, 10)
	userRepo := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
	coordinator := newAdminBalanceCoordinator(0)
	adjustment := service.AdminBalanceAdjustment{Amount: 1.25, Operation: "add", Notes: "replay-once"}
	opts := adminBalanceOptions(user.ID, "admin-replay-"+uuid.NewString(), adjustment.Amount, adjustment.Operation, adjustment.Notes)
	var calls atomic.Int32
	execute := func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		calls.Add(1)
		return applyAdminBalanceRepositoryAtomic(ctx, userRepo, user.ID, adjustment, claim)
	}

	first, err := coordinator.ExecuteAtomic(ctx, opts, execute)
	require.NoError(t, err)
	second, err := coordinator.ExecuteAtomic(ctx, opts, execute)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	require.True(t, second.Replayed)
	require.Equal(t, int32(1), calls.Load())
	firstJSON, err := json.Marshal(first.Data)
	require.NoError(t, err)
	secondJSON, err := json.Marshal(second.Data)
	require.NoError(t, err)
	require.JSONEq(t, string(firstJSON), string(secondJSON))
	require.Equal(t, 11.25, persistedBalance(t, user.ID))
	require.Equal(t, 1, adminBalanceAuditCount(t, user.ID, adjustment.Notes))
	require.Equal(t, 1, adminBalanceRebateJobCount(t, user.ID, adjustment.Notes))
}

func TestAdminBalanceAtomicRollsBackWhenIdempotencyResponseCannotPersist(t *testing.T) {
	ctx := context.Background()
	user := newAdminBalanceTestUser(t, 10)
	userRepo := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
	coordinator := newAdminBalanceCoordinator(1)
	adjustment := service.AdminBalanceAdjustment{Amount: 2, Operation: "add", Notes: "idempotency-failure"}
	opts := adminBalanceOptions(user.ID, "admin-idem-fail-"+uuid.NewString(), adjustment.Amount, adjustment.Operation, adjustment.Notes)

	_, err := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return applyAdminBalanceRepositoryAtomic(ctx, userRepo, user.ID, adjustment, claim)
	})
	require.ErrorIs(t, err, service.ErrIdempotencyStoreUnavail)
	require.Equal(t, 10.0, persistedBalance(t, user.ID))
	require.Zero(t, adminBalanceAuditCount(t, user.ID, adjustment.Notes))
	require.Zero(t, adminBalanceRebateJobCount(t, user.ID, adjustment.Notes))
}

func TestAdminBalanceAtomicRollsBackWhenAuditInsertFails(t *testing.T) {
	ctx := context.Background()
	user := newAdminBalanceTestUser(t, 10)
	duplicateCode := "audit-failure-" + uuid.NewString()[:16]
	_, err := integrationEntClient.RedeemCode.Create().
		SetCode(duplicateCode).
		SetType(service.RedeemTypeBalance).
		SetStatus(service.StatusUnused).
		Save(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM redeem_codes WHERE code = $1", duplicateCode)
	})

	userRepo := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
	coordinator := newAdminBalanceCoordinator(0)
	adjustment := service.AdminBalanceAdjustment{
		Amount:    2,
		Operation: "add",
		Notes:     "audit-failure",
		AuditCode: duplicateCode,
	}
	opts := adminBalanceOptions(user.ID, "admin-audit-fail-"+uuid.NewString(), adjustment.Amount, adjustment.Operation, adjustment.Notes)
	_, err = coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return applyAdminBalanceRepositoryAtomic(ctx, userRepo, user.ID, adjustment, claim)
	})
	require.Error(t, err)
	require.Equal(t, 10.0, persistedBalance(t, user.ID))
	require.Zero(t, adminBalanceAuditCount(t, user.ID, adjustment.Notes))
	require.Zero(t, adminBalanceRebateJobCount(t, user.ID, adjustment.Notes))
}

func TestAdminBalanceAtomicSetAndSubtractBoundaries(t *testing.T) {
	ctx := context.Background()
	user := newAdminBalanceTestUser(t, 1.23456789)
	userRepo := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
	coordinator := newAdminBalanceCoordinator(0)

	execute := func(adjustment service.AdminBalanceAdjustment) error {
		opts := adminBalanceOptions(user.ID, "admin-boundary-"+uuid.NewString(), adjustment.Amount, adjustment.Operation, adjustment.Notes)
		_, err := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
			return applyAdminBalanceRepositoryAtomic(ctx, userRepo, user.ID, adjustment, claim)
		})
		return err
	}

	require.NoError(t, execute(service.AdminBalanceAdjustment{Amount: 1.23456789, Operation: "subtract", Notes: "subtract-to-zero"}))
	require.Zero(t, persistedBalance(t, user.ID))
	require.NoError(t, execute(service.AdminBalanceAdjustment{Amount: 2.000000004, Operation: "set", Notes: "set-normalized"}))
	require.Equal(t, 2.0, persistedBalance(t, user.ID))
	require.Zero(t, adminBalanceRebateJobCount(t, user.ID, "subtract-to-zero"))
	require.Zero(t, adminBalanceRebateJobCount(t, user.ID, "set-normalized"))
	err := execute(service.AdminBalanceAdjustment{Amount: 2.00000001, Operation: "subtract", Notes: "subtract-too-much"})
	require.ErrorIs(t, err, service.ErrAdminBalanceInsufficient)
	require.Equal(t, 2.0, persistedBalance(t, user.ID))
	require.Zero(t, adminBalanceAuditCount(t, user.ID, "subtract-too-much"))

	var values []float64
	rows, err := integrationDB.QueryContext(ctx, `
SELECT value
FROM redeem_codes
WHERE used_by = $1 AND type = $2 AND notes IN ('subtract-to-zero', 'set-normalized')
ORDER BY notes`, user.ID, service.AdjustmentTypeAdminBalance)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var value float64
		require.NoError(t, rows.Scan(&value))
		values = append(values, value)
	}
	require.NoError(t, rows.Err())
	require.ElementsMatch(t, []float64{-1.23456789, 2}, values)
}

func TestAdminBalanceAtomicResponseUsesCommittedEightDecimalValue(t *testing.T) {
	ctx := context.Background()
	user := newAdminBalanceTestUser(t, 0.1)
	userRepo := NewUserRepository(integrationEntClient, integrationDB).(*userRepository)
	coordinator := newAdminBalanceCoordinator(0)
	adjustment := service.AdminBalanceAdjustment{Amount: 0.200000004, Operation: "add", Notes: "response-precision"}
	opts := adminBalanceOptions(user.ID, "admin-response-"+uuid.NewString(), adjustment.Amount, adjustment.Operation, adjustment.Notes)

	result, err := coordinator.ExecuteAtomic(ctx, opts, func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
		return applyAdminBalanceRepositoryAtomic(ctx, userRepo, user.ID, adjustment, claim)
	})
	require.NoError(t, err)
	require.Equal(t, 0.3, persistedBalance(t, user.ID))
	require.Equal(t, 1, adminBalanceRebateJobCount(t, user.ID, adjustment.Notes))
	raw, err := json.Marshal(result.Data)
	require.NoError(t, err)
	require.JSONEq(t, fmt.Sprintf(`{"id":%d,"balance":0.3}`, user.ID), string(raw))
}
