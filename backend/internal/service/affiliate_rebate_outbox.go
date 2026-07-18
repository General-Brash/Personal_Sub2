package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
)

const (
	AffiliateRebateSourceRedeem        = "redeem"
	AffiliateRebateSourceAdminRecharge = "admin_recharge"

	affiliateRebateWorkerPollInterval = time.Second
	affiliateRebateProcessingLease    = 5 * time.Minute
	affiliateRebateRetryBase          = 5 * time.Second
	affiliateRebateRetryMax           = 10 * time.Minute
)

type AffiliateRebateJobInput struct {
	InviteeUserID      int64
	SourceRedeemCodeID int64
	SourceKind         string
	BaseAmount         float64
}

type AffiliateAccrualResult struct {
	Amount    float64
	Applied   bool
	Duplicate bool
}

// AffiliateRebateOutboxRepository is the durable write boundary shared by
// redeem, admin balance adjustment, and the asynchronous rebate worker.
type AffiliateRebateOutboxRepository interface {
	EnqueueAffiliateRebateJob(ctx context.Context, input AffiliateRebateJobInput) error
	AccrueQuotaCapped(
		ctx context.Context,
		inviterID int64,
		inviteeUserID int64,
		requestedAmount float64,
		perInviteeCap float64,
		freezeHours int,
		sourceOrderID *int64,
		sourceRedeemCodeID *int64,
	) (AffiliateAccrualResult, error)
}

type affiliateRebateRuntimeConfig struct {
	Enabled       bool
	RatePercent   float64
	FreezeHours   int
	DurationDays  int
	PerInviteeCap float64
}

type affiliateRebateJob struct {
	ID                 int64
	InviteeUserID      int64
	SourceRedeemCodeID int64
	SourceKind         string
	BaseAmount         float64
	Attempts           int
}

func (s *AffiliateService) EnqueueAffiliateRebateJob(ctx context.Context, input AffiliateRebateJobInput) error {
	if s == nil || s.repo == nil {
		return errors.New("affiliate service is not configured")
	}
	if input.InviteeUserID <= 0 || input.SourceRedeemCodeID <= 0 {
		return errors.New("invalid affiliate rebate job source")
	}
	if input.SourceKind != AffiliateRebateSourceRedeem && input.SourceKind != AffiliateRebateSourceAdminRecharge {
		return errors.New("invalid affiliate rebate job kind")
	}
	input.BaseAmount = roundTo(input.BaseAmount, 8)
	if input.BaseAmount <= 0 || math.IsNaN(input.BaseAmount) || math.IsInf(input.BaseAmount, 0) {
		return errors.New("invalid affiliate rebate base amount")
	}
	repo, ok := s.repo.(AffiliateRebateOutboxRepository)
	if !ok || repo == nil {
		return errors.New("affiliate rebate outbox repository is not configured")
	}
	return repo.EnqueueAffiliateRebateJob(ctx, input)
}

func (s *AffiliateService) accrueQueuedAffiliateRebate(
	ctx context.Context,
	job affiliateRebateJob,
	cfg affiliateRebateRuntimeConfig,
) (AffiliateAccrualResult, string, error) {
	if s == nil || s.repo == nil {
		return AffiliateAccrualResult{}, "", errors.New("affiliate service is not configured")
	}

	invitee, err := s.repo.EnsureUserAffiliate(ctx, job.InviteeUserID)
	if err != nil {
		return AffiliateAccrualResult{}, "", err
	}
	if invitee.InviterID == nil || *invitee.InviterID <= 0 {
		return AffiliateAccrualResult{}, "no inviter bound", nil
	}
	if cfg.DurationDays > 0 && time.Now().After(invitee.CreatedAt.AddDate(0, 0, cfg.DurationDays)) {
		return AffiliateAccrualResult{}, "rebate duration expired", nil
	}

	inviter, err := s.repo.EnsureUserAffiliate(ctx, *invitee.InviterID)
	if err != nil {
		return AffiliateAccrualResult{}, "", err
	}
	rate := cfg.RatePercent
	if inviter.AffRebateRatePercent != nil {
		rate = *inviter.AffRebateRatePercent
		if math.IsNaN(rate) || math.IsInf(rate, 0) || rate < AffiliateRebateRateMin || rate > AffiliateRebateRateMax {
			return AffiliateAccrualResult{}, "", fmt.Errorf("invalid inviter affiliate rebate rate: %v", rate)
		}
	}

	requested := roundTo(job.BaseAmount*(rate/100), 8)
	if requested <= 0 {
		return AffiliateAccrualResult{}, "rebate amount is zero", nil
	}
	repo, ok := s.repo.(AffiliateRebateOutboxRepository)
	if !ok || repo == nil {
		return AffiliateAccrualResult{}, "", errors.New("affiliate rebate outbox repository is not configured")
	}
	sourceRedeemCodeID := job.SourceRedeemCodeID
	result, err := repo.AccrueQuotaCapped(
		ctx,
		*invitee.InviterID,
		job.InviteeUserID,
		requested,
		cfg.PerInviteeCap,
		cfg.FreezeHours,
		nil,
		&sourceRedeemCodeID,
	)
	if err != nil {
		return AffiliateAccrualResult{}, "", err
	}
	if result.Amount <= 0 && !result.Duplicate {
		return result, "per-invitee cap exhausted", nil
	}
	return result, "", nil
}

func (s *SettingService) loadAffiliateRebateRuntimeConfig(ctx context.Context, sourceKind string) (affiliateRebateRuntimeConfig, error) {
	if s == nil || s.settingRepo == nil {
		return affiliateRebateRuntimeConfig{}, errors.New("affiliate settings repository is not configured")
	}
	keys := []string{
		SettingKeyAffiliateEnabled,
		SettingKeyAffiliateAdminRechargeEnabled,
		SettingKeyAffiliateRebateRate,
		SettingKeyAffiliateRebateFreezeHours,
		SettingKeyAffiliateRebateDurationDays,
		SettingKeyAffiliateRebatePerInviteeCap,
	}
	values, err := s.settingRepo.GetMultiple(ctx, keys)
	if err != nil {
		return affiliateRebateRuntimeConfig{}, fmt.Errorf("load affiliate settings: %w", err)
	}

	enabled, err := parseAffiliateBoolSetting(values, SettingKeyAffiliateEnabled, AffiliateEnabledDefault)
	if err != nil {
		return affiliateRebateRuntimeConfig{}, err
	}
	config := affiliateRebateRuntimeConfig{Enabled: enabled}
	if !enabled {
		return config, nil
	}
	if sourceKind == AffiliateRebateSourceAdminRecharge {
		adminEnabled, err := parseAffiliateBoolSetting(values, SettingKeyAffiliateAdminRechargeEnabled, AdminRechargeRebateEnabledDefault)
		if err != nil {
			return affiliateRebateRuntimeConfig{}, err
		}
		if !adminEnabled {
			config.Enabled = false
			return config, nil
		}
	}

	config.RatePercent, err = parseAffiliateFloatSetting(
		values,
		SettingKeyAffiliateRebateRate,
		AffiliateRebateRateDefault,
		AffiliateRebateRateMin,
		AffiliateRebateRateMax,
	)
	if err != nil {
		return affiliateRebateRuntimeConfig{}, err
	}
	config.FreezeHours, err = parseAffiliateIntSetting(
		values,
		SettingKeyAffiliateRebateFreezeHours,
		AffiliateRebateFreezeHoursDefault,
		0,
		AffiliateRebateFreezeHoursMax,
	)
	if err != nil {
		return affiliateRebateRuntimeConfig{}, err
	}
	config.DurationDays, err = parseAffiliateIntSetting(
		values,
		SettingKeyAffiliateRebateDurationDays,
		AffiliateRebateDurationDaysDefault,
		0,
		AffiliateRebateDurationDaysMax,
	)
	if err != nil {
		return affiliateRebateRuntimeConfig{}, err
	}
	config.PerInviteeCap, err = parseAffiliateFloatSetting(
		values,
		SettingKeyAffiliateRebatePerInviteeCap,
		AffiliateRebatePerInviteeCapDefault,
		0,
		math.MaxFloat64,
	)
	if err != nil {
		return affiliateRebateRuntimeConfig{}, err
	}
	return config, nil
}

func parseAffiliateBoolSetting(values map[string]string, key string, fallback bool) (bool, error) {
	raw, ok := values[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		return false, fmt.Errorf("parse affiliate setting %s: %w", key, err)
	}
	return value, nil
}

func parseAffiliateFloatSetting(values map[string]string, key string, fallback, minValue, maxValue float64) (float64, error) {
	raw, ok := values[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < minValue || value > maxValue {
		if err == nil {
			err = errors.New("value is outside the allowed range")
		}
		return 0, fmt.Errorf("parse affiliate setting %s: %w", key, err)
	}
	return value, nil
}

func parseAffiliateIntSetting(values map[string]string, key string, fallback, minValue, maxValue int) (int, error) {
	raw, ok := values[key]
	if !ok || strings.TrimSpace(raw) == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minValue || value > maxValue {
		if err == nil {
			err = errors.New("value is outside the allowed range")
		}
		return 0, fmt.Errorf("parse affiliate setting %s: %w", key, err)
	}
	return value, nil
}

// AffiliateRebateWorker drains the durable outbox. Each job row remains locked
// for the entire business transaction; ledger creation and terminal job state
// therefore commit together.
type AffiliateRebateWorker struct {
	client           *dbent.Client
	affiliateService *AffiliateService
	settingService   *SettingService

	mu      sync.Mutex
	cancel  context.CancelFunc
	wait    sync.WaitGroup
	started bool
}

func NewAffiliateRebateWorker(client *dbent.Client, affiliateService *AffiliateService, settingService *SettingService) *AffiliateRebateWorker {
	return &AffiliateRebateWorker{
		client:           client,
		affiliateService: affiliateService,
		settingService:   settingService,
	}
}

func (w *AffiliateRebateWorker) Start() {
	if w == nil || w.client == nil || w.affiliateService == nil || w.settingService == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.started {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	w.started = true
	w.wait.Add(1)
	go w.run(ctx)
}

func (w *AffiliateRebateWorker) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if !w.started {
		w.mu.Unlock()
		return
	}
	cancel := w.cancel
	w.started = false
	w.cancel = nil
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	w.wait.Wait()
}

func (w *AffiliateRebateWorker) run(ctx context.Context) {
	defer w.wait.Done()
	ticker := time.NewTicker(affiliateRebateWorkerPollInterval)
	defer ticker.Stop()

	for {
		w.drain(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *AffiliateRebateWorker) drain(ctx context.Context) {
	for i := 0; i < 32; i++ {
		processed, err := w.ProcessOnce(ctx)
		if err != nil {
			slog.Error("affiliate_rebate.worker_failed", "error", err)
			return
		}
		if !processed {
			return
		}
	}
}

func (w *AffiliateRebateWorker) ProcessOnce(ctx context.Context) (bool, error) {
	if w == nil || w.client == nil || w.affiliateService == nil || w.settingService == nil {
		return false, errors.New("affiliate rebate worker is not configured")
	}
	tx, err := w.client.Tx(ctx)
	if err != nil {
		return false, fmt.Errorf("begin affiliate rebate job transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	client := tx.Client()

	job, err := claimAffiliateRebateJob(txCtx, client)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	cfg, processErr := w.settingService.loadAffiliateRebateRuntimeConfig(txCtx, job.SourceKind)
	if processErr == nil && !cfg.Enabled {
		processErr = markAffiliateRebateJobSkipped(txCtx, client, job.ID)
		if processErr == nil {
			if err := tx.Commit(); err != nil {
				return true, fmt.Errorf("commit skipped affiliate rebate job: %w", err)
			}
			return true, nil
		}
	}

	if processErr == nil {
		result, reason, accrueErr := w.affiliateService.accrueQueuedAffiliateRebate(txCtx, *job, cfg)
		processErr = accrueErr
		if processErr == nil {
			if reason != "" {
				processErr = markAffiliateRebateJobSkipped(txCtx, client, job.ID)
			} else if result.Applied || result.Duplicate {
				processErr = markAffiliateRebateJobSucceeded(txCtx, client, job.ID)
			} else {
				processErr = markAffiliateRebateJobSkipped(txCtx, client, job.ID)
			}
			if processErr == nil {
				if err := tx.Commit(); err != nil {
					return true, fmt.Errorf("commit affiliate rebate job: %w", err)
				}
				return true, nil
			}
		}
	}

	if err := markAffiliateRebateJobFailed(txCtx, client, job.ID, job.Attempts, processErr); err != nil {
		return true, errors.Join(processErr, err)
	}
	if err := tx.Commit(); err != nil {
		return true, errors.Join(processErr, fmt.Errorf("commit failed affiliate rebate job: %w", err))
	}
	return true, processErr
}

func claimAffiliateRebateJob(ctx context.Context, client *dbent.Client) (*affiliateRebateJob, error) {
	rows, err := client.QueryContext(ctx, `
SELECT id,
       invitee_user_id,
       source_redeem_code_id,
       source_kind,
       base_amount::double precision,
       attempts
FROM affiliate_rebate_jobs
WHERE (status IN ('pending', 'failed') AND next_retry_at <= NOW())
   OR (
        status = 'processing'
        AND (
            processing_started_at IS NULL
            OR processing_started_at <= NOW() - ($1 * INTERVAL '1 second')
        )
   )
ORDER BY id
LIMIT 1
FOR UPDATE SKIP LOCKED`, affiliateRebateProcessingLease.Seconds())
	if err != nil {
		return nil, fmt.Errorf("claim affiliate rebate job: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, sql.ErrNoRows
	}
	job := &affiliateRebateJob{}
	if err := rows.Scan(
		&job.ID,
		&job.InviteeUserID,
		&job.SourceRedeemCodeID,
		&job.SourceKind,
		&job.BaseAmount,
		&job.Attempts,
	); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	job.Attempts++
	result, err := client.ExecContext(ctx, `
UPDATE affiliate_rebate_jobs
SET status = 'processing',
    attempts = $2,
    processing_started_at = NOW(),
    updated_at = NOW()
WHERE id = $1`, job.ID, job.Attempts)
	if err != nil {
		return nil, fmt.Errorf("mark affiliate rebate job processing: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, sql.ErrNoRows
	}
	return job, nil
}

func markAffiliateRebateJobSucceeded(ctx context.Context, client *dbent.Client, jobID int64) error {
	_, err := client.ExecContext(ctx, `
UPDATE affiliate_rebate_jobs
SET status = 'succeeded',
    succeeded_at = NOW(),
    processing_started_at = NULL,
    last_error = NULL,
    last_error_at = NULL,
    failed_at = NULL,
    updated_at = NOW()
WHERE id = $1`, jobID)
	return err
}

func markAffiliateRebateJobSkipped(ctx context.Context, client *dbent.Client, jobID int64) error {
	_, err := client.ExecContext(ctx, `
UPDATE affiliate_rebate_jobs
SET status = 'skipped',
    skipped_at = NOW(),
    processing_started_at = NULL,
    last_error = NULL,
    last_error_at = NULL,
    failed_at = NULL,
    updated_at = NOW()
WHERE id = $1`, jobID)
	return err
}

func markAffiliateRebateJobFailed(ctx context.Context, client *dbent.Client, jobID int64, attempts int, cause error) error {
	message := "unknown affiliate rebate failure"
	if cause != nil {
		message = cause.Error()
	}
	nextRetry := time.Now().Add(affiliateRebateRetryDelay(attempts))
	_, err := client.ExecContext(ctx, `
UPDATE affiliate_rebate_jobs
SET status = 'failed',
    next_retry_at = $2,
    last_error = $3,
    last_error_at = NOW(),
    failed_at = NOW(),
    processing_started_at = NULL,
    updated_at = NOW()
WHERE id = $1`, jobID, nextRetry, message)
	return err
}

func affiliateRebateRetryDelay(attempts int) time.Duration {
	if attempts < 1 {
		attempts = 1
	}
	shift := attempts - 1
	if shift > 7 {
		shift = 7
	}
	delay := affiliateRebateRetryBase * time.Duration(1<<shift)
	if delay > affiliateRebateRetryMax {
		return affiliateRebateRetryMax
	}
	return delay
}
