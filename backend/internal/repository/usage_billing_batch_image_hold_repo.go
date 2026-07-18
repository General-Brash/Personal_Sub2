package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type batchImageHoldOperation uint8

const (
	batchImageHoldOperationReserve batchImageHoldOperation = iota + 1
	batchImageHoldOperationCapture
	batchImageHoldOperationRelease
)

type batchImageBillingJobSnapshot struct {
	UserID     int64
	APIKeyID   int64
	GroupID    *int64
	HoldAmount float64
	ActualCost *float64
	CreatedAt  time.Time
}

type batchImageCreditHoldRecord struct {
	ID                      int64
	BatchID                 string
	UserID                  int64
	APIKeyID                int64
	GroupID                 *int64
	Status                  string
	HoldAmount              float64
	TemporaryReservedAmount float64
	PermanentReservedAmount float64
	CapturedAmount          float64
	TemporaryCapturedAmount float64
	PermanentCapturedAmount float64
	ExpiredUnrestoredAmount float64
	ReserveFingerprint      string
	TerminalFingerprint     *string
}

type batchImageHoldDedupRecord struct {
	Found       bool
	Fingerprint string
}

type batchImageHoldDedupState struct {
	Reserve batchImageHoldDedupRecord
	Capture batchImageHoldDedupRecord
	Release batchImageHoldDedupRecord
}

type batchImageTemporaryCreditCandidate struct {
	GrantID   int64
	Available float64
	ExpiresAt time.Time
}

type batchImageTemporaryCreditAllocation struct {
	ID             int64
	GrantID        int64
	ReservedAmount float64
	ExpiresAt      time.Time
}

func (r *usageBillingRepository) applyBatchImageCreditHoldOperation(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.BatchImageBalanceHoldCommand,
	operation batchImageHoldOperation,
) (*service.BatchImageBalanceHoldResult, error) {
	if tx == nil || cmd == nil {
		return nil, errors.New("batch image credit hold transaction and command are required")
	}
	if cmd.UserID <= 0 || cmd.APIKeyID <= 0 || cmd.BatchID == "" {
		return nil, errors.New("batch image credit hold identity is invalid")
	}
	if cmd.RequestID != expectedBatchImageHoldRequestID(operation, cmd.BatchID) {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if operation != batchImageHoldOperationCapture && cmd.ActualAmount != 0 {
		return nil, service.ErrUsageBillingRequestConflict
	}
	if operation == batchImageHoldOperationCapture && cmd.ActualAmount > cmd.HoldAmount {
		return nil, service.ErrBatchImageSettlementCostExceedsHold
	}

	job, err := lockBatchImageBillingJob(ctx, tx, cmd.BatchID)
	if err != nil {
		return nil, err
	}
	if job.UserID != cmd.UserID || job.APIKeyID != cmd.APIKeyID || job.HoldAmount != cmd.HoldAmount {
		return nil, service.ErrUsageBillingRequestConflict
	}
	cmd.GroupID = cloneBatchImageGroupID(job.GroupID)

	dedup, err := loadBatchImageHoldDedupState(ctx, tx, cmd.BatchID, cmd.APIKeyID)
	if err != nil {
		return nil, err
	}
	if dedup.Capture.Found && dedup.Release.Found {
		return nil, errors.New("batch image credit hold has ambiguous terminal dedup records")
	}

	hold, err := lockBatchImageCreditHold(ctx, tx, cmd.BatchID)
	if err != nil {
		return nil, err
	}
	if hold != nil {
		if err := validateBatchImageCreditHoldIdentity(hold, cmd); err != nil {
			return nil, err
		}
		if err := validateBatchImageCreditHoldDedup(hold, dedup); err != nil {
			return nil, err
		}
	}

	now, err := batchImageDatabaseNow(ctx, tx)
	if err != nil {
		return nil, err
	}

	switch operation {
	case batchImageHoldOperationReserve:
		return r.reserveBatchImageCreditHold(ctx, tx, cmd, job, hold, dedup, now)
	case batchImageHoldOperationCapture, batchImageHoldOperationRelease:
		return r.settleBatchImageCreditHold(ctx, tx, cmd, job, hold, dedup, operation, now)
	default:
		return nil, errors.New("unknown batch image credit hold operation")
	}
}

func expectedBatchImageHoldRequestID(operation batchImageHoldOperation, batchID string) string {
	switch operation {
	case batchImageHoldOperationReserve:
		return service.BatchImageHoldRequestID(batchID)
	case batchImageHoldOperationCapture:
		return service.BatchImageCaptureRequestID(batchID)
	case batchImageHoldOperationRelease:
		return service.BatchImageReleaseRequestID(batchID)
	default:
		return ""
	}
}

func lockBatchImageBillingJob(ctx context.Context, tx *sql.Tx, batchID string) (*batchImageBillingJobSnapshot, error) {
	var (
		job          batchImageBillingJobSnapshot
		apiKeyID     sql.NullInt64
		actualCost   sql.NullFloat64
		apiKeyUserID int64
		groupID      sql.NullInt64
	)
	err := tx.QueryRowContext(ctx, `
SELECT user_id, api_key_id, COALESCE(hold_amount, estimated_cost, 0), actual_cost, created_at
FROM batch_image_jobs
WHERE batch_id = $1
FOR UPDATE`, batchID).Scan(&job.UserID, &apiKeyID, &job.HoldAmount, &actualCost, &job.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrBatchImageJobNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock batch image billing job: %w", err)
	}
	if !apiKeyID.Valid || apiKeyID.Int64 <= 0 {
		return nil, service.ErrBatchImageSettlementMissingAPIKeyID
	}
	job.APIKeyID = apiKeyID.Int64
	job.HoldAmount, err = normalizeBatchImageLedgerAmount(job.HoldAmount)
	if err != nil || job.HoldAmount < 0 {
		return nil, errors.New("batch image job hold amount is invalid")
	}
	if actualCost.Valid {
		normalized, normalizeErr := normalizeBatchImageLedgerAmount(actualCost.Float64)
		if normalizeErr != nil || normalized < 0 || normalized > job.HoldAmount {
			return nil, errors.New("batch image job actual cost is invalid")
		}
		job.ActualCost = &normalized
	}
	err = tx.QueryRowContext(ctx, `
SELECT user_id, group_id
FROM api_keys
WHERE id = $1`, job.APIKeyID).Scan(&apiKeyUserID, &groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load batch image API key snapshot: %w", err)
	}
	if apiKeyUserID != job.UserID {
		return nil, errors.New("batch image API key owner does not match job owner")
	}
	if groupID.Valid {
		job.GroupID = &groupID.Int64
	}
	return &job, nil
}

func lockBatchImageCreditHold(ctx context.Context, tx *sql.Tx, batchID string) (*batchImageCreditHoldRecord, error) {
	var (
		hold                batchImageCreditHoldRecord
		groupID             sql.NullInt64
		terminalFingerprint sql.NullString
	)
	err := tx.QueryRowContext(ctx, `
SELECT id, batch_id, user_id, api_key_id, group_id, status,
       hold_amount, temporary_reserved_amount, permanent_reserved_amount,
       captured_amount, temporary_captured_amount, permanent_captured_amount,
       expired_unrestored_amount, reserve_fingerprint, terminal_fingerprint
FROM batch_image_credit_holds
WHERE batch_id = $1
FOR UPDATE`, batchID).Scan(
		&hold.ID,
		&hold.BatchID,
		&hold.UserID,
		&hold.APIKeyID,
		&groupID,
		&hold.Status,
		&hold.HoldAmount,
		&hold.TemporaryReservedAmount,
		&hold.PermanentReservedAmount,
		&hold.CapturedAmount,
		&hold.TemporaryCapturedAmount,
		&hold.PermanentCapturedAmount,
		&hold.ExpiredUnrestoredAmount,
		&hold.ReserveFingerprint,
		&terminalFingerprint,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock batch image credit hold: %w", err)
	}
	if groupID.Valid {
		hold.GroupID = &groupID.Int64
	}
	if terminalFingerprint.Valid {
		value := strings.TrimSpace(terminalFingerprint.String)
		hold.TerminalFingerprint = &value
	}
	amounts := []*float64{
		&hold.HoldAmount,
		&hold.TemporaryReservedAmount,
		&hold.PermanentReservedAmount,
		&hold.CapturedAmount,
		&hold.TemporaryCapturedAmount,
		&hold.PermanentCapturedAmount,
		&hold.ExpiredUnrestoredAmount,
	}
	for _, amount := range amounts {
		*amount, err = normalizeBatchImageLedgerAmount(*amount)
		if err != nil || *amount < 0 {
			return nil, errors.New("batch image credit hold contains an invalid amount")
		}
	}
	return &hold, nil
}

func loadBatchImageHoldDedupState(ctx context.Context, tx *sql.Tx, batchID string, apiKeyID int64) (batchImageHoldDedupState, error) {
	reserve, err := loadUsageBillingDedupRecord(ctx, tx, service.BatchImageHoldRequestID(batchID), apiKeyID)
	if err != nil {
		return batchImageHoldDedupState{}, err
	}
	capture, err := loadUsageBillingDedupRecord(ctx, tx, service.BatchImageCaptureRequestID(batchID), apiKeyID)
	if err != nil {
		return batchImageHoldDedupState{}, err
	}
	release, err := loadUsageBillingDedupRecord(ctx, tx, service.BatchImageReleaseRequestID(batchID), apiKeyID)
	if err != nil {
		return batchImageHoldDedupState{}, err
	}
	return batchImageHoldDedupState{Reserve: reserve, Capture: capture, Release: release}, nil
}

func loadUsageBillingDedupRecord(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64) (batchImageHoldDedupRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT request_fingerprint
FROM (
    SELECT request_fingerprint
    FROM usage_billing_dedup
    WHERE request_id = $1 AND api_key_id = $2
    UNION ALL
    SELECT request_fingerprint
    FROM usage_billing_dedup_archive
    WHERE request_id = $1 AND api_key_id = $2
) records`, requestID, apiKeyID)
	if err != nil {
		return batchImageHoldDedupRecord{}, fmt.Errorf("load usage billing dedup record: %w", err)
	}
	defer rows.Close()

	var record batchImageHoldDedupRecord
	for rows.Next() {
		var fingerprint string
		if err := rows.Scan(&fingerprint); err != nil {
			return batchImageHoldDedupRecord{}, fmt.Errorf("scan usage billing dedup record: %w", err)
		}
		fingerprint = strings.TrimSpace(fingerprint)
		if fingerprint == "" {
			return batchImageHoldDedupRecord{}, errors.New("usage billing dedup fingerprint is empty")
		}
		if record.Found && record.Fingerprint != fingerprint {
			return batchImageHoldDedupRecord{}, service.ErrUsageBillingRequestConflict
		}
		record.Found = true
		record.Fingerprint = fingerprint
	}
	if err := rows.Err(); err != nil {
		return batchImageHoldDedupRecord{}, fmt.Errorf("iterate usage billing dedup record: %w", err)
	}
	return record, nil
}

func validateBatchImageCreditHoldIdentity(hold *batchImageCreditHoldRecord, cmd *service.BatchImageBalanceHoldCommand) error {
	if hold == nil || cmd == nil {
		return errors.New("batch image credit hold identity is missing")
	}
	if hold.BatchID != cmd.BatchID || hold.UserID != cmd.UserID || hold.APIKeyID != cmd.APIKeyID || hold.HoldAmount != cmd.HoldAmount {
		return service.ErrUsageBillingRequestConflict
	}
	reservedTotal, err := addBatchImageLedgerAmount(hold.TemporaryReservedAmount, hold.PermanentReservedAmount)
	if err != nil {
		return err
	}
	capturedTotal, err := addBatchImageLedgerAmount(hold.TemporaryCapturedAmount, hold.PermanentCapturedAmount)
	if err != nil {
		return err
	}
	if reservedTotal != hold.HoldAmount || capturedTotal != hold.CapturedAmount || hold.CapturedAmount > hold.HoldAmount {
		return errors.New("batch image credit hold conservation check failed")
	}
	return nil
}

func validateBatchImageCreditHoldDedup(hold *batchImageCreditHoldRecord, dedup batchImageHoldDedupState) error {
	if hold == nil || !dedup.Reserve.Found || dedup.Reserve.Fingerprint != strings.TrimSpace(hold.ReserveFingerprint) {
		return errors.New("batch image credit hold reserve dedup is inconsistent")
	}
	switch hold.Status {
	case "reserved":
		if dedup.Capture.Found || dedup.Release.Found || hold.TerminalFingerprint != nil {
			return errors.New("reserved batch image credit hold has terminal evidence")
		}
	case "captured":
		if !dedup.Capture.Found || dedup.Release.Found || hold.TerminalFingerprint == nil || dedup.Capture.Fingerprint != *hold.TerminalFingerprint {
			return errors.New("captured batch image credit hold dedup is inconsistent")
		}
	case "released":
		if !dedup.Release.Found || dedup.Capture.Found || hold.TerminalFingerprint == nil || dedup.Release.Fingerprint != *hold.TerminalFingerprint {
			return errors.New("released batch image credit hold dedup is inconsistent")
		}
	default:
		return errors.New("batch image credit hold status is invalid")
	}
	return nil
}

func batchImageDatabaseNow(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	var now time.Time
	if err := tx.QueryRowContext(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("load batch image billing timestamp: %w", err)
	}
	return now, nil
}

func normalizeBatchImageLedgerAmount(amount float64) (float64, error) {
	return service.NormalizeUsageBillingLedgerAmount(amount)
}

func formatBatchImageLedgerAmount(amount float64) string {
	return service.FormatUsageBillingLedgerAmount(amount)
}

func subtractBatchImageLedgerAmount(left, right float64) (float64, error) {
	return normalizeBatchImageLedgerAmount(left - right)
}

func addBatchImageLedgerAmount(left, right float64) (float64, error) {
	return normalizeBatchImageLedgerAmount(left + right)
}

func minBatchImageLedgerAmount(left, right float64) float64 {
	value, _ := normalizeBatchImageLedgerAmount(math.Min(left, right))
	return value
}

func cloneBatchImageGroupID(groupID *int64) *int64 {
	if groupID == nil {
		return nil
	}
	value := *groupID
	return &value
}

func nullableBatchImageGroupID(groupID *int64) any {
	if groupID == nil {
		return nil
	}
	return *groupID
}

func (r *usageBillingRepository) reserveBatchImageCreditHold(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.BatchImageBalanceHoldCommand,
	job *batchImageBillingJobSnapshot,
	hold *batchImageCreditHoldRecord,
	dedup batchImageHoldDedupState,
	now time.Time,
) (*service.BatchImageBalanceHoldResult, error) {
	if hold != nil {
		if !service.MatchesBatchImageBalanceHoldFingerprint(dedup.Reserve.Fingerprint, cmd) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		return batchImageCreditHoldResult(hold, false), nil
	}
	if dedup.Reserve.Found {
		if !service.MatchesBatchImageBalanceHoldFingerprint(dedup.Reserve.Fingerprint, cmd) {
			return nil, service.ErrUsageBillingRequestConflict
		}
		materialized, result, err := materializeLegacyBatchImageCreditHold(ctx, tx, cmd, job, dedup, batchImageHoldOperationReserve, now)
		if err != nil {
			return nil, err
		}
		_ = materialized
		return result, nil
	}
	if dedup.Capture.Found || dedup.Release.Found {
		return nil, errors.New("batch image terminal dedup exists without a reserve dedup")
	}
	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, errors.New("batch image reserve dedup appeared without a hold ledger")
	}
	return createBatchImageCreditHoldReservation(ctx, tx, cmd, job, now)
}

func createBatchImageCreditHoldReservation(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.BatchImageBalanceHoldCommand,
	job *batchImageBillingJobSnapshot,
	now time.Time,
) (*service.BatchImageBalanceHoldResult, error) {
	candidates, err := lockBatchImageTemporaryCreditCandidates(ctx, tx, cmd.UserID, cmd.HoldAmount)
	if err != nil {
		return nil, err
	}

	temporaryReserved := 0.0
	remaining := cmd.HoldAmount
	portions := make([]float64, len(candidates))
	for i, candidate := range candidates {
		if remaining <= 0 {
			break
		}
		portion := minBatchImageLedgerAmount(candidate.Available, remaining)
		if portion <= 0 {
			continue
		}
		portions[i] = portion
		temporaryReserved, err = addBatchImageLedgerAmount(temporaryReserved, portion)
		if err != nil {
			return nil, err
		}
		remaining, err = subtractBatchImageLedgerAmount(remaining, portion)
		if err != nil {
			return nil, err
		}
	}
	permanentReserved := remaining

	balance, frozen, err := lockBatchImageUserBalance(ctx, tx, cmd.UserID)
	if err != nil {
		return nil, err
	}
	if balance < permanentReserved {
		return nil, service.ErrBatchImageInsufficientBalance
	}

	var holdID int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO batch_image_credit_holds (
    batch_id, user_id, api_key_id, group_id, status,
    hold_amount, temporary_reserved_amount, permanent_reserved_amount,
    reserve_fingerprint, reserved_at, updated_at
)
VALUES ($1, $2, $3, $4, 'reserved', $5, $6, $7, $8, $9, $9)
RETURNING id`,
		cmd.BatchID,
		cmd.UserID,
		cmd.APIKeyID,
		nullableBatchImageGroupID(job.GroupID),
		formatBatchImageLedgerAmount(cmd.HoldAmount),
		formatBatchImageLedgerAmount(temporaryReserved),
		formatBatchImageLedgerAmount(permanentReserved),
		cmd.RequestFingerprint,
		now,
	).Scan(&holdID)
	if err != nil {
		return nil, fmt.Errorf("insert batch image credit hold: %w", err)
	}

	for i, candidate := range candidates {
		portion := portions[i]
		if portion <= 0 {
			continue
		}
		res, updateErr := tx.ExecContext(ctx, `
UPDATE temporary_credit_grants
SET remaining_amount = remaining_amount - $1,
    updated_at = $3
WHERE id = $2
	AND remaining_amount >= $1`, formatBatchImageLedgerAmount(portion), candidate.GrantID, now)
		if updateErr != nil {
			return nil, fmt.Errorf("reserve temporary credit grant %d: %w", candidate.GrantID, updateErr)
		}
		affected, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if affected != 1 {
			return nil, fmt.Errorf("temporary credit grant %d changed during reservation", candidate.GrantID)
		}
		_, insertErr := tx.ExecContext(ctx, `
INSERT INTO batch_image_credit_hold_allocations (
    hold_id, batch_id, grant_id, grant_expires_at, reserved_amount, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $6)`,
			holdID,
			cmd.BatchID,
			candidate.GrantID,
			candidate.ExpiresAt,
			formatBatchImageLedgerAmount(portion),
			now,
		)
		if insertErr != nil {
			return nil, fmt.Errorf("insert batch image temporary credit allocation: %w", insertErr)
		}
	}

	if permanentReserved > 0 {
		err = tx.QueryRowContext(ctx, `
UPDATE users
SET balance = balance - $1,
    frozen_balance = COALESCE(frozen_balance, 0) + $1,
    updated_at = $3
WHERE id = $2
  AND deleted_at IS NULL
  AND balance >= $1
RETURNING balance, frozen_balance`, formatBatchImageLedgerAmount(permanentReserved), cmd.UserID, now).Scan(&balance, &frozen)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, service.ErrBatchImageInsufficientBalance
		}
		if err != nil {
			return nil, fmt.Errorf("reserve permanent batch image balance: %w", err)
		}
		balance, err = normalizeBatchImageLedgerAmount(balance)
		if err != nil {
			return nil, err
		}
		frozen, err = normalizeBatchImageLedgerAmount(frozen)
		if err != nil {
			return nil, err
		}
	}

	return &service.BatchImageBalanceHoldResult{
		Applied:                 true,
		NewBalance:              &balance,
		FrozenBalance:           &frozen,
		TemporaryReservedAmount: temporaryReserved,
		PermanentReservedAmount: permanentReserved,
	}, nil
}

func lockBatchImageTemporaryCreditCandidates(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	holdAmount float64,
) ([]batchImageTemporaryCreditCandidate, error) {
	if holdAmount <= 0 {
		return nil, nil
	}
	rows, err := tx.QueryContext(ctx, `
SELECT id, remaining_amount, expires_at
FROM temporary_credit_grants
WHERE user_id = $1
  AND remaining_amount > 0
  AND expires_at > clock_timestamp()
ORDER BY expires_at ASC, id ASC
FOR UPDATE`, userID)
	if err != nil {
		return nil, fmt.Errorf("lock batch image temporary credit candidates: %w", err)
	}
	defer rows.Close()

	candidates := make([]batchImageTemporaryCreditCandidate, 0)
	remaining := holdAmount
	for rows.Next() {
		var candidate batchImageTemporaryCreditCandidate
		if err := rows.Scan(&candidate.GrantID, &candidate.Available, &candidate.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan batch image temporary credit candidate: %w", err)
		}
		candidate.Available, err = normalizeBatchImageLedgerAmount(candidate.Available)
		if err != nil || candidate.Available <= 0 {
			return nil, fmt.Errorf("temporary credit grant %d is invalid for reservation", candidate.GrantID)
		}
		candidates = append(candidates, candidate)
		remaining, err = subtractBatchImageLedgerAmount(remaining, minBatchImageLedgerAmount(remaining, candidate.Available))
		if err != nil {
			return nil, err
		}
		if remaining <= 0 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch image temporary credit candidates: %w", err)
	}
	return candidates, nil
}

func lockBatchImageUserBalance(ctx context.Context, tx *sql.Tx, userID int64) (float64, float64, error) {
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
SELECT balance, COALESCE(frozen_balance, 0)
FROM users
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE`, userID).Scan(&balance, &frozen)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, service.ErrUserNotFound
	}
	if err != nil {
		return 0, 0, fmt.Errorf("lock batch image user balance: %w", err)
	}
	balance, err = normalizeBatchImageLedgerAmount(balance)
	if err != nil {
		return 0, 0, errors.New("batch image user balance is invalid")
	}
	frozen, err = normalizeBatchImageLedgerAmount(frozen)
	if err != nil || frozen < 0 {
		return 0, 0, errors.New("batch image user frozen balance is invalid")
	}
	return balance, frozen, nil
}

func (r *usageBillingRepository) settleBatchImageCreditHold(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.BatchImageBalanceHoldCommand,
	job *batchImageBillingJobSnapshot,
	hold *batchImageCreditHoldRecord,
	dedup batchImageHoldDedupState,
	operation batchImageHoldOperation,
	now time.Time,
) (*service.BatchImageBalanceHoldResult, error) {
	currentDedup, oppositeDedup := terminalBatchImageHoldDedup(operation, dedup)
	if hold == nil {
		if !dedup.Reserve.Found {
			return nil, errors.New("batch image terminal operation has no reserve ledger or dedup")
		}
		if currentDedup.Found {
			legacyReleaseReplay := operation == batchImageHoldOperationRelease && !oppositeDedup.Found
			if !legacyReleaseReplay && !service.MatchesBatchImageBalanceHoldFingerprint(currentDedup.Fingerprint, cmd) {
				return nil, service.ErrUsageBillingRequestConflict
			}
			if oppositeDedup.Found {
				return nil, errors.New("batch image credit hold has ambiguous terminal dedup records")
			}
			_, result, err := materializeLegacyBatchImageCreditHold(ctx, tx, cmd, job, dedup, operation, now)
			return result, err
		}
		if oppositeDedup.Found {
			return nil, service.ErrUsageBillingRequestConflict
		}
		materialized, _, err := materializeLegacyBatchImageCreditHold(ctx, tx, cmd, job, dedup, 0, now)
		if err != nil {
			return nil, err
		}
		hold = materialized
	}

	expectedStatus := terminalBatchImageHoldStatus(operation)
	if hold.Status != "reserved" {
		if hold.Status != expectedStatus {
			return nil, service.ErrUsageBillingRequestConflict
		}
		if !currentDedup.Found || !service.MatchesBatchImageBalanceHoldFingerprint(currentDedup.Fingerprint, cmd) || hold.TerminalFingerprint == nil || *hold.TerminalFingerprint != currentDedup.Fingerprint {
			return nil, service.ErrUsageBillingRequestConflict
		}
		return batchImageCreditHoldResult(hold, false), nil
	}
	if currentDedup.Found || oppositeDedup.Found {
		return nil, errors.New("reserved batch image credit hold has terminal dedup evidence")
	}

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return nil, errors.New("batch image terminal dedup appeared while hold remained reserved")
	}
	return applyBatchImageCreditHoldSettlement(ctx, tx, cmd, hold, operation, now)
}

func terminalBatchImageHoldDedup(operation batchImageHoldOperation, dedup batchImageHoldDedupState) (batchImageHoldDedupRecord, batchImageHoldDedupRecord) {
	if operation == batchImageHoldOperationCapture {
		return dedup.Capture, dedup.Release
	}
	return dedup.Release, dedup.Capture
}

func terminalBatchImageHoldStatus(operation batchImageHoldOperation) string {
	if operation == batchImageHoldOperationCapture {
		return "captured"
	}
	return "released"
}

func applyBatchImageCreditHoldSettlement(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.BatchImageBalanceHoldCommand,
	hold *batchImageCreditHoldRecord,
	operation batchImageHoldOperation,
	now time.Time,
) (*service.BatchImageBalanceHoldResult, error) {
	temporaryCaptured := 0.0
	permanentCaptured := 0.0
	if operation == batchImageHoldOperationCapture {
		var splitErr error
		temporaryCaptured, permanentCaptured, splitErr = splitBatchImageCapturedAmounts(cmd.ActualAmount, hold)
		if splitErr != nil {
			return nil, splitErr
		}
	}

	allocations, err := lockBatchImageTemporaryCreditAllocations(ctx, tx, hold.ID, hold.BatchID)
	if err != nil {
		return nil, err
	}
	allocationTotal := 0.0
	for _, allocation := range allocations {
		allocationTotal, err = addBatchImageLedgerAmount(allocationTotal, allocation.ReservedAmount)
		if err != nil {
			return nil, err
		}
	}
	if allocationTotal != hold.TemporaryReservedAmount {
		return nil, errors.New("batch image temporary credit allocation total does not match hold")
	}

	temporaryCaptureRemaining := temporaryCaptured
	temporaryRefunded := 0.0
	temporaryExpired := 0.0
	for _, allocation := range allocations {
		captured := minBatchImageLedgerAmount(allocation.ReservedAmount, temporaryCaptureRemaining)
		unused, subtractErr := subtractBatchImageLedgerAmount(allocation.ReservedAmount, captured)
		if subtractErr != nil {
			return nil, subtractErr
		}
		refunded := 0.0
		expired := 0.0
		if unused > 0 {
			if allocation.ExpiresAt.After(now) {
				res, restoreErr := tx.ExecContext(ctx, `
UPDATE temporary_credit_grants
SET remaining_amount = remaining_amount + $1,
    updated_at = $3
WHERE id = $2
	AND expires_at > clock_timestamp()`, formatBatchImageLedgerAmount(unused), allocation.GrantID, now)
				if restoreErr != nil {
					return nil, fmt.Errorf("restore batch image temporary credit grant %d: %w", allocation.GrantID, restoreErr)
				}
				affected, rowsErr := res.RowsAffected()
				if rowsErr != nil {
					return nil, rowsErr
				}
				if affected == 1 {
					refunded = unused
				} else if affected == 0 {
					expired = unused
				} else {
					return nil, fmt.Errorf("temporary credit grant %d refund affected %d rows", allocation.GrantID, affected)
				}
			} else {
				expired = unused
			}
		}
		if captured > 0 {
			_, insertErr := tx.ExecContext(ctx, `
INSERT INTO temporary_credit_consumptions (grant_id, request_id, amount)
VALUES ($1, $2, $3)`, allocation.GrantID, cmd.RequestID, formatBatchImageLedgerAmount(captured))
			if insertErr != nil {
				return nil, fmt.Errorf("insert batch image temporary credit consumption: %w", insertErr)
			}
		}
		res, updateErr := tx.ExecContext(ctx, `
UPDATE batch_image_credit_hold_allocations
SET captured_amount = $1,
    refunded_amount = $2,
    expired_amount = $3,
    updated_at = $5
WHERE id = $4
  AND captured_amount = 0
  AND refunded_amount = 0
  AND expired_amount = 0`,
			formatBatchImageLedgerAmount(captured),
			formatBatchImageLedgerAmount(refunded),
			formatBatchImageLedgerAmount(expired),
			allocation.ID,
			now,
		)
		if updateErr != nil {
			return nil, fmt.Errorf("settle batch image temporary credit allocation: %w", updateErr)
		}
		affected, rowsErr := res.RowsAffected()
		if rowsErr != nil {
			return nil, rowsErr
		}
		if affected != 1 {
			return nil, errors.New("batch image temporary credit allocation was already settled")
		}
		temporaryCaptureRemaining, err = subtractBatchImageLedgerAmount(temporaryCaptureRemaining, captured)
		if err != nil {
			return nil, err
		}
		temporaryRefunded, err = addBatchImageLedgerAmount(temporaryRefunded, refunded)
		if err != nil {
			return nil, err
		}
		temporaryExpired, err = addBatchImageLedgerAmount(temporaryExpired, expired)
		if err != nil {
			return nil, err
		}
	}
	if temporaryCaptureRemaining != 0 {
		return nil, errors.New("batch image temporary credit capture was not fully allocated")
	}

	var newBalance, frozenBalance *float64
	if hold.PermanentReservedAmount > 0 {
		permanentRefunded, subtractErr := subtractBatchImageLedgerAmount(hold.PermanentReservedAmount, permanentCaptured)
		if subtractErr != nil {
			return nil, subtractErr
		}
		var balance, frozen float64
		err = tx.QueryRowContext(ctx, `
UPDATE users
SET balance = balance + $1,
    frozen_balance = COALESCE(frozen_balance, 0) - $2,
    updated_at = $4
WHERE id = $3
  AND deleted_at IS NULL
  AND COALESCE(frozen_balance, 0) >= $2
RETURNING balance, frozen_balance`,
			formatBatchImageLedgerAmount(permanentRefunded),
			formatBatchImageLedgerAmount(hold.PermanentReservedAmount),
			hold.UserID,
			now,
		).Scan(&balance, &frozen)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("batch image permanent frozen balance is insufficient")
		}
		if err != nil {
			return nil, fmt.Errorf("settle batch image permanent balance: %w", err)
		}
		balance, err = normalizeBatchImageLedgerAmount(balance)
		if err != nil {
			return nil, err
		}
		frozen, err = normalizeBatchImageLedgerAmount(frozen)
		if err != nil || frozen < 0 {
			return nil, errors.New("settled batch image frozen balance is invalid")
		}
		newBalance = &balance
		frozenBalance = &frozen
	}

	capturedAmount := 0.0
	if operation == batchImageHoldOperationCapture {
		capturedAmount = cmd.ActualAmount
	}
	res, err := tx.ExecContext(ctx, `
UPDATE batch_image_credit_holds
SET status = $1,
    captured_amount = $2,
    temporary_captured_amount = $3,
    permanent_captured_amount = $4,
    expired_unrestored_amount = $5,
    terminal_fingerprint = $6,
    settled_at = $7,
    updated_at = $7
WHERE id = $8 AND status = 'reserved'`,
		terminalBatchImageHoldStatus(operation),
		formatBatchImageLedgerAmount(capturedAmount),
		formatBatchImageLedgerAmount(temporaryCaptured),
		formatBatchImageLedgerAmount(permanentCaptured),
		formatBatchImageLedgerAmount(temporaryExpired),
		cmd.RequestFingerprint,
		now,
		hold.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("finalize batch image credit hold: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if affected != 1 {
		return nil, errors.New("batch image credit hold terminal state changed concurrently")
	}

	return &service.BatchImageBalanceHoldResult{
		Applied:                 true,
		NewBalance:              newBalance,
		FrozenBalance:           frozenBalance,
		TemporaryReservedAmount: hold.TemporaryReservedAmount,
		PermanentReservedAmount: hold.PermanentReservedAmount,
		TemporaryCapturedAmount: temporaryCaptured,
		PermanentCapturedAmount: permanentCaptured,
		TemporaryRefundedAmount: temporaryRefunded,
		TemporaryExpiredAmount:  temporaryExpired,
	}, nil
}

func splitBatchImageCapturedAmounts(actualAmount float64, hold *batchImageCreditHoldRecord) (float64, float64, error) {
	if hold == nil {
		return 0, 0, errors.New("batch image credit hold is required")
	}
	actualAmount, err := normalizeBatchImageLedgerAmount(actualAmount)
	if err != nil || actualAmount < 0 {
		return 0, 0, service.ErrUsageBillingAmountInvalid
	}
	if actualAmount > hold.HoldAmount {
		return 0, 0, service.ErrBatchImageSettlementCostExceedsHold
	}
	temporaryCaptured := minBatchImageLedgerAmount(actualAmount, hold.TemporaryReservedAmount)
	permanentCaptured, err := subtractBatchImageLedgerAmount(actualAmount, temporaryCaptured)
	if err != nil {
		return 0, 0, err
	}
	if permanentCaptured > hold.PermanentReservedAmount {
		return 0, 0, service.ErrBatchImageSettlementCostExceedsHold
	}
	return temporaryCaptured, permanentCaptured, nil
}

func lockBatchImageTemporaryCreditAllocations(ctx context.Context, tx *sql.Tx, holdID int64, batchID string) ([]batchImageTemporaryCreditAllocation, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT allocation.id, allocation.grant_id, allocation.reserved_amount, credit_grant.expires_at
FROM batch_image_credit_hold_allocations allocation
JOIN temporary_credit_grants credit_grant ON credit_grant.id = allocation.grant_id
WHERE allocation.hold_id = $1 AND allocation.batch_id = $2
ORDER BY allocation.grant_expires_at ASC, allocation.grant_id ASC
FOR UPDATE OF allocation, credit_grant`, holdID, batchID)
	if err != nil {
		return nil, fmt.Errorf("lock batch image temporary credit allocations: %w", err)
	}
	defer rows.Close()

	allocations := make([]batchImageTemporaryCreditAllocation, 0)
	for rows.Next() {
		var allocation batchImageTemporaryCreditAllocation
		if err := rows.Scan(&allocation.ID, &allocation.GrantID, &allocation.ReservedAmount, &allocation.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan batch image temporary credit allocation: %w", err)
		}
		allocation.ReservedAmount, err = normalizeBatchImageLedgerAmount(allocation.ReservedAmount)
		if err != nil || allocation.ReservedAmount <= 0 {
			return nil, errors.New("batch image temporary credit allocation amount is invalid")
		}
		allocations = append(allocations, allocation)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch image temporary credit allocations: %w", err)
	}
	return allocations, nil
}

func materializeLegacyBatchImageCreditHold(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.BatchImageBalanceHoldCommand,
	job *batchImageBillingJobSnapshot,
	dedup batchImageHoldDedupState,
	requestedOperation batchImageHoldOperation,
	now time.Time,
) (*batchImageCreditHoldRecord, *service.BatchImageBalanceHoldResult, error) {
	if !dedup.Reserve.Found {
		return nil, nil, errors.New("legacy batch image hold has no reserve dedup")
	}
	if dedup.Capture.Found && dedup.Release.Found {
		return nil, nil, errors.New("legacy batch image hold has ambiguous terminal dedup records")
	}

	status := "reserved"
	terminalFingerprint := (*string)(nil)
	capturedAmount := 0.0
	permanentCaptured := 0.0
	if dedup.Capture.Found {
		status = "captured"
		fingerprint := dedup.Capture.Fingerprint
		terminalFingerprint = &fingerprint
		if requestedOperation == batchImageHoldOperationCapture {
			if !service.MatchesBatchImageBalanceHoldFingerprint(fingerprint, cmd) {
				return nil, nil, service.ErrUsageBillingRequestConflict
			}
			capturedAmount = cmd.ActualAmount
			if job.ActualCost != nil && *job.ActualCost != capturedAmount {
				return nil, nil, service.ErrUsageBillingRequestConflict
			}
		} else {
			if job.ActualCost == nil {
				return nil, nil, errors.New("legacy captured batch image hold has no auditable actual cost")
			}
			capturedAmount = *job.ActualCost
		}
		permanentCaptured = capturedAmount
	}
	if dedup.Release.Found {
		status = "released"
		fingerprint := dedup.Release.Fingerprint
		terminalFingerprint = &fingerprint
	}
	if requestedOperation == batchImageHoldOperationCapture && status != "captured" {
		return nil, nil, errors.New("legacy capture dedup is missing")
	}
	if requestedOperation == batchImageHoldOperationRelease && status != "released" {
		return nil, nil, errors.New("legacy release dedup is missing")
	}
	if capturedAmount > cmd.HoldAmount {
		return nil, nil, service.ErrBatchImageSettlementCostExceedsHold
	}

	var resultBalance, resultFrozen *float64
	if status == "reserved" {
		balance, frozen, err := validateLegacyBatchImageFrozenAttribution(ctx, tx, cmd.UserID, cmd.HoldAmount)
		if err != nil {
			return nil, nil, err
		}
		resultBalance = &balance
		resultFrozen = &frozen
	}

	reservedAt := job.CreatedAt
	if reservedAt.IsZero() || reservedAt.After(now) {
		reservedAt = now
	}
	var settledAt any
	if status != "reserved" {
		settledAt = now
	}
	var holdID int64
	err := tx.QueryRowContext(ctx, `
INSERT INTO batch_image_credit_holds (
    batch_id, user_id, api_key_id, group_id, status,
    hold_amount, temporary_reserved_amount, permanent_reserved_amount,
    captured_amount, temporary_captured_amount, permanent_captured_amount,
    expired_unrestored_amount, reserve_fingerprint, terminal_fingerprint,
    reserved_at, settled_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, 0, $6, $7, 0, $7, 0, $8, $9, $10, $11, $12)
RETURNING id`,
		cmd.BatchID,
		cmd.UserID,
		cmd.APIKeyID,
		nullableBatchImageGroupID(job.GroupID),
		status,
		formatBatchImageLedgerAmount(cmd.HoldAmount),
		formatBatchImageLedgerAmount(permanentCaptured),
		dedup.Reserve.Fingerprint,
		nullableBatchImageFingerprint(terminalFingerprint),
		reservedAt,
		settledAt,
		now,
	).Scan(&holdID)
	if err != nil {
		return nil, nil, fmt.Errorf("materialize legacy batch image credit hold: %w", err)
	}

	hold := &batchImageCreditHoldRecord{
		ID:                      holdID,
		BatchID:                 cmd.BatchID,
		UserID:                  cmd.UserID,
		APIKeyID:                cmd.APIKeyID,
		GroupID:                 cloneBatchImageGroupID(job.GroupID),
		Status:                  status,
		HoldAmount:              cmd.HoldAmount,
		PermanentReservedAmount: cmd.HoldAmount,
		CapturedAmount:          capturedAmount,
		PermanentCapturedAmount: permanentCaptured,
		ReserveFingerprint:      dedup.Reserve.Fingerprint,
		TerminalFingerprint:     terminalFingerprint,
	}
	result := batchImageCreditHoldResult(hold, false)
	result.NewBalance = resultBalance
	result.FrozenBalance = resultFrozen
	return hold, result, nil
}

func validateLegacyBatchImageFrozenAttribution(ctx context.Context, tx *sql.Tx, userID int64, holdAmount float64) (float64, float64, error) {
	balance, frozen, err := lockBatchImageUserBalance(ctx, tx, userID)
	if err != nil {
		return 0, 0, err
	}
	var attributed float64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(permanent_reserved_amount), 0)
FROM batch_image_credit_holds
WHERE user_id = $1 AND status = 'reserved'`, userID).Scan(&attributed); err != nil {
		return 0, 0, fmt.Errorf("sum attributed legacy batch image frozen balance: %w", err)
	}
	attributed, err = normalizeBatchImageLedgerAmount(attributed)
	if err != nil || attributed < 0 {
		return 0, 0, errors.New("attributed batch image frozen balance is invalid")
	}
	required, err := addBatchImageLedgerAmount(attributed, holdAmount)
	if err != nil {
		return 0, 0, err
	}
	if frozen < required {
		return 0, 0, errors.New("legacy batch image frozen balance cannot be attributed safely")
	}
	return balance, frozen, nil
}

func batchImageCreditHoldResult(hold *batchImageCreditHoldRecord, applied bool) *service.BatchImageBalanceHoldResult {
	if hold == nil {
		return &service.BatchImageBalanceHoldResult{Applied: applied}
	}
	refunded := 0.0
	if hold.Status != "reserved" {
		refunded, _ = subtractBatchImageLedgerAmount(hold.TemporaryReservedAmount, hold.TemporaryCapturedAmount)
		refunded, _ = subtractBatchImageLedgerAmount(refunded, hold.ExpiredUnrestoredAmount)
	}
	return &service.BatchImageBalanceHoldResult{
		Applied:                 applied,
		TemporaryReservedAmount: hold.TemporaryReservedAmount,
		PermanentReservedAmount: hold.PermanentReservedAmount,
		TemporaryCapturedAmount: hold.TemporaryCapturedAmount,
		PermanentCapturedAmount: hold.PermanentCapturedAmount,
		TemporaryRefundedAmount: refunded,
		TemporaryExpiredAmount:  hold.ExpiredUnrestoredAmount,
	}
}

func nullableBatchImageFingerprint(fingerprint *string) any {
	if fingerprint == nil {
		return nil
	}
	return *fingerprint
}
