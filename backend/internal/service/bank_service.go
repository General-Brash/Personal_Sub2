package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyBankAdvanceMinAmount    = "bank_advance_min_amount"
	SettingKeyBankAdvanceMaxAmount    = "bank_advance_max_amount"
	SettingKeyBankDebtGraceDays       = "bank_debt_grace_days"
	SettingKeyBankDebtConversionRatio = "bank_debt_conversion_ratio"
	SettingKeyBankExchangeRate        = "bank_exchange_rate"
	bankSettlementInterval            = time.Minute
	bankLedgerPageSize                = 50
)

var (
	ErrBankPolicyInvalid             = infraerrors.BadRequest("INVALID_BANK_POLICY", "bank policy is invalid")
	ErrBankAdvanceAmountOutOfRange   = infraerrors.BadRequest("BANK_ADVANCE_OUT_OF_RANGE", "bank advance amount is outside the configured range")
	ErrBankAdvanceAlreadyOutstanding = infraerrors.Conflict("BANK_ADVANCE_OUTSTANDING", "a bank advance is already outstanding")
	ErrBankPermanentBalanceNegative  = infraerrors.Forbidden("PERMANENT_BALANCE_NEGATIVE", "permanent balance is negative")
	ErrBankPermanentInsufficient     = infraerrors.Conflict("PERMANENT_BALANCE_INSUFFICIENT", "permanent balance is insufficient for exchange")
	ErrBankDebtSettlementPending     = infraerrors.ServiceUnavailable("BANK_DEBT_SETTLEMENT_PENDING", "bank debt settlement is pending")
	ErrBankAmountInvalid             = infraerrors.BadRequest("INVALID_BANK_AMOUNT", "bank amount is invalid")
	ErrBankExchangeMaintenanceWindow = infraerrors.Forbidden("BANK_EXCHANGE_MAINTENANCE_WINDOW", "bank exchange is unavailable daily from 23:55 to 00:05 Asia/Shanghai").WithMetadata(map[string]string{
		"timezone":     "Asia/Shanghai",
		"window_start": "23:55:00",
		"window_end":   "00:05:00",
	})
)

// BankPolicy is the persisted administrator policy. Amount fields are kept as
// float64 internally and serialized as fixed eight-decimal strings at the API
// boundary, matching the existing credit-ledger contract.
type BankPolicy struct {
	AdvanceMinAmount    float64 `json:"advance_min_amount"`
	AdvanceMaxAmount    float64 `json:"advance_max_amount"`
	DebtGraceDays       int     `json:"debt_grace_days"`
	DebtConversionRatio float64 `json:"debt_conversion_ratio"`
	ExchangeRate        float64 `json:"exchange_rate"`
}

func DefaultBankPolicy() BankPolicy {
	return BankPolicy{
		AdvanceMinAmount:    5,
		AdvanceMaxAmount:    20,
		DebtGraceDays:       3,
		DebtConversionRatio: 1,
		ExchangeRate:        1,
	}
}

func (p BankPolicy) Validate() error {
	min, err := normalizeLedgerAmount(p.AdvanceMinAmount)
	if err != nil || min <= 0 {
		return ErrBankPolicyInvalid
	}
	max, err := normalizeLedgerAmount(p.AdvanceMaxAmount)
	if err != nil || max < min {
		return ErrBankPolicyInvalid
	}
	conversion, err := normalizeLedgerAmount(p.DebtConversionRatio)
	if err != nil || conversion <= 0 {
		return ErrBankPolicyInvalid
	}
	exchange, err := normalizeLedgerAmount(p.ExchangeRate)
	if err != nil || exchange <= 0 {
		return ErrBankPolicyInvalid
	}
	if p.DebtGraceDays < 1 || p.DebtGraceDays > 365 {
		return ErrBankPolicyInvalid
	}
	return nil
}

func (p BankPolicy) normalized() (BankPolicy, error) {
	if err := p.Validate(); err != nil {
		return BankPolicy{}, err
	}
	p.AdvanceMinAmount, _ = normalizeLedgerAmount(p.AdvanceMinAmount)
	p.AdvanceMaxAmount, _ = normalizeLedgerAmount(p.AdvanceMaxAmount)
	p.DebtConversionRatio, _ = normalizeLedgerAmount(p.DebtConversionRatio)
	p.ExchangeRate, _ = normalizeLedgerAmount(p.ExchangeRate)
	return p, nil
}

type BankPolicyDTO struct {
	AdvanceMinAmount    string `json:"advance_min_amount"`
	AdvanceMaxAmount    string `json:"advance_max_amount"`
	DebtGraceDays       int    `json:"debt_grace_days"`
	DebtConversionRatio string `json:"debt_conversion_ratio"`
	ExchangeRate        string `json:"exchange_rate"`
}

func (p BankPolicy) DTO() BankPolicyDTO {
	return BankPolicyDTO{
		AdvanceMinAmount:    formatLedgerAmount(p.AdvanceMinAmount),
		AdvanceMaxAmount:    formatLedgerAmount(p.AdvanceMaxAmount),
		DebtGraceDays:       p.DebtGraceDays,
		DebtConversionRatio: formatLedgerAmount(p.DebtConversionRatio),
		ExchangeRate:        formatLedgerAmount(p.ExchangeRate),
	}
}

func bankPolicyFromDTO(dto BankPolicyDTO) (BankPolicy, error) {
	min, err := ParseStrictPositiveLedgerAmount(dto.AdvanceMinAmount)
	if err != nil {
		return BankPolicy{}, ErrBankPolicyInvalid
	}
	max, err := ParseStrictPositiveLedgerAmount(dto.AdvanceMaxAmount)
	if err != nil {
		return BankPolicy{}, ErrBankPolicyInvalid
	}
	conversion, err := ParseStrictPositiveLedgerAmount(dto.DebtConversionRatio)
	if err != nil {
		return BankPolicy{}, ErrBankPolicyInvalid
	}
	exchange, err := ParseStrictPositiveLedgerAmount(dto.ExchangeRate)
	if err != nil {
		return BankPolicy{}, ErrBankPolicyInvalid
	}
	return (BankPolicy{
		AdvanceMinAmount:    min,
		AdvanceMaxAmount:    max,
		DebtGraceDays:       dto.DebtGraceDays,
		DebtConversionRatio: conversion,
		ExchangeRate:        exchange,
	}).normalized()
}

type BankAdvanceStatus struct {
	ID              int64     `json:"id"`
	Principal       string    `json:"principal"`
	DebtRemaining   string    `json:"debt_remaining"`
	Status          string    `json:"status"`
	GrantedAt       time.Time `json:"granted_at"`
	GrantExpiresAt  time.Time `json:"grant_expires_at"`
	SettlementDueAt time.Time `json:"settlement_due_at"`
}

type BankLedgerItem struct {
	ID             int64     `json:"id"`
	Operation      string    `json:"operation"`
	LoanID         *int64    `json:"loan_id,omitempty"`
	GrantID        *int64    `json:"grant_id,omitempty"`
	PermanentDelta string    `json:"permanent_delta"`
	TemporaryDelta string    `json:"temporary_delta"`
	DebtDelta      string    `json:"debt_delta"`
	DebtBefore     string    `json:"debt_before"`
	DebtAfter      string    `json:"debt_after"`
	CreatedAt      time.Time `json:"created_at"`
}

type BankStatus struct {
	PermanentBalance                 string             `json:"permanent_balance"`
	TemporaryCreditAvailable         string             `json:"temporary_credit_available"`
	TemporaryCreditEarliestExpiresAt *time.Time         `json:"temporary_credit_earliest_expires_at"`
	TemporaryDebt                    string             `json:"temporary_debt"`
	TemporaryDebtDueAt               *time.Time         `json:"temporary_debt_due_at"`
	ActiveAdvance                    *BankAdvanceStatus `json:"active_advance"`
	Policy                           BankPolicyDTO      `json:"policy"`
	Ledger                           []BankLedgerItem   `json:"ledger"`
}

type BankAdvanceResult struct {
	AdvanceID              int64     `json:"advance_id"`
	TemporaryCreditGrantID int64     `json:"temporary_credit_grant_id"`
	Amount                 string    `json:"amount"`
	TemporaryDebt          string    `json:"temporary_debt"`
	ExpiresAt              time.Time `json:"expires_at"`
	SettlementDueAt        time.Time `json:"settlement_due_at"`
}

type BankExchangeResult struct {
	PermanentSpent     string    `json:"permanent_spent"`
	TemporaryGranted   string    `json:"temporary_granted"`
	TemporaryAvailable string    `json:"temporary_available"`
	PermanentBalance   string    `json:"permanent_balance"`
	TemporaryDebt      string    `json:"temporary_debt"`
	ExpiresAt          time.Time `json:"expires_at"`
}

// BankService owns bank mutations and the due-debt worker. It deliberately
// uses database/sql so user balance, debt, loan, ledger, and idempotency rows
// can share one transaction.
type BankService struct {
	db              *sql.DB
	temporaryCredit *TemporaryCreditService
	billingCache    BillingCache
	stop            chan struct{}
	workerOnce      sync.Once
	stopOnce        sync.Once
	workerWG        sync.WaitGroup
}

func NewBankService(
	db *sql.DB,
	temporaryCreditRepo TemporaryCreditRepository,
	billingCache BillingCache,
) *BankService {
	var temporaryCredit *TemporaryCreditService
	if temporaryCreditRepo != nil {
		temporaryCredit = NewTemporaryCreditService(temporaryCreditRepo)
	}
	return &BankService{
		db:              db,
		temporaryCredit: temporaryCredit,
		billingCache:    billingCache,
		stop:            make(chan struct{}),
	}
}

// Start launches a lightweight due-debt poller. The poller is intentionally
// minute-granular; due timestamps are persisted in Asia/Shanghai calendar
// time and each mutation/eligibility read also performs a lazy settlement.
func (s *BankService) Start() {
	if s == nil || s.db == nil || s.temporaryCredit == nil {
		return
	}
	s.workerOnce.Do(func() {
		s.workerWG.Add(1)
		go func() {
			defer s.workerWG.Done()
			_ = s.SettleDue(context.Background())
			ticker := time.NewTicker(bankSettlementInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					_ = s.SettleDue(context.Background())
				case <-s.stop:
					return
				}
			}
		}()
	})
}

func (s *BankService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stop) })
	s.workerWG.Wait()
}

type bankQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

var bankPolicyKeys = []string{
	SettingKeyBankAdvanceMinAmount,
	SettingKeyBankAdvanceMaxAmount,
	SettingKeyBankDebtGraceDays,
	SettingKeyBankDebtConversionRatio,
	SettingKeyBankExchangeRate,
}

func loadBankPolicy(ctx context.Context, q bankQueryer) (BankPolicy, error) {
	policy := DefaultBankPolicy()
	args := make([]any, 0, len(bankPolicyKeys))
	placeholders := make([]string, 0, len(bankPolicyKeys))
	for i, key := range bankPolicyKeys {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, key)
	}
	rows, err := q.QueryContext(ctx, `SELECT key, value FROM settings WHERE key IN (`+strings.Join(placeholders, ",")+")", args...)
	if err != nil {
		return BankPolicy{}, fmt.Errorf("load bank policy settings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	values := make(map[string]string, len(bankPolicyKeys))
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return BankPolicy{}, fmt.Errorf("scan bank policy setting: %w", err)
		}
		values[key] = value
	}
	if err := rows.Err(); err != nil {
		return BankPolicy{}, fmt.Errorf("iterate bank policy settings: %w", err)
	}
	if raw, ok := values[SettingKeyBankAdvanceMinAmount]; ok {
		policy.AdvanceMinAmount, err = ParseStrictPositiveLedgerAmount(raw)
		if err != nil {
			return BankPolicy{}, ErrBankPolicyInvalid
		}
	}
	if raw, ok := values[SettingKeyBankAdvanceMaxAmount]; ok {
		policy.AdvanceMaxAmount, err = ParseStrictPositiveLedgerAmount(raw)
		if err != nil {
			return BankPolicy{}, ErrBankPolicyInvalid
		}
	}
	if raw, ok := values[SettingKeyBankDebtGraceDays]; ok {
		policy.DebtGraceDays, err = strconv.Atoi(strings.TrimSpace(raw))
		if err != nil {
			return BankPolicy{}, ErrBankPolicyInvalid
		}
	}
	if raw, ok := values[SettingKeyBankDebtConversionRatio]; ok {
		policy.DebtConversionRatio, err = ParseStrictPositiveLedgerAmount(raw)
		if err != nil {
			return BankPolicy{}, ErrBankPolicyInvalid
		}
	}
	if raw, ok := values[SettingKeyBankExchangeRate]; ok {
		policy.ExchangeRate, err = ParseStrictPositiveLedgerAmount(raw)
		if err != nil {
			return BankPolicy{}, ErrBankPolicyInvalid
		}
	}
	return policy.normalized()
}

func bankPolicyValues(policy BankPolicy) (map[string]string, error) {
	normalized, err := policy.normalized()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		SettingKeyBankAdvanceMinAmount:    formatLedgerAmount(normalized.AdvanceMinAmount),
		SettingKeyBankAdvanceMaxAmount:    formatLedgerAmount(normalized.AdvanceMaxAmount),
		SettingKeyBankDebtGraceDays:       strconv.Itoa(normalized.DebtGraceDays),
		SettingKeyBankDebtConversionRatio: formatLedgerAmount(normalized.DebtConversionRatio),
		SettingKeyBankExchangeRate:        formatLedgerAmount(normalized.ExchangeRate),
	}, nil
}

func writeBankPolicyTx(ctx context.Context, tx *sql.Tx, policy BankPolicy) error {
	values, err := bankPolicyValues(policy)
	if err != nil {
		return err
	}
	for _, key := range bankPolicyKeys {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO settings (key, value, updated_at)
VALUES ($1, $2, clock_timestamp())
ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = clock_timestamp()`, key, values[key]); err != nil {
			return fmt.Errorf("write bank policy setting %s: %w", key, err)
		}
	}
	return nil
}

func parseBankAmount(raw string) (float64, error) {
	amount, err := ParseStrictPositiveLedgerAmount(strings.TrimSpace(raw))
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return 0, ErrBankAmountInvalid
	}
	return amount, nil
}

func bankExchangeInMaintenanceWindow(now time.Time) bool {
	beijingNow := now.In(beijingLocation)
	secondsSinceMidnight := beijingNow.Hour()*60*60 + beijingNow.Minute()*60 + beijingNow.Second()
	return secondsSinceMidnight >= 23*60*60+55*60 || secondsSinceMidnight < 5*60
}

func marshalBankMetadata(values map[string]any) []byte {
	if len(values) == 0 {
		return []byte(`{}`)
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return []byte(`{}`)
	}
	return raw
}

func (s *BankService) GetPolicy(ctx context.Context) (BankPolicyDTO, error) {
	if s == nil || s.db == nil {
		return BankPolicyDTO{}, errors.New("bank service database is nil")
	}
	policy, err := loadBankPolicy(ctx, s.db)
	if err != nil {
		return BankPolicyDTO{}, err
	}
	return policy.DTO(), nil
}

func (s *BankService) UpdatePolicyAtomic(
	ctx context.Context,
	actorID int64,
	dto BankPolicyDTO,
	claim *IdempotencyAtomicClaim,
) (*BankPolicyDTO, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("bank service database is nil")
	}
	if actorID <= 0 || claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	policy, err := bankPolicyFromDTO(dto)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bank policy transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeBankPolicyTx(ctx, tx, policy); err != nil {
		return nil, err
	}
	result := policy.DTO()
	if err := claim.PersistSuccess(ctx, tx, &result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bank policy transaction: %w", err)
	}
	return &result, nil
}

func (s *BankService) GetStatus(ctx context.Context, userID int64) (*BankStatus, error) {
	if s == nil || s.db == nil || s.temporaryCredit == nil || s.temporaryCredit.repo == nil {
		return nil, errors.New("bank service is not configured")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if err := s.SettleDueForUser(ctx, userID); err != nil {
		return nil, err
	}
	policy, err := loadBankPolicy(ctx, s.db)
	if err != nil {
		return nil, err
	}

	var balance, debt float64
	var debtDue sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at
FROM users
WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&balance, &debt, &debtDue); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("load bank user status: %w", err)
	}
	available, earliestExpiry, err := s.temporaryCredit.repo.AvailableSummary(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("load bank temporary credit: %w", err)
	}
	advance, err := loadActiveBankAdvance(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	ledger, err := loadBankLedger(ctx, s.db, userID, bankLedgerPageSize)
	if err != nil {
		return nil, err
	}
	if earliestExpiry != nil {
		utc := earliestExpiry.UTC()
		earliestExpiry = &utc
	}
	var debtDueAt *time.Time
	if debtDue.Valid {
		utc := debtDue.Time.UTC()
		debtDueAt = &utc
	}
	return &BankStatus{
		PermanentBalance:                 formatLedgerAmount(balance),
		TemporaryCreditAvailable:         formatLedgerAmount(available),
		TemporaryCreditEarliestExpiresAt: earliestExpiry,
		TemporaryDebt:                    formatLedgerAmount(debt),
		TemporaryDebtDueAt:               debtDueAt,
		ActiveAdvance:                    advance,
		Policy:                           policy.DTO(),
		Ledger:                           ledger,
	}, nil
}

func (s *BankService) AdvanceAtomic(
	ctx context.Context,
	userID int64,
	amount float64,
	claim *IdempotencyAtomicClaim,
) (*BankAdvanceResult, error) {
	if s == nil || s.db == nil || s.temporaryCredit == nil {
		return nil, errors.New("bank service is not configured")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if err := ValidateTemporaryCreditAmount(amount); err != nil {
		return nil, ErrBankAmountInvalid
	}
	amount, _ = normalizeLedgerAmount(amount)
	if err := s.SettleDueForUser(ctx, userID); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bank advance transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	policy, err := loadBankPolicy(ctx, tx)
	if err != nil {
		return nil, err
	}
	if amount < policy.AdvanceMinAmount || amount > policy.AdvanceMaxAmount {
		return nil, ErrBankAdvanceAmountOutOfRange
	}

	balance, debt, _, err := lockBankUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if balance < 0 {
		return nil, ErrBankPermanentBalanceNegative
	}
	if debt > ledgerAmountEpsilon {
		return nil, ErrBankAdvanceAlreadyOutstanding
	}
	var existingLoanID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM bank_loans WHERE user_id = $1 AND status = 'active' FOR UPDATE`, userID).Scan(&existingLoanID)
	if err == nil {
		return nil, ErrBankAdvanceAlreadyOutstanding
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check active bank advance: %w", err)
	}
	var businessNow time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&businessNow); err != nil {
		return nil, fmt.Errorf("sample bank transaction clock: %w", err)
	}
	grant, err := s.temporaryCredit.CreateGrantTx(ctx, tx, CreateTemporaryCreditGrantInput{
		UserID:      userID,
		Source:      TemporaryCreditSourceBankAdvance,
		Amount:      amount,
		businessNow: &businessNow,
	})
	if err != nil {
		return nil, fmt.Errorf("create bank advance grant: %w", err)
	}
	settlementDueAt := grant.ExpiresAt().In(beijingLocation).AddDate(0, 0, policy.DebtGraceDays)
	var advanceID int64
	if err := tx.QueryRowContext(ctx, `
INSERT INTO bank_loans
    (user_id, grant_id, principal, debt_remaining, status, granted_at, grant_expires_at, settlement_due_at)
VALUES ($1, $2, $3, $3, 'active', $4, $5, $6)
RETURNING id`, userID, grant.ID, formatLedgerAmount(amount), businessNow, grant.ExpiresAt(), settlementDueAt).Scan(&advanceID); err != nil {
		return nil, fmt.Errorf("create bank advance record: %w", err)
	}
	var debtAfter float64
	if err := tx.QueryRowContext(ctx, `
UPDATE users
SET temporary_credit_debt = temporary_credit_debt + $1,
    temporary_credit_debt_due_at = $2,
    updated_at = clock_timestamp()
WHERE id = $3 AND deleted_at IS NULL
RETURNING temporary_credit_debt`, formatLedgerAmount(amount), settlementDueAt, userID).Scan(&debtAfter); err != nil {
		return nil, fmt.Errorf("record bank advance debt: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_ledger
    (user_id, operation, loan_id, grant_id, actor_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after, metadata)
VALUES ($1, 'advance', $2, $3, $1, 0, $4, $4, $5, $6, $7)`,
		userID,
		advanceID,
		grant.ID,
		formatLedgerAmount(amount),
		formatLedgerAmount(debt),
		formatLedgerAmount(debtAfter),
		marshalBankMetadata(map[string]any{"grace_days": policy.DebtGraceDays}),
	); err != nil {
		return nil, fmt.Errorf("record bank advance ledger: %w", err)
	}
	result := &BankAdvanceResult{
		AdvanceID:              advanceID,
		TemporaryCreditGrantID: grant.ID,
		Amount:                 formatLedgerAmount(amount),
		TemporaryDebt:          formatLedgerAmount(debtAfter),
		ExpiresAt:              grant.ExpiresAt().UTC(),
		SettlementDueAt:        settlementDueAt.UTC(),
	}
	if err := claim.PersistSuccess(ctx, tx, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bank advance transaction: %w", err)
	}
	s.invalidateBankCredits(ctx, userID, false)
	return result, nil
}

func (s *BankService) ExchangeAtomic(
	ctx context.Context,
	userID int64,
	permanentAmount float64,
	claim *IdempotencyAtomicClaim,
) (*BankExchangeResult, error) {
	if s == nil || s.db == nil || s.temporaryCredit == nil {
		return nil, errors.New("bank service is not configured")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if err := ValidateTemporaryCreditAmount(permanentAmount); err != nil {
		return nil, ErrBankAmountInvalid
	}
	permanentAmount, _ = normalizeLedgerAmount(permanentAmount)
	if err := s.SettleDueForUser(ctx, userID); err != nil {
		return nil, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bank exchange transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	balance, debtBefore, dueAt, err := lockBankUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	policy, err := loadBankPolicy(ctx, tx)
	if err != nil {
		return nil, err
	}
	var businessNow time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&businessNow); err != nil {
		return nil, fmt.Errorf("sample bank exchange clock: %w", err)
	}
	if _, err := settleBankDebtLocked(ctx, tx, userID, balance, debtBefore, dueAt, policy, businessNow); err != nil {
		return nil, err
	}
	if bankExchangeInMaintenanceWindow(businessNow) {
		return nil, ErrBankExchangeMaintenanceWindow
	}
	balance, debtBefore, _, err = lockBankUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if balance < 0 {
		return nil, ErrBankPermanentBalanceNegative
	}
	if balance+ledgerAmountEpsilon < permanentAmount {
		return nil, ErrBankPermanentInsufficient
	}
	temporaryAmount, err := normalizeLedgerAmount(permanentAmount * policy.ExchangeRate)
	if err != nil || temporaryAmount <= 0 {
		return nil, ErrBankAmountInvalid
	}
	var deductedBalance float64
	if err := tx.QueryRowContext(ctx, `
UPDATE users
SET balance = balance - $1, updated_at = clock_timestamp()
WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
RETURNING balance`, formatLedgerAmount(permanentAmount), userID).Scan(&deductedBalance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBankPermanentInsufficient
		}
		return nil, fmt.Errorf("deduct permanent balance for bank exchange: %w", err)
	}
	grant, err := s.temporaryCredit.CreateGrantTx(ctx, tx, CreateTemporaryCreditGrantInput{
		UserID:      userID,
		Source:      TemporaryCreditSourceBankExchange,
		Amount:      temporaryAmount,
		businessNow: &businessNow,
	})
	if err != nil {
		return nil, fmt.Errorf("create bank exchange grant: %w", err)
	}
	var balanceAfter, debtAfter float64
	if err := tx.QueryRowContext(ctx, `SELECT balance, temporary_credit_debt FROM users WHERE id = $1`, userID).Scan(&balanceAfter, &debtAfter); err != nil {
		return nil, fmt.Errorf("load bank balances after exchange: %w", err)
	}
	temporaryAvailable, err := temporaryCreditAvailableInTx(ctx, tx, userID, businessNow)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_ledger
    (user_id, operation, grant_id, actor_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after, metadata)
VALUES ($1, 'exchange', $2, $1, $3, $4, 0, $5, $6, $7)`,
		userID,
		grant.ID,
		formatLedgerAmount(-permanentAmount),
		formatLedgerAmount(temporaryAmount),
		formatLedgerAmount(debtBefore),
		formatLedgerAmount(debtAfter),
		marshalBankMetadata(map[string]any{"exchange_rate": formatLedgerAmount(policy.ExchangeRate)}),
	); err != nil {
		return nil, fmt.Errorf("record bank exchange ledger: %w", err)
	}
	result := &BankExchangeResult{
		PermanentSpent:     formatLedgerAmount(permanentAmount),
		TemporaryGranted:   formatLedgerAmount(temporaryAmount),
		TemporaryAvailable: formatLedgerAmount(temporaryAvailable),
		PermanentBalance:   formatLedgerAmount(balanceAfter),
		TemporaryDebt:      formatLedgerAmount(debtAfter),
		ExpiresAt:          grant.ExpiresAt().UTC(),
	}
	if err := claim.PersistSuccess(ctx, tx, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bank exchange transaction: %w", err)
	}
	s.invalidateBankCredits(ctx, userID, true)
	return result, nil
}

func temporaryCreditAvailableInTx(ctx context.Context, tx *sql.Tx, userID int64, now time.Time) (float64, error) {
	var available float64
	if err := tx.QueryRowContext(ctx, `
SELECT COALESCE(SUM(remaining_amount), 0)
FROM temporary_credit_grants
WHERE user_id = $1
  AND remaining_amount > 0
  AND expires_at > $2`, userID, now).Scan(&available); err != nil {
		return 0, fmt.Errorf("load temporary credit after bank exchange: %w", err)
	}
	normalized, err := normalizeDerivedLedgerAmount(available)
	if err != nil || normalized < 0 {
		return 0, fmt.Errorf("invalid temporary credit after bank exchange")
	}
	return normalized, nil
}

func (s *BankService) SettleDueForUser(ctx context.Context, userID int64) error {
	if s == nil || s.db == nil {
		return errors.New("bank service database is nil")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bank settlement transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	policy, err := loadBankPolicy(ctx, tx)
	if err != nil {
		return err
	}
	balance, debt, dueAt, err := lockBankUser(ctx, tx, userID)
	if err != nil {
		return err
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return fmt.Errorf("sample bank settlement clock: %w", err)
	}
	settled, err := settleBankDebtLocked(ctx, tx, userID, balance, debt, dueAt, policy, now)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bank settlement transaction: %w", err)
	}
	if settled {
		s.invalidateBankCredits(ctx, userID, true)
	}
	return nil
}

// SettleDue finds due accounts and delegates each one to the idempotent,
// transaction-bound user settlement. Multiple application instances may run
// this concurrently; the user row lock and cleared debt make repeats no-ops.
func (s *BankService) SettleDue(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id
FROM users
WHERE deleted_at IS NULL
  AND temporary_credit_debt > 0
  AND temporary_credit_debt_due_at IS NOT NULL
  AND temporary_credit_debt_due_at <= clock_timestamp()
ORDER BY id
LIMIT 500`)
	if err != nil {
		return fmt.Errorf("list due bank debts: %w", err)
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan due bank debt user: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate due bank debts: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close due bank debt rows: %w", err)
	}
	for _, userID := range ids {
		if err := s.SettleDueForUser(ctx, userID); err != nil {
			return err
		}
	}
	return nil
}

// CheckPermanentBalanceEligibility performs lazy debt settlement, then rejects
// any account whose permanent balance is negative even when temporary credit
// is still positive.
func (s *BankService) CheckPermanentBalanceEligibility(ctx context.Context, userID int64) error {
	if err := s.SettleDueForUser(ctx, userID); err != nil {
		return ErrBankDebtSettlementPending
	}
	var balance float64
	if err := s.db.QueryRowContext(ctx, `SELECT balance FROM users WHERE id = $1 AND deleted_at IS NULL`, userID).Scan(&balance); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrUserNotFound
		}
		return ErrBankDebtSettlementPending
	}
	if math.IsNaN(balance) || math.IsInf(balance, 0) || balance < 0 {
		return ErrInsufficientBalance
	}
	return nil
}

func lockBankUser(ctx context.Context, tx *sql.Tx, userID int64) (balance, debt float64, dueAt sql.NullTime, err error) {
	err = tx.QueryRowContext(ctx, `
SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at
FROM users
WHERE id = $1 AND deleted_at IS NULL
FOR UPDATE`, userID).Scan(&balance, &debt, &dueAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, sql.NullTime{}, ErrUserNotFound
	}
	if err != nil {
		return 0, 0, sql.NullTime{}, fmt.Errorf("lock bank user: %w", err)
	}
	return balance, debt, dueAt, nil
}

func settleBankDebtLocked(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	balance, debt float64,
	dueAt sql.NullTime,
	policy BankPolicy,
	now time.Time,
) (bool, error) {
	if debt <= ledgerAmountEpsilon {
		return false, nil
	}
	if !dueAt.Valid || dueAt.Time.After(now) {
		return false, nil
	}
	permanentAmount, err := normalizeLedgerAmount(debt * policy.DebtConversionRatio)
	if err != nil || permanentAmount <= 0 {
		return false, ErrBankPolicyInvalid
	}
	var loanID sql.NullInt64
	err = tx.QueryRowContext(ctx, `
UPDATE bank_loans
SET debt_remaining = 0,
    status = 'settled',
    settled_at = $1,
    settlement_permanent_amount = $2,
    updated_at = $1
WHERE user_id = $3 AND status = 'active'
RETURNING id`, now, formatLedgerAmount(permanentAmount), userID).Scan(&loanID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("settle active bank loan: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
UPDATE users
SET balance = balance - $1,
    temporary_credit_debt = 0,
    temporary_credit_debt_due_at = NULL,
    updated_at = $2
WHERE id = $3 AND deleted_at IS NULL`, formatLedgerAmount(permanentAmount), now, userID); err != nil {
		return false, fmt.Errorf("settle bank debt from permanent balance: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_ledger
    (user_id, operation, loan_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after, metadata)
VALUES ($1, 'permanent_settlement', $2, $3, 0, $4, $5, 0, $6)`,
		userID,
		nullableBankLedgerInt64(loanID),
		formatLedgerAmount(-permanentAmount),
		formatLedgerAmount(-debt),
		formatLedgerAmount(debt),
		marshalBankMetadata(map[string]any{
			"conversion_ratio": formatLedgerAmount(policy.DebtConversionRatio),
			"balance_before":   formatLedgerAmount(balance),
			"balance_after":    formatLedgerAmount(balance - permanentAmount),
		}),
	); err != nil {
		return false, fmt.Errorf("record bank debt settlement: %w", err)
	}
	return true, nil
}

func loadActiveBankAdvance(ctx context.Context, q bankQueryer, userID int64) (*BankAdvanceStatus, error) {
	var item BankAdvanceStatus
	var principal, debt float64
	err := q.QueryRowContext(ctx, `
SELECT id, principal, debt_remaining, status, granted_at, grant_expires_at, settlement_due_at
FROM bank_loans
WHERE user_id = $1 AND status = 'active'`, userID).Scan(
		&item.ID,
		&principal,
		&debt,
		&item.Status,
		&item.GrantedAt,
		&item.GrantExpiresAt,
		&item.SettlementDueAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load active bank advance: %w", err)
	}
	item.Principal = formatLedgerAmount(principal)
	item.DebtRemaining = formatLedgerAmount(debt)
	item.GrantedAt = item.GrantedAt.UTC()
	item.GrantExpiresAt = item.GrantExpiresAt.UTC()
	item.SettlementDueAt = item.SettlementDueAt.UTC()
	return &item, nil
}

func loadBankLedger(ctx context.Context, q bankQueryer, userID int64, limit int) ([]BankLedgerItem, error) {
	if limit < 1 || limit > bankLedgerPageSize {
		limit = bankLedgerPageSize
	}
	rows, err := q.QueryContext(ctx, `
SELECT id, operation, loan_id, grant_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after, created_at
FROM bank_ledger
WHERE user_id = $1
ORDER BY created_at DESC, id DESC
LIMIT $2`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("load bank ledger: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]BankLedgerItem, 0)
	for rows.Next() {
		var item BankLedgerItem
		var loanID, grantID sql.NullInt64
		var permanent, temporary, debtDelta, debtBefore, debtAfter float64
		if err := rows.Scan(
			&item.ID,
			&item.Operation,
			&loanID,
			&grantID,
			&permanent,
			&temporary,
			&debtDelta,
			&debtBefore,
			&debtAfter,
			&item.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan bank ledger: %w", err)
		}
		if loanID.Valid {
			id := loanID.Int64
			item.LoanID = &id
		}
		if grantID.Valid {
			id := grantID.Int64
			item.GrantID = &id
		}
		item.PermanentDelta = formatLedgerAmount(permanent)
		item.TemporaryDelta = formatLedgerAmount(temporary)
		item.DebtDelta = formatLedgerAmount(debtDelta)
		item.DebtBefore = formatLedgerAmount(debtBefore)
		item.DebtAfter = formatLedgerAmount(debtAfter)
		item.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bank ledger: %w", err)
	}
	return items, nil
}

func (s *BankService) invalidateBankCredits(ctx context.Context, userID int64, permanentChanged bool) {
	if s == nil {
		return
	}
	if permanentChanged && s.billingCache != nil {
		_ = s.billingCache.InvalidateUserBalance(ctx, userID)
	}
	if invalidator, ok := s.billingCache.(AvailableCreditInvalidator); ok {
		_ = invalidator.InvalidateAvailableCredit(ctx, userID)
	}
}
