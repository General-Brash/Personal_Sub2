//go:build integration

package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type batchImageHoldIntegrationFixture struct {
	ctx      context.Context
	repo     service.UsageBillingRepository
	userID   int64
	apiKeyID int64
	groupID  int64
	batchID  string
	hold     float64
}

func TestUsageBillingRepositoryBatchImageReserveUsesTemporaryCreditFirst(t *testing.T) {
	t.Run("FEFO temporary-only", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1.2)
		earlier := fixture.createGrant(t, 0.8, time.Now().Add(time.Hour))
		later := fixture.createGrant(t, 1, time.Now().Add(2*time.Hour))

		result, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		require.True(t, result.Applied)
		require.Equal(t, 1.2, result.TemporaryReservedAmount)
		require.Zero(t, result.PermanentReservedAmount)
		require.Equal(t, 0.0, fixture.grantRemaining(t, earlier))
		require.Equal(t, 0.6, fixture.grantRemaining(t, later))
		require.Equal(t, []float64{0.8, 0.4}, fixture.allocationAmounts(t))
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)

		var groupID int64
		require.NoError(t, integrationDB.QueryRowContext(fixture.ctx,
			"SELECT group_id FROM batch_image_credit_holds WHERE batch_id = $1", fixture.batchID,
		).Scan(&groupID))
		require.Equal(t, fixture.groupID, groupID)
	})

	t.Run("mixed temporary and permanent", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 0.8, time.Now().Add(time.Hour))

		result, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		require.Equal(t, 0.8, result.TemporaryReservedAmount)
		require.Equal(t, 0.2, result.PermanentReservedAmount)
		require.Zero(t, fixture.grantRemaining(t, grantID))
		balance, frozen := fixture.balance(t)
		require.Equal(t, 9.8, balance)
		require.Equal(t, 0.2, frozen)
	})

	t.Run("grant selected while valid remains eligible after crossing expiry under lock", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 1, time.Now().Add(time.Hour))
		installBatchImageGrantUpdateDelay(t, grantID, 1.5)
		_, err := integrationDB.ExecContext(fixture.ctx, `
UPDATE temporary_credit_grants
SET expires_at = clock_timestamp() + INTERVAL '1 second'
WHERE id = $1`, grantID)
		require.NoError(t, err)

		result, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		require.Equal(t, 1.0, result.TemporaryReservedAmount)
		require.Zero(t, result.PermanentReservedAmount)
		require.Zero(t, fixture.grantRemaining(t, grantID))
		var expired bool
		require.NoError(t, integrationDB.QueryRowContext(fixture.ctx,
			"SELECT expires_at <= clock_timestamp() FROM temporary_credit_grants WHERE id = $1", grantID,
		).Scan(&expired))
		require.True(t, expired)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)
	})

	t.Run("grant already expired at selection is skipped for permanent fallback", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 1, time.Now().Add(-time.Minute))

		result, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		require.Zero(t, result.TemporaryReservedAmount)
		require.Equal(t, 1.0, result.PermanentReservedAmount)
		require.Equal(t, 1.0, fixture.grantRemaining(t, grantID))
		require.Empty(t, fixture.allocationAmounts(t))
		balance, frozen := fixture.balance(t)
		require.Equal(t, 9.0, balance)
		require.Equal(t, 1.0, frozen)
	})

	t.Run("permanent-only", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)

		result, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		require.Zero(t, result.TemporaryReservedAmount)
		require.Equal(t, 1.0, result.PermanentReservedAmount)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 9.0, balance)
		require.Equal(t, 1.0, frozen)

		captured, err := fixture.repo.CaptureBatchImageBalance(fixture.ctx, fixture.captureCommand(0.6, "manifest"))
		require.NoError(t, err)
		require.Zero(t, captured.TemporaryCapturedAmount)
		require.Equal(t, 0.6, captured.PermanentCapturedAmount)
		balance, frozen = fixture.balance(t)
		require.Equal(t, 9.4, balance)
		require.Zero(t, frozen)
	})

	t.Run("ninth decimal is rounded once at the ledger boundary", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1.123456789)

		result, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		require.Equal(t, 1.12345679, result.PermanentReservedAmount)
		var stored string
		require.NoError(t, integrationDB.QueryRowContext(fixture.ctx,
			"SELECT hold_amount::text FROM batch_image_credit_holds WHERE batch_id = $1", fixture.batchID,
		).Scan(&stored))
		require.Equal(t, "1.12345679", stored)
	})

	t.Run("combined credit insufficiency rolls back", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 0.5, 1)
		grantID := fixture.createGrant(t, 0.4, time.Now().Add(time.Hour))

		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.ErrorIs(t, err, service.ErrBatchImageInsufficientBalance)
		require.Equal(t, 0.4, fixture.grantRemaining(t, grantID))
		balance, frozen := fixture.balance(t)
		require.Equal(t, 0.5, balance)
		require.Zero(t, frozen)
		fixture.requireLedgerCounts(t, 0, 0, 0)
	})
}

func TestUsageBillingRepositoryBatchImageCaptureAndRelease(t *testing.T) {
	t.Run("partial capture refunds valid temporary and all unused permanent", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 0.8, time.Now().Add(time.Hour))
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)

		capture := fixture.captureCommand(0.5, "manifest")
		result, err := fixture.repo.CaptureBatchImageBalance(fixture.ctx, capture)
		require.NoError(t, err)
		require.True(t, result.Applied)
		require.Equal(t, 0.5, result.TemporaryCapturedAmount)
		require.Zero(t, result.PermanentCapturedAmount)
		require.Equal(t, 0.3, result.TemporaryRefundedAmount)
		require.Zero(t, result.TemporaryExpiredAmount)
		require.Equal(t, 0.3, fixture.grantRemaining(t, grantID))
		consumptionCount, consumed := fixture.temporaryConsumption(t, capture.RequestID)
		require.Equal(t, 1, consumptionCount)
		require.Equal(t, 0.5, consumed)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)
		fixture.requireHold(t, "captured", 0.5, 0.5, 0, 0)

		duplicate, err := fixture.repo.CaptureBatchImageBalance(fixture.ctx, fixture.captureCommand(0.5, "manifest"))
		require.NoError(t, err)
		require.False(t, duplicate.Applied)
		require.Equal(t, 0.3, fixture.grantRemaining(t, grantID))
		_, err = fixture.repo.CaptureBatchImageBalance(fixture.ctx, fixture.captureCommand(0.4, "different"))
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		_, err = fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	})

	t.Run("full mixed capture never makes temporary credit negative", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 0.8, time.Now().Add(time.Hour))
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)

		result, err := fixture.repo.CaptureBatchImageBalance(fixture.ctx, fixture.captureCommand(1, "manifest"))
		require.NoError(t, err)
		require.Equal(t, 0.8, result.TemporaryCapturedAmount)
		require.Equal(t, 0.2, result.PermanentCapturedAmount)
		require.Zero(t, fixture.grantRemaining(t, grantID))
		consumptionCount, consumed := fixture.temporaryConsumption(t, service.BatchImageCaptureRequestID(fixture.batchID))
		require.Equal(t, 1, consumptionCount)
		require.Equal(t, 0.8, consumed)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 9.8, balance)
		require.Zero(t, frozen)
	})

	t.Run("expired unused temporary credit is not revived", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 1, time.Now().Add(time.Hour))
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(fixture.ctx,
			"UPDATE temporary_credit_grants SET expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1", grantID,
		)
		require.NoError(t, err)

		result, err := fixture.repo.CaptureBatchImageBalance(fixture.ctx, fixture.captureCommand(0.4, "manifest"))
		require.NoError(t, err)
		require.Equal(t, 0.4, result.TemporaryCapturedAmount)
		require.Zero(t, result.TemporaryRefundedAmount)
		require.Equal(t, 0.6, result.TemporaryExpiredAmount)
		require.Zero(t, fixture.grantRemaining(t, grantID))
		consumptionCount, consumed := fixture.temporaryConsumption(t, service.BatchImageCaptureRequestID(fixture.batchID))
		require.Equal(t, 1, consumptionCount)
		require.Equal(t, 0.4, consumed)
		fixture.requireHold(t, "captured", 0.4, 0.4, 0, 0.6)
	})

	t.Run("release restores valid temporary and permanent reservations", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 0.6, time.Now().Add(time.Hour))
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)

		result, err := fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
		require.NoError(t, err)
		require.True(t, result.Applied)
		require.Equal(t, 0.6, result.TemporaryRefundedAmount)
		require.Equal(t, 0.6, fixture.grantRemaining(t, grantID))
		consumptionCount, consumed := fixture.temporaryConsumption(t, service.BatchImageReleaseRequestID(fixture.batchID))
		require.Zero(t, consumptionCount)
		require.Zero(t, consumed)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)
		fixture.requireHold(t, "released", 0, 0, 0, 0)

		duplicate, err := fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
		require.NoError(t, err)
		require.False(t, duplicate.Applied)
		require.Equal(t, 0.6, fixture.grantRemaining(t, grantID))

		_, err = fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("different-request"))
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		require.Equal(t, 0.6, fixture.grantRemaining(t, grantID))
	})

	t.Run("release does not revive expired temporary credit", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		grantID := fixture.createGrant(t, 1, time.Now().Add(time.Hour))
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(fixture.ctx,
			"UPDATE temporary_credit_grants SET expires_at = clock_timestamp() - INTERVAL '1 second' WHERE id = $1", grantID,
		)
		require.NoError(t, err)

		result, err := fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
		require.NoError(t, err)
		require.Zero(t, result.TemporaryRefundedAmount)
		require.Equal(t, 1.0, result.TemporaryExpiredAmount)
		require.Zero(t, fixture.grantRemaining(t, grantID))
		fixture.requireHold(t, "released", 0, 0, 0, 1)
	})

	t.Run("captured hold rejects release", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		_, err = fixture.repo.CaptureBatchImageBalance(fixture.ctx, fixture.captureCommand(0.5, "manifest"))
		require.NoError(t, err)

		_, err = fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 9.5, balance)
		require.Zero(t, frozen)
		fixture.requireHold(t, "captured", 0.5, 0, 0.5, 0)
	})

	t.Run("new released ledger rejects a different fingerprint", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)
		_, err = fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
		require.NoError(t, err)

		_, err = fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("different-request"))
		require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)
		fixture.requireHold(t, "released", 0, 0, 0, 0)
	})
}

func TestUsageBillingRepositoryBatchImageTerminalConcurrencyAndLegacy(t *testing.T) {
	t.Run("capture and release race has one terminal winner", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		_, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, fixture.reserveCommand("request"))
		require.NoError(t, err)

		errs := make(chan error, 2)
		go func() {
			_, captureErr := fixture.repo.CaptureBatchImageBalance(fixture.ctx, fixture.captureCommand(1, "manifest"))
			errs <- captureErr
		}()
		go func() {
			_, releaseErr := fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
			errs <- releaseErr
		}()
		first, second := <-errs, <-errs
		successes := 0
		conflicts := 0
		for _, terminalErr := range []error{first, second} {
			if terminalErr == nil {
				successes++
			} else if errors.Is(terminalErr, service.ErrUsageBillingRequestConflict) {
				conflicts++
			}
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 1, conflicts)
		_, frozen := fixture.balance(t)
		require.Zero(t, frozen)
	})

	t.Run("legacy reserved hold materializes as permanent and releases once", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		_, err := integrationDB.ExecContext(fixture.ctx,
			"UPDATE users SET balance = 9, frozen_balance = 1 WHERE id = $1", fixture.userID,
		)
		require.NoError(t, err)
		reserve := fixture.reserveCommand("request")
		reserve.RequestFingerprint = "legacy-reserve-" + uuid.NewString()
		fixture.insertDedup(t, reserve.RequestID, reserve.RequestFingerprint)

		result, err := fixture.repo.ReserveBatchImageBalance(fixture.ctx, reserve)
		require.NoError(t, err)
		require.False(t, result.Applied)
		fixture.requireHold(t, "reserved", 0, 0, 0, 0)
		var temporaryReserved, permanentReserved float64
		require.NoError(t, integrationDB.QueryRowContext(fixture.ctx, `
SELECT temporary_reserved_amount, permanent_reserved_amount
FROM batch_image_credit_holds WHERE batch_id = $1`, fixture.batchID).Scan(&temporaryReserved, &permanentReserved))
		require.Zero(t, temporaryReserved)
		require.Equal(t, 1.0, permanentReserved)

		_, err = fixture.repo.ReleaseBatchImageBalance(fixture.ctx, fixture.releaseCommand("request"))
		require.NoError(t, err)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)
	})

	t.Run("legacy terminal dedup rebuilds ledger without moving funds twice", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		_, err := integrationDB.ExecContext(fixture.ctx,
			"UPDATE users SET balance = 9.4, frozen_balance = 0 WHERE id = $1", fixture.userID,
		)
		require.NoError(t, err)
		_, err = integrationDB.ExecContext(fixture.ctx,
			"UPDATE batch_image_jobs SET actual_cost = 0.6 WHERE batch_id = $1", fixture.batchID,
		)
		require.NoError(t, err)
		reserve := fixture.reserveCommand("request")
		reserve.RequestFingerprint = "legacy-reserve-" + uuid.NewString()
		capture := fixture.captureCommand(0.6, "manifest")
		capture.RequestFingerprint = "legacy-capture-" + uuid.NewString()
		fixture.insertDedup(t, reserve.RequestID, reserve.RequestFingerprint)
		fixture.insertDedup(t, capture.RequestID, capture.RequestFingerprint)

		result, err := fixture.repo.CaptureBatchImageBalance(fixture.ctx, capture)
		require.NoError(t, err)
		require.False(t, result.Applied)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 9.4, balance)
		require.Zero(t, frozen)
		fixture.requireHold(t, "captured", 0.6, 0, 0.6, 0)
	})

	t.Run("legacy release dedup replays idempotently", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		reserve := fixture.reserveCommand("request")
		release := fixture.releaseCommand("request")
		reserve.RequestFingerprint = "legacy-reserve-" + uuid.NewString()
		release.RequestFingerprint = "legacy-release-" + uuid.NewString()
		fixture.insertDedup(t, reserve.RequestID, reserve.RequestFingerprint)
		fixture.insertDedup(t, release.RequestID, release.RequestFingerprint)

		result, err := fixture.repo.ReleaseBatchImageBalance(fixture.ctx, release)
		require.NoError(t, err)
		require.False(t, result.Applied)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)
		fixture.requireHold(t, "released", 0, 0, 0, 0)
	})

	t.Run("legacy release fingerprint mismatch remains an idempotent no-op", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		reserve := fixture.reserveCommand("request")
		historicalRelease := fixture.releaseCommand("request")
		currentRelease := fixture.releaseCommand("different-request")
		reserve.RequestFingerprint = "legacy-reserve-" + uuid.NewString()
		historicalRelease.RequestFingerprint = "legacy-release-" + uuid.NewString()
		currentRelease.RequestFingerprint = "different-release-" + uuid.NewString()
		fixture.insertDedup(t, reserve.RequestID, reserve.RequestFingerprint)
		fixture.insertDedup(t, historicalRelease.RequestID, historicalRelease.RequestFingerprint)

		result, err := fixture.repo.ReleaseBatchImageBalance(fixture.ctx, currentRelease)
		require.NoError(t, err)
		require.False(t, result.Applied)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 10.0, balance)
		require.Zero(t, frozen)
		fixture.requireHold(t, "released", 0, 0, 0, 0)
	})

	t.Run("ambiguous legacy terminal evidence fails closed", func(t *testing.T) {
		fixture := newBatchImageHoldIntegrationFixture(t, 10, 1)
		_, err := integrationDB.ExecContext(fixture.ctx,
			"UPDATE users SET balance = 9, frozen_balance = 1 WHERE id = $1", fixture.userID,
		)
		require.NoError(t, err)
		reserve := fixture.reserveCommand("request")
		capture := fixture.captureCommand(1, "manifest")
		release := fixture.releaseCommand("request")
		reserve.RequestFingerprint = "legacy-reserve-" + uuid.NewString()
		capture.RequestFingerprint = "legacy-capture-" + uuid.NewString()
		release.RequestFingerprint = "legacy-release-" + uuid.NewString()
		fixture.insertDedup(t, reserve.RequestID, reserve.RequestFingerprint)
		fixture.insertDedup(t, capture.RequestID, capture.RequestFingerprint)
		fixture.insertDedup(t, release.RequestID, release.RequestFingerprint)

		_, err = fixture.repo.ReleaseBatchImageBalance(fixture.ctx, release)
		require.Error(t, err)
		balance, frozen := fixture.balance(t)
		require.Equal(t, 9.0, balance)
		require.Equal(t, 1.0, frozen)
		fixture.requireLedgerCounts(t, 0, 0, 1)
	})
}

func newBatchImageHoldIntegrationFixture(t *testing.T, balance, hold float64) *batchImageHoldIntegrationFixture {
	t.Helper()
	ctx := context.Background()
	client := testEntClient(t)
	user := mustCreateUser(t, client, &service.User{
		Email:        fmt.Sprintf("batch-hold-%s@example.com", uuid.NewString()),
		PasswordHash: "hash",
		Balance:      balance,
	})
	group := mustCreateGroup(t, client, &service.Group{Name: "batch-hold-" + uuid.NewString()})
	apiKey := mustCreateApiKey(t, client, &service.APIKey{
		UserID:  user.ID,
		GroupID: &group.ID,
		Key:     "sk-batch-hold-" + uuid.NewString(),
		Name:    "batch-hold",
	})
	batchID := "imgbatch_" + uuid.NewString()
	_, err := integrationDB.ExecContext(ctx, `
INSERT INTO batch_image_jobs (
    batch_id, user_id, api_key_id, provider, model, status,
    item_count, estimated_cost, hold_amount, currency
)
VALUES ($1, $2, $3, 'vertex', 'imagen', 'created', 1, $4, $4, 'USD')`, batchID, user.ID, apiKey.ID, hold)
	require.NoError(t, err)

	fixture := &batchImageHoldIntegrationFixture{
		ctx:      ctx,
		repo:     NewUsageBillingRepository(client, integrationDB),
		userID:   user.ID,
		apiKeyID: apiKey.ID,
		groupID:  group.ID,
		batchID:  batchID,
		hold:     hold,
	}
	t.Cleanup(func() {
		cleanupCtx := context.Background()
		statements := []struct {
			query string
			args  []any
		}{
			{"DELETE FROM temporary_credit_consumptions WHERE grant_id IN (SELECT id FROM temporary_credit_grants WHERE user_id = $1)", []any{user.ID}},
			{"DELETE FROM batch_image_credit_hold_allocations WHERE batch_id = $1", []any{batchID}},
			{"DELETE FROM batch_image_credit_holds WHERE batch_id = $1", []any{batchID}},
			{"DELETE FROM usage_billing_dedup WHERE api_key_id = $1 AND request_id IN ($2, $3, $4)", []any{apiKey.ID, service.BatchImageHoldRequestID(batchID), service.BatchImageCaptureRequestID(batchID), service.BatchImageReleaseRequestID(batchID)}},
			{"DELETE FROM usage_billing_dedup_archive WHERE api_key_id = $1 AND request_id IN ($2, $3, $4)", []any{apiKey.ID, service.BatchImageHoldRequestID(batchID), service.BatchImageCaptureRequestID(batchID), service.BatchImageReleaseRequestID(batchID)}},
			{"DELETE FROM temporary_credit_grants WHERE user_id = $1", []any{user.ID}},
			{"DELETE FROM batch_image_jobs WHERE batch_id = $1", []any{batchID}},
			{"DELETE FROM api_keys WHERE id = $1", []any{apiKey.ID}},
			{"DELETE FROM groups WHERE id = $1", []any{group.ID}},
			{"DELETE FROM users WHERE id = $1", []any{user.ID}},
		}
		for _, statement := range statements {
			if _, cleanupErr := integrationDB.ExecContext(cleanupCtx, statement.query, statement.args...); cleanupErr != nil {
				t.Errorf("cleanup batch hold fixture: %v", cleanupErr)
			}
		}
	})
	return fixture
}

func (f *batchImageHoldIntegrationFixture) createGrant(t *testing.T, amount float64, expiresAt time.Time) int64 {
	t.Helper()
	var grantID int64
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, `
INSERT INTO temporary_credit_grants (
    user_id, source, amount, remaining_amount, expires_at, notes, granted_by, created_at, updated_at
)
VALUES ($1, 'admin_grant', $2, $2, $3, '', $1, clock_timestamp(), clock_timestamp())
RETURNING id`, f.userID, amount, expiresAt).Scan(&grantID))
	return grantID
}

func installBatchImageGrantUpdateDelay(t *testing.T, grantID int64, delaySeconds float64) {
	t.Helper()
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	functionName := "test_batch_hold_delay_fn_" + suffix
	triggerName := "test_batch_hold_delay_trg_" + suffix
	_, err := integrationDB.ExecContext(context.Background(), fmt.Sprintf(`
CREATE FUNCTION %s() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    PERFORM pg_sleep(%.3f);
    RETURN NEW;
END;
$$`, functionName, delaySeconds))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON temporary_credit_grants", triggerName))
		_, _ = integrationDB.ExecContext(context.Background(), fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", functionName))
	})
	_, err = integrationDB.ExecContext(context.Background(), fmt.Sprintf(`
CREATE TRIGGER %s
BEFORE UPDATE OF remaining_amount ON temporary_credit_grants
FOR EACH ROW
WHEN (OLD.id = %d)
EXECUTE FUNCTION %s()`, triggerName, grantID, functionName))
	require.NoError(t, err)
}

func (f *batchImageHoldIntegrationFixture) reserveCommand(payload string) *service.BatchImageBalanceHoldCommand {
	return &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageHoldRequestID(f.batchID), APIKeyID: f.apiKeyID, UserID: f.userID, BatchID: f.batchID, HoldAmount: f.hold, RequestPayloadHash: payload}
}

func (f *batchImageHoldIntegrationFixture) captureCommand(actual float64, payload string) *service.BatchImageBalanceHoldCommand {
	return &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageCaptureRequestID(f.batchID), APIKeyID: f.apiKeyID, UserID: f.userID, BatchID: f.batchID, HoldAmount: f.hold, ActualAmount: actual, RequestPayloadHash: payload}
}

func (f *batchImageHoldIntegrationFixture) releaseCommand(payload string) *service.BatchImageBalanceHoldCommand {
	return &service.BatchImageBalanceHoldCommand{RequestID: service.BatchImageReleaseRequestID(f.batchID), APIKeyID: f.apiKeyID, UserID: f.userID, BatchID: f.batchID, HoldAmount: f.hold, RequestPayloadHash: payload}
}

func (f *batchImageHoldIntegrationFixture) insertDedup(t *testing.T, requestID, fingerprint string) {
	t.Helper()
	_, err := integrationDB.ExecContext(f.ctx, `
INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
VALUES ($1, $2, $3)`, requestID, f.apiKeyID, fingerprint)
	require.NoError(t, err)
}

func (f *batchImageHoldIntegrationFixture) balance(t *testing.T) (float64, float64) {
	t.Helper()
	var balance, frozen float64
	require.NoError(t, integrationDB.QueryRowContext(f.ctx,
		"SELECT balance, frozen_balance FROM users WHERE id = $1", f.userID,
	).Scan(&balance, &frozen))
	return balance, frozen
}

func (f *batchImageHoldIntegrationFixture) grantRemaining(t *testing.T, grantID int64) float64 {
	t.Helper()
	var remaining float64
	require.NoError(t, integrationDB.QueryRowContext(f.ctx,
		"SELECT remaining_amount FROM temporary_credit_grants WHERE id = $1", grantID,
	).Scan(&remaining))
	return remaining
}

func (f *batchImageHoldIntegrationFixture) allocationAmounts(t *testing.T) []float64 {
	t.Helper()
	rows, err := integrationDB.QueryContext(f.ctx, `
SELECT reserved_amount
FROM batch_image_credit_hold_allocations
WHERE batch_id = $1
ORDER BY grant_expires_at, grant_id`, f.batchID)
	require.NoError(t, err)
	defer rows.Close()
	amounts := make([]float64, 0)
	for rows.Next() {
		var amount float64
		require.NoError(t, rows.Scan(&amount))
		amounts = append(amounts, amount)
	}
	require.NoError(t, rows.Err())
	return amounts
}

func (f *batchImageHoldIntegrationFixture) temporaryConsumption(t *testing.T, requestID string) (int, float64) {
	t.Helper()
	var count int
	var amount float64
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, `
SELECT COUNT(*), COALESCE(SUM(amount), 0)
FROM temporary_credit_consumptions
WHERE request_id = $1`, requestID).Scan(&count, &amount))
	return count, amount
}

func (f *batchImageHoldIntegrationFixture) requireLedgerCounts(t *testing.T, holds, allocations, dedup int) {
	t.Helper()
	var holdCount, allocationCount, dedupCount int
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM batch_image_credit_holds WHERE batch_id = $1", f.batchID).Scan(&holdCount))
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM batch_image_credit_hold_allocations WHERE batch_id = $1", f.batchID).Scan(&allocationCount))
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM usage_billing_dedup WHERE api_key_id = $1 AND request_id = $2", f.apiKeyID, service.BatchImageHoldRequestID(f.batchID)).Scan(&dedupCount))
	require.Equal(t, holds, holdCount)
	require.Equal(t, allocations, allocationCount)
	require.Equal(t, dedup, dedupCount)
}

func (f *batchImageHoldIntegrationFixture) requireHold(t *testing.T, status string, captured, temporaryCaptured, permanentCaptured, expired float64) {
	t.Helper()
	var gotStatus string
	var gotCaptured, gotTemporary, gotPermanent, gotExpired float64
	require.NoError(t, integrationDB.QueryRowContext(f.ctx, `
SELECT status, captured_amount, temporary_captured_amount, permanent_captured_amount, expired_unrestored_amount
FROM batch_image_credit_holds
WHERE batch_id = $1`, f.batchID).Scan(&gotStatus, &gotCaptured, &gotTemporary, &gotPermanent, &gotExpired))
	require.Equal(t, status, gotStatus)
	require.Equal(t, captured, gotCaptured)
	require.Equal(t, temporaryCaptured, gotTemporary)
	require.Equal(t, permanentCaptured, gotPermanent)
	require.Equal(t, expired, gotExpired)
}
