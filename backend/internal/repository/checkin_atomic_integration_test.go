//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const integrationCheckinScope = "user.daily_checkin.create"

func TestCheckInAtomic_RealPostgresCommitsStableDTOAndExistingDayPriority(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, testEntClient(t), &service.User{})
	actorScope := fmt.Sprintf("user:%d", user.ID)
	cleanupCheckinIntegrationUser(t, user.ID, actorScope)

	policyProvider := newEnabledIntegrationCheckinPolicy()
	checkinService, coordinator := newIntegrationCheckinRuntime(policyProvider)

	var dbBefore time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&dbBefore))
	first, err := coordinator.ExecuteAtomic(
		ctx,
		integrationCheckinOptions(user.ID, "first-real-pg-key"),
		func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
			return checkinService.CheckInAtomic(ctx, user.ID, claim)
		},
	)
	require.NoError(t, err)
	require.False(t, first.Replayed)
	firstResult, ok := first.Data.(*service.CheckinResult)
	require.True(t, ok)
	require.False(t, firstResult.AlreadyCheckedIn)
	require.Equal(t, 1, firstResult.StreakDay)
	require.Equal(t, 1, firstResult.RewardDay)
	require.Equal(t, "2.50000000", firstResult.RewardAmount)
	require.Equal(t, time.UTC, firstResult.ExpiresAt.Location())

	var dbAfter time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&dbAfter))
	possibleBusinessDates := []string{
		service.BeijingBusinessDate(dbBefore),
		service.BeijingBusinessDate(dbAfter),
	}
	require.Contains(t, possibleBusinessDates, firstResult.CheckinDate)
	expectedExpiry := service.NextBeijingMidnight(dbAfter).UTC()
	if firstResult.CheckinDate == service.BeijingBusinessDate(dbBefore) {
		expectedExpiry = service.NextBeijingMidnight(dbBefore).UTC()
	}
	require.Equal(t, expectedExpiry, firstResult.ExpiresAt)

	var (
		checkinID          int64
		persistedDate      string
		persistedStreak    int
		persistedRewardDay int
		persistedAmount    string
		grantID            int64
		grantAmount        string
		remainingAmount    string
		persistedExpiry    time.Time
	)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT c.id,
       c.checkin_date::text,
       c.streak_day,
       c.reward_day,
       c.reward_amount::text,
       g.id,
       g.amount::text,
       g.remaining_amount::text,
       g.expires_at
FROM daily_checkins c
JOIN temporary_credit_grants g ON g.checkin_id = c.id
WHERE c.user_id = $1`, user.ID).Scan(
		&checkinID,
		&persistedDate,
		&persistedStreak,
		&persistedRewardDay,
		&persistedAmount,
		&grantID,
		&grantAmount,
		&remainingAmount,
		&persistedExpiry,
	))
	require.Positive(t, checkinID)
	require.Equal(t, firstResult.CheckinDate, persistedDate)
	require.Equal(t, firstResult.StreakDay, persistedStreak)
	require.Equal(t, firstResult.RewardDay, persistedRewardDay)
	require.Equal(t, firstResult.RewardAmount, persistedAmount)
	require.Equal(t, firstResult.TemporaryCreditGrantID, grantID)
	require.Equal(t, "2.50000000", grantAmount)
	require.Equal(t, grantAmount, remainingAmount)
	require.Equal(t, firstResult.ExpiresAt, persistedExpiry.UTC())
	requireAtomicSuccessSnapshot(t, ctx, actorScope, "first-real-pg-key", false, firstResult)

	policyProvider.policy.Enabled = false
	second, err := coordinator.ExecuteAtomic(
		ctx,
		integrationCheckinOptions(user.ID, "second-real-pg-key"),
		func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
			return checkinService.CheckInAtomic(ctx, user.ID, claim)
		},
	)
	require.NoError(t, err, "an existing same-day check-in must win before the disabled policy")
	secondResult, ok := second.Data.(*service.CheckinResult)
	require.True(t, ok)
	require.True(t, secondResult.AlreadyCheckedIn)
	require.Equal(t, firstResult.CheckinDate, secondResult.CheckinDate)
	require.Equal(t, firstResult.StreakDay, secondResult.StreakDay)
	require.Equal(t, firstResult.RewardDay, secondResult.RewardDay)
	require.Equal(t, firstResult.RewardAmount, secondResult.RewardAmount)
	require.Equal(t, firstResult.TemporaryCreditGrantID, secondResult.TemporaryCreditGrantID)
	require.Equal(t, firstResult.ExpiresAt, secondResult.ExpiresAt)
	require.Equal(t, 1, policyProvider.calls, "existing-day path must not reload the now-disabled policy")
	requireAtomicSuccessSnapshot(t, ctx, actorScope, "second-real-pg-key", true, secondResult)

	var checkinCount, grantCount, succeededCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM daily_checkins WHERE user_id = $1`, user.ID).Scan(&checkinCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM temporary_credit_grants WHERE user_id = $1`, user.ID).Scan(&grantCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM idempotency_records
WHERE operation_scope = $1 AND actor_scope = $2 AND status = $3`,
		integrationCheckinScope, actorScope, service.IdempotencyStatusSucceeded,
	).Scan(&succeededCount))
	require.Equal(t, 1, checkinCount)
	require.Equal(t, 1, grantCount)
	require.Equal(t, 2, succeededCount)
}

func TestCheckInAtomic_RealPostgresDTOPersistenceFailureRollsBackLedger(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, testEntClient(t), &service.User{})
	actorScope := fmt.Sprintf("user:%d", user.ID)
	cleanupCheckinIntegrationUser(t, user.ID, actorScope)

	checkinService, coordinator := newIntegrationCheckinRuntime(newEnabledIntegrationCheckinPolicy())
	key := "dto-persistence-failure-key"
	_, err := coordinator.ExecuteAtomic(
		ctx,
		integrationCheckinOptions(user.ID, key),
		func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
			result, tamperErr := integrationDB.ExecContext(ctx, `
UPDATE idempotency_records
SET request_fingerprint = repeat('0', 64)
WHERE operation_scope = $1
  AND actor_scope = $2
  AND idempotency_key_hash = $3`, integrationCheckinScope, actorScope, service.HashIdempotencyKey(key))
			if tamperErr != nil {
				return nil, tamperErr
			}
			affected, rowsErr := result.RowsAffected()
			if rowsErr != nil {
				return nil, rowsErr
			}
			if affected != 1 {
				return nil, fmt.Errorf("tamper processing fingerprint: affected=%d", affected)
			}
			return checkinService.CheckInAtomic(ctx, user.ID, claim)
		},
	)
	require.Error(t, err)

	var checkinCount, grantCount, succeededCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM daily_checkins WHERE user_id = $1`, user.ID).Scan(&checkinCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM temporary_credit_grants WHERE user_id = $1`, user.ID).Scan(&grantCount))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM idempotency_records
WHERE operation_scope = $1 AND actor_scope = $2 AND status = $3`,
		integrationCheckinScope, actorScope, service.IdempotencyStatusSucceeded,
	).Scan(&succeededCount))
	require.Zero(t, checkinCount, "check-in must roll back when its success DTO cannot be persisted")
	require.Zero(t, grantCount, "grant must roll back when its success DTO cannot be persisted")
	require.Zero(t, succeededCount)
}

type integrationCheckinPolicyProvider struct {
	policy service.DailyCheckinPolicy
	calls  int
}

func newEnabledIntegrationCheckinPolicy() *integrationCheckinPolicyProvider {
	policy := service.DefaultDailyCheckinPolicy()
	policy.Enabled = true
	for index := range policy.RewardTiers {
		policy.RewardTiers[index].Amount = 2.5
	}
	return &integrationCheckinPolicyProvider{policy: policy}
}

func (p *integrationCheckinPolicyProvider) GetDailyCheckinPolicy(context.Context) (*service.DailyCheckinPolicy, error) {
	p.calls++
	policy := p.policy
	policy.RewardTiers = append([]service.DailyCheckinRewardTier(nil), p.policy.RewardTiers...)
	return &policy, nil
}

func newIntegrationCheckinRuntime(policyProvider service.DailyCheckinPolicyProvider) (*service.CheckinService, *service.IdempotencyCoordinator) {
	temporaryCreditService := service.NewTemporaryCreditService(NewTemporaryCreditRepository(integrationDB))
	checkinService := service.NewCheckinService(integrationDB, policyProvider, temporaryCreditService)
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	coordinator := service.NewIdempotencyCoordinator(
		NewIdempotencyRepository(integrationEntClient, integrationDB),
		cfg,
	)
	return checkinService, coordinator
}

func integrationCheckinOptions(userID int64, key string) service.IdempotencyExecuteOptions {
	return service.IdempotencyExecuteOptions{
		Scope:          integrationCheckinScope,
		ActorScope:     fmt.Sprintf("user:%d", userID),
		Method:         http.MethodPost,
		Route:          "/api/v1/user/check-in",
		IdempotencyKey: key,
		Payload:        map[string]json.RawMessage{},
		TTL:            time.Hour,
		RequireKey:     true,
	}
}

func requireAtomicSuccessSnapshot(
	t *testing.T,
	ctx context.Context,
	actorScope string,
	key string,
	alreadyCheckedIn bool,
	want *service.CheckinResult,
) {
	t.Helper()
	var status, responseBody string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status, response_body
FROM idempotency_records
WHERE operation_scope = $1
  AND actor_scope = $2
  AND idempotency_key_hash = $3`,
		integrationCheckinScope, actorScope, service.HashIdempotencyKey(key),
	).Scan(&status, &responseBody))
	require.Equal(t, service.IdempotencyStatusSucceeded, status)

	var snapshot map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(responseBody), &snapshot))
	require.Len(t, snapshot, 7)
	require.Equal(t, json.RawMessage(fmt.Sprintf("%t", alreadyCheckedIn)), snapshot["already_checked_in"])
	require.Equal(t, json.RawMessage(fmt.Sprintf("%q", want.CheckinDate)), snapshot["checkin_date"])
	require.Equal(t, json.RawMessage(fmt.Sprintf("%d", want.StreakDay)), snapshot["streak_day"])
	require.Equal(t, json.RawMessage(fmt.Sprintf("%d", want.RewardDay)), snapshot["reward_day"])
	require.Equal(t, json.RawMessage(fmt.Sprintf("%q", want.RewardAmount)), snapshot["reward_amount"])
	require.Equal(t, json.RawMessage(fmt.Sprintf("%d", want.TemporaryCreditGrantID)), snapshot["temporary_credit_grant_id"])
	require.Equal(t, json.RawMessage(fmt.Sprintf("%q", want.ExpiresAt.Format(time.RFC3339Nano))), snapshot["expires_at"])
}

func cleanupCheckinIntegrationUser(t *testing.T, userID int64, actorScope string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM idempotency_records WHERE actor_scope = $1`, actorScope)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM temporary_credit_grants WHERE user_id = $1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM daily_checkins WHERE user_id = $1`, userID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	})
}
