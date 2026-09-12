//go:build integration

package repository

// These tests are prepared for the explicit dedicated-DB gate. Compile-only
// validation is safe; running them requires integration_harness_test.go's
// target fingerprint/fixture-reset authorization. They are not parallel because
// the check-in case temporarily replaces a complete test policy bundle.
import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestPersonalFeaturesBankExpiryRefundIsSourceBoundAndAtMostOnce(t *testing.T) {
	ctx := context.Background()
	user := mustCreateUser(t, testEntClient(t), &service.User{})
	t.Cleanup(func() {
		for _, query := range []string{
			`DELETE FROM bank_ledger WHERE user_id=$1`,
			`DELETE FROM bank_exchange_expiry_settlements WHERE user_id=$1`,
			`DELETE FROM bank_exchange_grant_snapshots WHERE user_id=$1`,
			`DELETE FROM temporary_credit_grants WHERE user_id=$1`,
			`DELETE FROM users WHERE id=$1`,
		} {
			_, err := integrationDB.ExecContext(ctx, query, user.ID)
			require.NoError(t, err)
		}
	})
	var refundGrant, legacyGrant int64
	for i, dest := range []*int64{&refundGrant, &legacyGrant} {
		source := "bank_exchange"
		if i == 1 {
			source = "bank_advance"
		}
		require.NoError(t, integrationDB.QueryRowContext(ctx, `
INSERT INTO temporary_credit_grants(user_id,source,amount,remaining_amount,available_at,expires_at)
VALUES($1,$2,20,8,NOW()-INTERVAL '2 days',NOW()-INTERVAL '1 day') RETURNING id`, user.ID, source).Scan(dest))
	}
	_, err := integrationDB.ExecContext(ctx, `INSERT INTO bank_exchange_grant_snapshots(grant_id,user_id,principal_permanent,generated_temporary,fee_bps,policy_version,expires_at) VALUES($1,$2,10,20,1000,1,NOW()-INTERVAL '1 day')`, refundGrant, user.ID)
	require.NoError(t, err)
	bank := service.NewBankService(integrationDB, NewTemporaryCreditRepository(integrationDB), nil)
	errs := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- bank.SettleDueForUser(ctx, user.ID) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var balance, remaining, principal, fee, net string
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.Equal(t, "3.60000000", balance)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT refundable_principal::text,fee_amount::text,net_refund::text FROM bank_exchange_expiry_settlements WHERE grant_id=$1`, refundGrant).Scan(&principal, &fee, &net))
	require.Equal(t, "4.00000000", principal)
	require.Equal(t, "0.40000000", fee)
	require.Equal(t, "3.60000000", net)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM bank_ledger WHERE user_id=$1 AND operation='exchange_expiry_refund'`, user.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT remaining_amount::text FROM temporary_credit_grants WHERE id=$1`, legacyGrant).Scan(&remaining))
	require.Equal(t, "8.00000000", remaining)
}

func TestPersonalFeaturesPlayerInvitationRetryReserveAndClaimAreAtomic(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	inviter := mustCreateUser(t, client, &service.User{})
	invitee := mustCreateUser(t, client, &service.User{})
	ids := []int64{invitee.ID, inviter.ID}
	t.Cleanup(func() {
		for _, id := range ids {
			_, err := integrationDB.ExecContext(ctx, `DELETE FROM player_invitation_relations WHERE invitee_user_id=$1`, id)
			require.NoError(t, err)
		}
		for _, id := range ids {
			for _, query := range []string{`DELETE FROM player_invitation_reservations WHERE inviter_user_id=$1`, `DELETE FROM player_invitation_quota_events WHERE user_id=$1`, `DELETE FROM user_affiliates WHERE user_id=$1`, `DELETE FROM users WHERE id=$1`} {
				_, err := integrationDB.ExecContext(ctx, query, id)
				require.NoError(t, err)
			}
		}
	})
	repo := NewPlayerInvitationRepository(client, integrationDB)
	policy := service.PlayerInvitationPolicyFunc(func(context.Context) (service.PlayerInvitationPolicy, error) {
		return service.PlayerInvitationPolicy{Enabled: true, ReservationTTL: time.Hour}, nil
	})
	invitations := service.NewPlayerInvitationService(repo, nil, policy)
	invitations.SetSigningKey("integration-only-invitation-signing-secret")
	key := fmt.Sprintf("test-reserve-%d-stable", inviter.ID)
	first, err := invitations.Reserve(ctx, inviter.ID, key)
	require.NoError(t, err)
	replay, err := invitations.Reserve(ctx, inviter.ID, key)
	require.NoError(t, err)
	require.Equal(t, first.Token, replay.Token)
	require.Equal(t, first.Reservation.ID, replay.Reservation.ID)
	_, err = invitations.Reserve(ctx, inviter.ID, key+"different")
	require.ErrorIs(t, err, service.ErrInvitationQuotaExhausted)
	relation, err := invitations.ClaimRegistration(ctx, first.Token, invitee.ID, &inviter.ID)
	require.NoError(t, err)
	require.Equal(t, inviter.ID, relation.InviterUserID)
	_, err = invitations.ClaimRegistration(ctx, first.Token, invitee.ID, &inviter.ID)
	require.NoError(t, err)
	summary, err := invitations.GetSummary(ctx, inviter.ID)
	require.NoError(t, err)
	require.EqualValues(t, 1, summary.Consumed)
	require.Zero(t, summary.Available)
	require.Zero(t, summary.Reserved)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT aff_count FROM user_affiliates WHERE user_id=$1`, inviter.ID).Scan(&count))
	require.Equal(t, 1, count)
}

func TestPersonalFeaturesCheckinV2ConsentAndReplay(t *testing.T) {
	ctx := context.Background()
	client := testEntClient(t)
	personalFeatureSettingsFixture(t, map[string]string{
		service.SettingKeyDailyCheckinEnabled: "true", service.SettingKeyDailyCheckinMaxRewardDay: "1",
		service.SettingKeyDailyCheckinRewardTiers: `[{"day":1,"amount":"4.00000000","permanent_amount":"1.00000000"}]`,
		service.SettingKeyDailyCheckinPolicyV2:    `{"version":"test","refresh_time":"00:00","auto_fee_bps":500,"normal":{"enabled":false,"min_bps":10000,"max_bps":10000},"super":{"enabled":false,"min_bps":10000,"max_bps":10000,"cost":"0.00000000"}}`,
	})
	user := mustCreateUser(t, client, &service.User{})
	cleanupCheckinIntegrationUser(t, user.ID, fmt.Sprintf("user:%d", user.ID))
	t.Cleanup(func() {
		_, err := integrationDB.ExecContext(ctx, `DELETE FROM daily_checkin_preferences WHERE user_id=$1`, user.ID)
		require.NoError(t, err)
	})
	settings := service.NewSettingService(NewSettingRepository(client), &config.Config{})
	checkin := service.NewCheckinServiceV2(integrationDB, settings, service.NewTemporaryCreditService(NewTemporaryCreditRepository(integrationDB)))
	preference, err := checkin.GetPreference(ctx, user.ID)
	require.NoError(t, err)
	require.False(t, preference.AutoEnabled)
	_, err = checkin.UpdatePreference(ctx, user.ID, true, true, "stale", 500)
	require.ErrorIs(t, err, service.ErrCheckinPolicyVersionStale)
	_, err = checkin.UpdatePreference(ctx, user.ID, true, true, preference.CurrentPolicyVersion, preference.CurrentFeeBps)
	require.NoError(t, err)
	cfg := service.DefaultIdempotencyConfig()
	cfg.ObserveOnly = false
	coordinator := service.NewIdempotencyCoordinator(NewIdempotencyRepository(client, integrationDB), cfg)
	claim := func(key string) *service.CheckinResult {
		result, err := coordinator.ExecuteAtomic(ctx, integrationCheckinOptions(user.ID, key), func(ctx context.Context, claim *service.IdempotencyAtomicClaim) (any, error) {
			return checkin.AutoCheckInAtomic(ctx, user.ID, claim)
		})
		require.NoError(t, err)
		reward, ok := result.Data.(*service.CheckinResult)
		require.True(t, ok)
		return reward
	}
	first := claim("personal-v2-auto-first")
	require.Equal(t, "3.80000000", first.RewardAmount)
	require.Equal(t, "1.00000000", first.PermanentRewardAmount)
	second := claim("personal-v2-auto-different-key")
	require.True(t, second.AlreadyCheckedIn)
	require.Equal(t, first.TemporaryCreditGrantID, second.TemporaryCreditGrantID)
	var balance string
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT balance::text FROM users WHERE id=$1`, user.ID).Scan(&balance))
	require.Equal(t, "1.00000000", balance)
}

func personalFeatureSettingsFixture(t *testing.T, updates map[string]string) {
	t.Helper()
	ctx := context.Background()
	saved := map[string]*string{}
	for key, value := range updates {
		var previous string
		err := integrationDB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, key).Scan(&previous)
		if err == nil {
			copyValue := previous
			saved[key] = &copyValue
		} else {
			require.ErrorIs(t, err, sql.ErrNoRows)
			saved[key] = nil
		}
		_, err = integrationDB.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value`, key, value)
		require.NoError(t, err)
	}
	t.Cleanup(func() {
		for key, previous := range saved {
			var err error
			if previous == nil {
				_, err = integrationDB.ExecContext(ctx, `DELETE FROM settings WHERE key=$1`, key)
			} else {
				_, err = integrationDB.ExecContext(ctx, `UPDATE settings SET value=$2 WHERE key=$1`, key, *previous)
			}
			require.NoError(t, err)
		}
	})
}
