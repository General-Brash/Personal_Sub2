//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUsageBillingRepositoryApply_EightDecimalZeroStillCommitsUsageAndDedup(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	requestID := "usage-zero-" + uuid.NewString()
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-zero-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      10,
	})
	account := mustCreateAccount(t, client, &service.Account{
		Name: "usage-zero-" + uuid.NewString(),
		Type: service.AccountTypeAPIKey,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID: user.ID,
		Key:    "sk-usage-zero-" + uuid.NewString(),
		Name:   "usage-zero",
	})
	var grantID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO temporary_credit_grants (
    user_id, source, amount, remaining_amount, expires_at, notes, granted_by
)
VALUES ($1, 'admin_grant', 1, 1, clock_timestamp() + INTERVAL '1 hour', '', $1)
RETURNING id`, user.ID).Scan(&grantID))
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		statements := []struct {
			query string
			args  []any
		}{
			{"DELETE FROM temporary_credit_consumptions WHERE grant_id = $1", []any{grantID}},
			{"DELETE FROM temporary_credit_grants WHERE id = $1", []any{grantID}},
			{"DELETE FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", []any{requestID, apiKey.ID}},
			{"DELETE FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", []any{requestID, apiKey.ID}},
			{"DELETE FROM usage_billing_dedup_archive WHERE request_id = $1 AND api_key_id = $2", []any{requestID, apiKey.ID}},
			{"DELETE FROM api_keys WHERE id = $1", []any{apiKey.ID}},
			{"DELETE FROM accounts WHERE id = $1", []any{account.ID}},
			{"DELETE FROM users WHERE id = $1", []any{user.ID}},
		}
		for _, statement := range statements {
			if _, err := integrationDB.ExecContext(cleanupCtx, statement.query, statement.args...); err != nil {
				t.Errorf("cleanup zero-cost usage billing fixture: %v", err)
			}
		}
	})

	usageLog := &service.UsageLog{
		UserID:    user.ID,
		APIKeyID:  apiKey.ID,
		AccountID: account.ID,
		RequestID: requestID,
		Model:     "gpt-5",
	}
	cmd := &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    apiKey.ID,
		UserID:      user.ID,
		AccountID:   account.ID,
		AccountType: service.AccountTypeAPIKey,
		BalanceCost: 0.000000004,
		UsageLog:    usageLog,
	}

	result, err := NewUsageBillingRepository(client, integrationDB).Apply(ctx, cmd)

	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Zero(t, cmd.BalanceCost)
	require.NotZero(t, usageLog.ID)
	require.Nil(t, result.PermanentBalanceDeduction)
	require.Nil(t, result.NewBalance)
	var balance, grantRemaining float64
	var consumptionCount, usageLogCount, dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT remaining_amount FROM temporary_credit_grants WHERE id = $1", grantID).Scan(&grantRemaining))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_credit_consumptions WHERE grant_id = $1", grantID).Scan(&consumptionCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&usageLogCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&dedupCount))
	require.Equal(t, 10.0, balance)
	require.Equal(t, 1.0, grantRemaining)
	require.Zero(t, consumptionCount)
	require.Equal(t, 1, usageLogCount)
	require.Equal(t, 1, dedupCount)
}

func TestUsageServiceCreate_EightDecimalZeroSkipsTemporaryAllocation(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	requestID := "usage-service-zero-" + uuid.NewString()
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-service-zero-%d@example.com", time.Now().UnixNano()),
		PasswordHash: "hash",
		Balance:      10,
	})
	account := mustCreateAccount(t, client, &service.Account{Name: "usage-service-zero-" + uuid.NewString(), Type: service.AccountTypeAPIKey})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-usage-service-zero-" + uuid.NewString(), Name: "usage-service-zero"})
	var grantID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO temporary_credit_grants (
    user_id, source, amount, remaining_amount, expires_at, notes, granted_by
)
VALUES ($1, 'admin_grant', 1, 1, clock_timestamp() + INTERVAL '1 hour', '', $1)
RETURNING id`, user.ID).Scan(&grantID))
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM temporary_credit_consumptions WHERE grant_id = $1", grantID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM temporary_credit_grants WHERE id = $1", grantID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM accounts WHERE id = $1", account.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	usageService := service.NewUsageService(
		NewUsageLogRepository(client, integrationDB),
		NewUserRepository(client, integrationDB),
		client,
		nil,
	)

	created, err := usageService.Create(ctx, service.CreateUsageLogRequest{
		UserID:     user.ID,
		APIKeyID:   apiKey.ID,
		AccountID:  account.ID,
		RequestID:  requestID,
		Model:      "gpt-5",
		TotalCost:  0.000000004,
		ActualCost: 0.000000004,
	})

	require.NoError(t, err)
	require.NotNil(t, created)
	require.NotZero(t, created.ID)
	var balance, grantRemaining float64
	var consumptionCount, usageLogCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id = $1", user.ID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT remaining_amount FROM temporary_credit_grants WHERE id = $1", grantID).Scan(&grantRemaining))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM temporary_credit_consumptions WHERE grant_id = $1", grantID).Scan(&consumptionCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT COUNT(*) FROM usage_logs WHERE request_id = $1 AND api_key_id = $2", requestID, apiKey.ID).Scan(&usageLogCount))
	require.Equal(t, 10.0, balance)
	require.Equal(t, 1.0, grantRemaining)
	require.Zero(t, consumptionCount)
	require.Equal(t, 1, usageLogCount)
}

func TestUsageBillingRepositoryApply_TemporaryFirstMixedCharge(t *testing.T) {
	fixture := newUsageBillingTemporaryCreditFixture(t, 10, 0.8)
	requestID := "usage-mixed-" + uuid.NewString()

	result, err := fixture.repo.Apply(fixture.ctx, &service.UsageBillingCommand{
		RequestID:   requestID,
		APIKeyID:    fixture.apiKeyID,
		UserID:      fixture.userID,
		BalanceCost: 1,
	})

	require.NoError(t, err)
	require.True(t, result.Applied)
	require.NotNil(t, result.PermanentBalanceDeduction)
	require.Equal(t, 0.2, *result.PermanentBalanceDeduction)
	balance, grantRemaining, consumptionCount, consumed := fixture.snapshot(t)
	require.Equal(t, 9.8, balance)
	require.Zero(t, grantRemaining)
	require.Equal(t, 1, consumptionCount)
	require.Equal(t, 0.8, consumed)
}

func TestUsageBillingRepositoryApply_ConcurrentChargesShareTemporaryCreditWithoutOverdraft(t *testing.T) {
	fixture := newUsageBillingTemporaryCreditFixture(t, 10, 1)
	type outcome struct {
		result *service.UsageBillingApplyResult
		err    error
	}
	outcomes := make(chan outcome, 2)
	for i := 0; i < 2; i++ {
		requestID := fmt.Sprintf("usage-concurrent-%d-%s", i, uuid.NewString())
		go func(requestID string) {
			result, err := fixture.repo.Apply(fixture.ctx, &service.UsageBillingCommand{
				RequestID:   requestID,
				APIKeyID:    fixture.apiKeyID,
				UserID:      fixture.userID,
				BalanceCost: 0.8,
			})
			outcomes <- outcome{result: result, err: err}
		}(requestID)
	}
	permanentTotal := 0.0
	for i := 0; i < 2; i++ {
		outcome := <-outcomes
		require.NoError(t, outcome.err)
		require.NotNil(t, outcome.result)
		require.True(t, outcome.result.Applied)
		require.NotNil(t, outcome.result.PermanentBalanceDeduction)
		permanentTotal += *outcome.result.PermanentBalanceDeduction
	}

	balance, grantRemaining, consumptionCount, consumed := fixture.snapshot(t)
	require.InDelta(t, 0.6, permanentTotal, 1e-12)
	require.InDelta(t, 9.4, balance, 1e-12)
	require.Zero(t, grantRemaining)
	require.Equal(t, 2, consumptionCount)
	require.Equal(t, 1.0, consumed)
}

type usageBillingTemporaryCreditFixture struct {
	ctx      context.Context
	repo     service.UsageBillingRepository
	userID   int64
	apiKeyID int64
	grantID  int64
}

func newUsageBillingTemporaryCreditFixture(t *testing.T, balance, grantAmount float64) *usageBillingTemporaryCreditFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("usage-credit-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      balance,
	})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: "sk-usage-credit-" + uuid.NewString(), Name: "usage-credit"})
	var grantID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO temporary_credit_grants (
    user_id, source, amount, remaining_amount, expires_at, notes, granted_by
)
VALUES ($1, 'admin_grant', $2, $2, clock_timestamp() + INTERVAL '1 hour', '', $1)
RETURNING id`, user.ID, grantAmount).Scan(&grantID))
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM temporary_credit_consumptions WHERE grant_id = $1", grantID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM temporary_credit_grants WHERE id = $1", grantID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM usage_billing_dedup WHERE api_key_id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM usage_billing_dedup_archive WHERE api_key_id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM api_keys WHERE id = $1", apiKey.ID)
		_, _ = integrationDB.ExecContext(cleanupCtx, "DELETE FROM users WHERE id = $1", user.ID)
	})
	return &usageBillingTemporaryCreditFixture{
		ctx:      ctx,
		repo:     NewUsageBillingRepository(client, integrationDB),
		userID:   user.ID,
		apiKeyID: apiKey.ID,
		grantID:  grantID,
	}
}

func (f *usageBillingTemporaryCreditFixture) snapshot(t *testing.T) (float64, float64, int, float64) {
	t.Helper()
	var balance, grantRemaining, consumed float64
	var consumptionCount int
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, "SELECT balance FROM users WHERE id = $1", f.userID).Scan(&balance))
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, "SELECT remaining_amount FROM temporary_credit_grants WHERE id = $1", f.grantID).Scan(&grantRemaining))
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, `
SELECT COUNT(*), COALESCE(SUM(amount), 0)
FROM temporary_credit_consumptions
WHERE grant_id = $1`, f.grantID).Scan(&consumptionCount, &consumed))
	return balance, grantRemaining, consumptionCount, consumed
}
