//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestAffiliateRebateOutbox_RedeemCreditsAndEnqueuesAtomically(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        "affiliate-redeem-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		Balance:      1,
		Status:       service.StatusActive,
		Role:         service.RoleUser,
	})
	redeemRepo := NewRedeemCodeRepository(integrationEntClient)
	code := &service.RedeemCode{
		Code:   "AFF-REDEEM-" + strings.ToUpper(uuid.NewString()[:12]),
		Type:   service.RedeemTypeBalance,
		Value:  2,
		Status: service.StatusUnused,
	}
	require.NoError(t, redeemRepo.Create(ctx, code))
	cleanupAffiliateRebateFixture(t, user.ID, code.ID)

	userRepo := NewUserRepository(integrationEntClient, integrationDB)
	affiliateRepo := NewAffiliateRepository(integrationEntClient, integrationDB)
	affiliateService := service.NewAffiliateService(affiliateRepo, nil, nil, nil)
	redeemService := service.NewRedeemService(
		redeemRepo,
		userRepo,
		nil,
		nil,
		nil,
		integrationEntClient,
		nil,
		affiliateService,
	)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			ready.Done()
			<-start
			_, err := redeemService.Redeem(ctx, user.ID, code.Code)
			errs <- err
		}()
	}
	ready.Wait()
	close(start)

	var success, used int
	for i := 0; i < 2; i++ {
		err := <-errs
		switch {
		case err == nil:
			success++
		case errors.Is(err, service.ErrRedeemCodeUsed):
			used++
		default:
			require.NoError(t, err)
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, used)
	require.InDelta(t, 3, persistedBalance(t, user.ID), 0.00000001)

	var jobCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM affiliate_rebate_jobs
WHERE source_redeem_code_id = $1
  AND invitee_user_id = $2
  AND source_kind = 'redeem'
  AND base_amount = 2`, code.ID, user.ID).Scan(&jobCount))
	require.Equal(t, 1, jobCount)
}

func TestAffiliateRebateOutbox_RedeemRollsBackWhenEnqueueFails(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        "affiliate-redeem-rollback-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		Balance:      4,
		Status:       service.StatusActive,
		Role:         service.RoleUser,
	})
	redeemRepo := NewRedeemCodeRepository(integrationEntClient)
	code := &service.RedeemCode{
		Code:   "AFF-ROLLBACK-" + strings.ToUpper(uuid.NewString()[:12]),
		Type:   service.RedeemTypeBalance,
		Value:  3,
		Status: service.StatusUnused,
	}
	require.NoError(t, redeemRepo.Create(ctx, code))
	cleanupAffiliateRebateFixture(t, user.ID, code.ID)

	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "fail_affiliate_job_" + suffix
	triggerName := "fail_affiliate_job_trigger_" + suffix
	_, err := integrationDB.ExecContext(ctx, fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger AS $$
BEGIN
    IF NEW.source_redeem_code_id = %d THEN
        RAISE EXCEPTION 'forced affiliate outbox failure';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER %s
BEFORE INSERT ON affiliate_rebate_jobs
FOR EACH ROW EXECUTE FUNCTION %s();`, functionName, code.ID, triggerName, functionName))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON affiliate_rebate_jobs", triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})

	userRepo := NewUserRepository(integrationEntClient, integrationDB)
	affiliateService := service.NewAffiliateService(NewAffiliateRepository(integrationEntClient, integrationDB), nil, nil, nil)
	redeemService := service.NewRedeemService(redeemRepo, userRepo, nil, nil, nil, integrationEntClient, nil, affiliateService)
	_, err = redeemService.Redeem(ctx, user.ID, code.Code)
	require.Error(t, err)
	require.Contains(t, err.Error(), "enqueue affiliate rebate job")
	require.InDelta(t, 4, persistedBalance(t, user.ID), 0.00000001)

	var status string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status FROM redeem_codes WHERE id = $1`, code.ID).Scan(&status))
	require.Equal(t, service.StatusUnused, status)
}

func TestAffiliateRebateWorker_ConcurrentJobsRespectCapAndReplayIsIdempotent(t *testing.T) {
	ctx := context.Background()
	settingRepo := NewSettingRepository(integrationEntClient)
	restoreAffiliateSettings(t, settingRepo)
	require.NoError(t, settingRepo.SetMultiple(ctx, map[string]string{
		service.SettingKeyAffiliateEnabled:             "true",
		service.SettingKeyAffiliateRebateRate:          "100",
		service.SettingKeyAffiliateRebateFreezeHours:   "0",
		service.SettingKeyAffiliateRebateDurationDays:  "0",
		service.SettingKeyAffiliateRebatePerInviteeCap: "1",
	}))
	settingService := service.NewSettingService(settingRepo, nil)

	inviter, invitee := createBoundAffiliateUsers(t, ctx)
	codeIDs := createAffiliateSourceCodes(t, ctx, invitee.ID, 0.8, 2)
	for _, codeID := range codeIDs {
		cleanupAffiliateRebateFixture(t, invitee.ID, codeID)
	}
	affiliateRepo := NewAffiliateRepository(integrationEntClient, integrationDB)
	affiliateService := service.NewAffiliateService(affiliateRepo, settingService, nil, nil)
	for _, codeID := range codeIDs {
		require.NoError(t, affiliateService.EnqueueAffiliateRebateJob(ctx, service.AffiliateRebateJobInput{
			InviteeUserID:      invitee.ID,
			SourceRedeemCodeID: codeID,
			SourceKind:         service.AffiliateRebateSourceRedeem,
			BaseAmount:         0.8,
		}))
	}
	worker := service.NewAffiliateRebateWorker(integrationEntClient, affiliateService, settingService)

	start := make(chan struct{})
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			<-start
			processed, err := worker.ProcessOnce(ctx)
			if !processed && err == nil {
				err = errors.New("expected one affiliate job")
			}
			errs <- err
		}()
	}
	close(start)
	require.NoError(t, <-errs)
	require.NoError(t, <-errs)

	var total float64
	var ledgerCount int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COALESCE(SUM(amount), 0)::double precision, COUNT(*)
FROM user_affiliate_ledger
WHERE user_id = $1 AND source_user_id = $2 AND action = 'accrue'`, inviter.ID, invitee.ID).Scan(&total, &ledgerCount))
	require.InDelta(t, 1, total, 0.00000001)
	require.Equal(t, 2, ledgerCount)

	var succeeded int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COUNT(*) FROM affiliate_rebate_jobs
WHERE source_redeem_code_id = ANY($1) AND status = 'succeeded'`, pq.Array(codeIDs)).Scan(&succeeded))
	require.Equal(t, 2, succeeded)

	// Simulate a stale status repair. The ledger's partial unique index is the
	// second idempotency barrier, so replay cannot credit quota again.
	_, err := integrationDB.ExecContext(ctx, `
UPDATE affiliate_rebate_jobs
SET status = 'failed', next_retry_at = NOW(), succeeded_at = NULL, updated_at = NOW()
WHERE source_redeem_code_id = $1`, codeIDs[0])
	require.NoError(t, err)
	processed, err := worker.ProcessOnce(ctx)
	require.True(t, processed)
	require.NoError(t, err)

	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT COALESCE(SUM(amount), 0)::double precision, COUNT(*)
FROM user_affiliate_ledger
WHERE user_id = $1 AND source_user_id = $2 AND action = 'accrue'`, inviter.ID, invitee.ID).Scan(&total, &ledgerCount))
	require.InDelta(t, 1, total, 0.00000001)
	require.Equal(t, 2, ledgerCount)
}

func TestAffiliateRebateWorker_ConfigFailureRetriesAfterRepair(t *testing.T) {
	ctx := context.Background()
	settingRepo := NewSettingRepository(integrationEntClient)
	restoreAffiliateSettings(t, settingRepo)
	require.NoError(t, settingRepo.SetMultiple(ctx, map[string]string{
		service.SettingKeyAffiliateEnabled:    "true",
		service.SettingKeyAffiliateRebateRate: "invalid",
	}))
	settingService := service.NewSettingService(settingRepo, nil)

	_, invitee := createBoundAffiliateUsers(t, ctx)
	codeID := createAffiliateSourceCodes(t, ctx, invitee.ID, 1, 1)[0]
	cleanupAffiliateRebateFixture(t, invitee.ID, codeID)
	affiliateService := service.NewAffiliateService(NewAffiliateRepository(integrationEntClient, integrationDB), settingService, nil, nil)
	require.NoError(t, affiliateService.EnqueueAffiliateRebateJob(ctx, service.AffiliateRebateJobInput{
		InviteeUserID:      invitee.ID,
		SourceRedeemCodeID: codeID,
		SourceKind:         service.AffiliateRebateSourceRedeem,
		BaseAmount:         1,
	}))
	worker := service.NewAffiliateRebateWorker(integrationEntClient, affiliateService, settingService)

	processed, err := worker.ProcessOnce(ctx)
	require.True(t, processed)
	require.Error(t, err)
	var status, lastError string
	var attempts int
	var nextRetry time.Time
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status, attempts, next_retry_at, last_error
FROM affiliate_rebate_jobs
WHERE source_redeem_code_id = $1`, codeID).Scan(&status, &attempts, &nextRetry, &lastError))
	require.Equal(t, "failed", status)
	require.Equal(t, 1, attempts)
	require.True(t, nextRetry.After(time.Now().Add(-time.Second)))
	require.Contains(t, lastError, service.SettingKeyAffiliateRebateRate)

	require.NoError(t, settingRepo.Set(ctx, service.SettingKeyAffiliateRebateRate, "50"))
	_, err = integrationDB.ExecContext(ctx, `
UPDATE affiliate_rebate_jobs SET next_retry_at = NOW() WHERE source_redeem_code_id = $1`, codeID)
	require.NoError(t, err)
	processed, err = worker.ProcessOnce(ctx)
	require.True(t, processed)
	require.NoError(t, err)

	var ledgerAmount float64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT amount::double precision
FROM user_affiliate_ledger
WHERE source_redeem_code_id = $1`, codeID).Scan(&ledgerAmount))
	require.InDelta(t, 0.5, ledgerAmount, 0.00000001)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `
SELECT status, attempts FROM affiliate_rebate_jobs WHERE source_redeem_code_id = $1`, codeID).Scan(&status, &attempts))
	require.Equal(t, "succeeded", status)
	require.Equal(t, 2, attempts)
}

func createBoundAffiliateUsers(t *testing.T, ctx context.Context) (*service.User, *service.User) {
	t.Helper()
	inviter := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        "affiliate-worker-inviter-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		Status:       service.StatusActive,
		Role:         service.RoleUser,
	})
	invitee := mustCreateUser(t, integrationEntClient, &service.User{
		Email:        "affiliate-worker-invitee-" + uuid.NewString() + "@example.com",
		PasswordHash: "hash",
		Status:       service.StatusActive,
		Role:         service.RoleUser,
	})
	repo := NewAffiliateRepository(integrationEntClient, integrationDB)
	_, err := repo.EnsureUserAffiliate(ctx, inviter.ID)
	require.NoError(t, err)
	_, err = repo.EnsureUserAffiliate(ctx, invitee.ID)
	require.NoError(t, err)
	bound, err := repo.BindInviter(ctx, invitee.ID, inviter.ID)
	require.NoError(t, err)
	require.True(t, bound)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM user_affiliate_ledger WHERE user_id = $1 OR source_user_id = $2", inviter.ID, invitee.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM user_affiliates WHERE user_id IN ($1, $2)", inviter.ID, invitee.ID)
		_, _ = integrationDB.ExecContext(context.Background(), "DELETE FROM users WHERE id IN ($1, $2)", inviter.ID, invitee.ID)
	})
	return inviter, invitee
}

func createAffiliateSourceCodes(t *testing.T, ctx context.Context, userID int64, amount float64, count int) []int64 {
	t.Helper()
	ids := make([]int64, 0, count)
	for i := 0; i < count; i++ {
		code, err := integrationEntClient.RedeemCode.Create().
			SetCode("AFF-SOURCE-" + strings.ToUpper(uuid.NewString()[:12])).
			SetType(service.RedeemTypeBalance).
			SetValue(amount).
			SetStatus(service.StatusUsed).
			SetUsedBy(userID).
			SetUsedAt(time.Now().UTC()).
			Save(ctx)
		require.NoError(t, err)
		ids = append(ids, code.ID)
	}
	return ids
}

func cleanupAffiliateRebateFixture(t *testing.T, userID, sourceRedeemCodeID int64) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM user_affiliate_ledger WHERE source_redeem_code_id = $1", sourceRedeemCodeID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM affiliate_rebate_jobs WHERE source_redeem_code_id = $1", sourceRedeemCodeID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM redeem_codes WHERE id = $1", sourceRedeemCodeID)
		_, _ = integrationDB.ExecContext(ctx, "DELETE FROM users WHERE id = $1", userID)
	})
}

func restoreAffiliateSettings(t *testing.T, repo service.SettingRepository) {
	t.Helper()
	ctx := context.Background()
	keys := []string{
		service.SettingKeyAffiliateEnabled,
		service.SettingKeyAffiliateAdminRechargeEnabled,
		service.SettingKeyAffiliateRebateRate,
		service.SettingKeyAffiliateRebateFreezeHours,
		service.SettingKeyAffiliateRebateDurationDays,
		service.SettingKeyAffiliateRebatePerInviteeCap,
	}
	type snapshot struct {
		value  string
		exists bool
	}
	snapshots := make(map[string]snapshot, len(keys))
	for _, key := range keys {
		value, err := repo.GetValue(ctx, key)
		if err == nil {
			snapshots[key] = snapshot{value: value, exists: true}
			continue
		}
		require.ErrorIs(t, err, service.ErrSettingNotFound)
	}
	t.Cleanup(func() {
		for _, key := range keys {
			stored := snapshots[key]
			if stored.exists {
				_ = repo.Set(context.Background(), key, stored.value)
			} else {
				_ = repo.Delete(context.Background(), key)
			}
		}
	})
}
