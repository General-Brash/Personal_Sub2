package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrBankAutoExchangeSettingsInvalid     = infraerrors.BadRequest("INVALID_BANK_AUTO_EXCHANGE_SETTINGS", "bank auto exchange settings are invalid")
	ErrBankAutoExchangeDepositInsufficient = infraerrors.Conflict("BANK_AUTO_EXCHANGE_DEPOSIT_INSUFFICIENT", "spendable permanent balance is insufficient for the auto exchange deposit")
)

type BankAutoExchangePreference struct {
	Enabled        bool   `json:"enabled"`
	Threshold      string `json:"threshold"`
	ExchangeAmount string `json:"exchange_amount"`
	FrozenDeposit  string `json:"frozen_deposit"`
}

func defaultBankAutoExchangePreference() BankAutoExchangePreference {
	return BankAutoExchangePreference{
		Threshold: "1.00000000", ExchangeAmount: "1.00000000", FrozenDeposit: "0.00000000",
	}
}

func validateBankAutoExchangeSettings(threshold, amount float64) error {
	if normalized, err := normalizeLedgerAmount(threshold); err != nil || normalized != threshold || threshold < 0 || threshold > 5 {
		return ErrBankAutoExchangeSettingsInvalid
	}
	if normalized, err := normalizeLedgerAmount(amount); err != nil || normalized != amount || amount < 0.5 || amount > 5 {
		return ErrBankAutoExchangeSettingsInvalid
	}
	return nil
}

// GetAutoExchangePreference returns defaults for an absent row, not an absent
// migration. Reading preferences never exchanges credits or settles debt.
func (s *BankService) GetAutoExchangePreference(ctx context.Context, userID int64) (*BankAutoExchangePreference, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("bank service database is nil")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	var preference BankAutoExchangePreference
	err := scanBankAutoExchangeRow(ctx, s.db, `
SELECT COALESCE(preference.enabled, FALSE),
       COALESCE(preference.threshold, 1)::text,
       COALESCE(preference.exchange_amount, 1)::text,
       COALESCE(preference.frozen_deposit, 0)::text
FROM users AS bank_user
LEFT JOIN bank_auto_exchange_preferences AS preference ON preference.user_id = bank_user.id
WHERE bank_user.id = $1 AND bank_user.deleted_at IS NULL`, []any{userID},
		&preference.Enabled, &preference.Threshold, &preference.ExchangeAmount, &preference.FrozenDeposit)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load bank auto exchange preference: %w", err)
	}
	if _, _, _, err := parseBankAutoExchangePreference(&preference); err != nil {
		return nil, err
	}
	return &preference, nil
}

func (s *BankService) UpdateAutoExchangePreferenceAtomic(
	ctx context.Context,
	userID int64,
	enabled bool,
	threshold, exchangeAmount float64,
	claim *IdempotencyAtomicClaim,
) (*BankAutoExchangePreference, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("bank service database is nil")
	}
	if userID <= 0 {
		return nil, ErrUserNotFound
	}
	if claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if err := validateBankAutoExchangeSettings(threshold, exchangeAmount); err != nil {
		return nil, err
	}
	// Do not settle debt here: disabling must release only this feature's hold,
	// including when spendable balance is negative.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin bank auto exchange preference transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	balance, frozen, debt, err := lockBankAutoExchangeUser(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	previous, err := loadBankAutoExchangePreferenceLocked(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	_, _, oldDeposit, err := parseBankAutoExchangePreference(&previous)
	if err != nil {
		return nil, err
	}
	if frozen < oldDeposit {
		return nil, errors.New("bank auto exchange deposit exceeds aggregate frozen balance")
	}
	deposit := float64(0)
	if enabled {
		deposit, _ = normalizeLedgerAmount(2 * exchangeAmount)
	}
	delta, _ := normalizeLedgerAmount(deposit - oldDeposit)
	if delta > 0 && balance < delta {
		return nil, ErrBankAutoExchangeDepositInsufficient
	}
	if delta != 0 {
		var balanceAfterRaw, frozenAfterRaw string
		err := scanBankAutoExchangeRow(ctx, tx, `
UPDATE users
SET balance = balance - $1::numeric,
    frozen_balance = frozen_balance + $1::numeric,
    updated_at = clock_timestamp()
WHERE id = $2 AND deleted_at IS NULL
  AND ($1::numeric <= 0 OR balance >= $1::numeric)
  AND frozen_balance + $1::numeric >= 0
RETURNING balance::text, frozen_balance::text`, []any{formatLedgerAmount(delta), userID}, &balanceAfterRaw, &frozenAfterRaw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrBankAutoExchangeDepositInsufficient
		}
		if err != nil {
			return nil, fmt.Errorf("transfer bank auto exchange deposit: %w", err)
		}
		balanceAfter, err := ParseStrictSignedLedgerAmount(balanceAfterRaw)
		if err != nil {
			return nil, fmt.Errorf("parse balance after bank deposit transfer: %w", err)
		}
		frozenAfter, err := ParseStrictLedgerAmount(frozenAfterRaw)
		if err != nil {
			return nil, fmt.Errorf("parse frozen balance after bank deposit transfer: %w", err)
		}
		operation := "auto_exchange_deposit_freeze"
		if delta < 0 {
			operation = "auto_exchange_deposit_release"
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_ledger
    (user_id, operation, actor_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after,
     permanent_balance_before, permanent_balance_after, metadata)
VALUES ($1, $2, $1, 0, 0, 0, $3, $3, $4, $5, $6)`, userID, operation,
			formatLedgerAmount(debt), formatLedgerAmount(balance), formatLedgerAmount(balanceAfter),
			marshalBankMetadata(map[string]any{
				"auto_exchange":                true,
				"spendable_permanent_delta":    formatLedgerAmount(-delta),
				"frozen_balance_delta":         formatLedgerAmount(delta),
				"frozen_balance_before":        formatLedgerAmount(frozen),
				"frozen_balance_after":         formatLedgerAmount(frozenAfter),
				"frozen_deposit_before":        formatLedgerAmount(oldDeposit),
				"frozen_deposit_after":         formatLedgerAmount(deposit),
				"total_permanent_assets_delta": formatLedgerAmount(0),
			})); err != nil {
			return nil, fmt.Errorf("record bank auto exchange deposit ledger: %w", err)
		}
	}
	preference := &BankAutoExchangePreference{
		Enabled: enabled, Threshold: formatLedgerAmount(threshold),
		ExchangeAmount: formatLedgerAmount(exchangeAmount), FrozenDeposit: formatLedgerAmount(deposit),
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_auto_exchange_preferences (user_id, enabled, threshold, exchange_amount, frozen_deposit)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id) DO UPDATE
SET enabled = EXCLUDED.enabled, threshold = EXCLUDED.threshold,
    exchange_amount = EXCLUDED.exchange_amount, frozen_deposit = EXCLUDED.frozen_deposit,
    updated_at = clock_timestamp()`, userID, enabled, preference.Threshold, preference.ExchangeAmount, preference.FrozenDeposit); err != nil {
		return nil, fmt.Errorf("save bank auto exchange preference: %w", err)
	}
	if err := claim.PersistSuccess(ctx, tx, preference); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit bank auto exchange preference transaction: %w", err)
	}
	if delta != 0 {
		s.invalidateBankCredits(ctx, userID, true)
	}
	return preference, nil
}

type BankAutoExchangeTxResult struct {
	Exchanged        bool
	Settled          bool
	PermanentSpent   float64
	TemporaryGranted float64
	PermanentBalance float64
}

// BankAutoExchangeInTx joins the caller's transaction. The caller must roll
// back on error and invalidate balance/available-credit caches after commit.
func BankAutoExchangeInTx(ctx context.Context, tx TemporaryCreditSQLExecutor, userID int64) (BankAutoExchangeTxResult, error) {
	result := BankAutoExchangeTxResult{}
	if tx == nil {
		return result, ErrTemporaryCreditTransactionRequired
	}
	if userID <= 0 {
		return result, ErrUserNotFound
	}
	balance, frozen, debt, err := lockBankAutoExchangeUser(ctx, tx, userID)
	if err != nil {
		return result, err
	}
	result.PermanentBalance = balance
	preference, err := loadBankAutoExchangePreferenceLocked(ctx, tx, userID)
	if err != nil {
		return result, err
	}
	threshold, amount, deposit, err := parseBankAutoExchangePreference(&preference)
	if err != nil {
		return result, err
	}
	if !preference.Enabled {
		return result, nil
	}
	if frozen < deposit {
		return result, errors.New("bank auto exchange deposit exceeds aggregate frozen balance")
	}
	var businessNow time.Time
	if err := scanBankAutoExchangeRow(ctx, tx, `SELECT clock_timestamp()`, nil, &businessNow); err != nil {
		return result, fmt.Errorf("sample bank auto exchange clock: %w", err)
	}
	policy, err := loadBankPolicy(ctx, tx)
	if err != nil {
		return result, err
	}
	var dueAt sql.NullTime
	if err := scanBankAutoExchangeRow(ctx, tx, `SELECT temporary_credit_debt_due_at FROM users WHERE id = $1`, []any{userID}, &dueAt); err != nil {
		return result, fmt.Errorf("load bank auto exchange debt due date: %w", err)
	}
	// Reconcile again under the caller's lock: debt can become due after preflight.
	_, unusedProcessed, forcedSettled, err := reconcileBankDebtLocked(ctx, tx, userID, balance, debt, dueAt, policy, businessNow)
	if err != nil {
		return result, err
	}
	result.Settled = unusedProcessed || forcedSettled
	if result.Settled {
		balance, _, debt, err = lockBankAutoExchangeUser(ctx, tx, userID)
		if err != nil {
			return result, err
		}
		result.PermanentBalance = balance
	}
	available, err := bankAutoExchangeTemporaryAvailable(ctx, tx, userID, businessNow, true)
	if err != nil {
		return result, err
	}
	// Equality does not trigger a positive threshold. After triggering, equality
	// does require another full exchange, until available is strictly greater.
	if (threshold > 0 && available >= threshold) || (threshold == 0 && available > 0) || balance < amount {
		return result, nil
	}
	expiresAt, err := nextTemporaryCreditExpiry(businessNow)
	if err != nil {
		return result, err
	}
	localDate := businessNow.In(beijingLocation).Format("2006-01-02")
	var dailyRaw string
	err = scanBankAutoExchangeRow(ctx, tx, `
SELECT permanent_exchanged::text FROM bank_exchange_daily_usage
WHERE user_id = $1 AND usage_date = $2::date FOR UPDATE`, []any{userID, localDate}, &dailyRaw)
	dailyBefore := float64(0)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("load bank auto exchange daily usage: %w", err)
	}
	if err == nil {
		dailyBefore, err = ParseStrictLedgerAmount(dailyRaw)
		if err != nil {
			return result, fmt.Errorf("parse bank auto exchange daily usage: %w", err)
		}
	}
	for available <= threshold && result.PermanentBalance >= amount {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		temporaryAmount, allocations, err := calculateBankTieredExchange(policy.ExchangeTiers, dailyBefore, amount)
		if err != nil {
			return result, err
		}
		if temporaryAmount <= 0 {
			return result, errors.New("bank auto exchange would grant no temporary credit")
		}
		var balanceAfterRaw string
		if err := scanBankAutoExchangeRow(ctx, tx, `
UPDATE users SET balance = balance - $1::numeric, updated_at = clock_timestamp()
WHERE id = $2 AND deleted_at IS NULL AND balance >= $1::numeric
RETURNING balance::text`, []any{formatLedgerAmount(amount), userID}, &balanceAfterRaw); err != nil {
			return result, fmt.Errorf("deduct permanent balance for bank auto exchange: %w", err)
		}
		balanceAfter, err := ParseStrictLedgerAmount(balanceAfterRaw)
		if err != nil || balanceAfter >= result.PermanentBalance {
			return result, errors.New("bank auto exchange did not reduce spendable balance")
		}
		var grantID int64
		if err := scanBankAutoExchangeRow(ctx, tx, `
INSERT INTO temporary_credit_grants
    (user_id, source, amount, remaining_amount, available_at, expires_at, notes)
VALUES ($1, $2, $3, $3, $4, $5, '') RETURNING id`,
			[]any{userID, TemporaryCreditSourceBankExchange, formatLedgerAmount(temporaryAmount), businessNow, expiresAt}, &grantID); err != nil {
			return result, fmt.Errorf("create bank auto exchange grant: %w", err)
		}
		var dailyAfterRaw string
		if err := scanBankAutoExchangeRow(ctx, tx, `
INSERT INTO bank_exchange_daily_usage (user_id, usage_date, permanent_exchanged, created_at, updated_at)
VALUES ($1, $2::date, $3, clock_timestamp(), clock_timestamp())
ON CONFLICT (user_id, usage_date) DO UPDATE
SET permanent_exchanged = bank_exchange_daily_usage.permanent_exchanged + EXCLUDED.permanent_exchanged,
    updated_at = clock_timestamp()
RETURNING permanent_exchanged::text`, []any{userID, localDate, formatLedgerAmount(amount)}, &dailyAfterRaw); err != nil {
			return result, fmt.Errorf("increment bank auto exchange daily usage: %w", err)
		}
		dailyAfter, err := ParseStrictLedgerAmount(dailyAfterRaw)
		if err != nil || dailyAfter <= dailyBefore {
			return result, errors.New("bank auto exchange did not advance daily usage")
		}
		availableAfter, err := bankAutoExchangeTemporaryAvailable(ctx, tx, userID, businessNow, false)
		if err != nil {
			return result, err
		}
		if availableAfter <= available {
			return result, errors.New("bank auto exchange did not increase available temporary credit")
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_ledger
    (user_id, operation, grant_id, actor_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after, metadata)
VALUES ($1, 'exchange', $2, $1, $3, $4, 0, $5, $5, $6)`, userID, grantID,
			formatLedgerAmount(-amount), formatLedgerAmount(temporaryAmount), formatLedgerAmount(debt),
			marshalBankMetadata(map[string]any{
				"auto_exchange": true, "threshold": preference.Threshold,
				"exchange_amount": preference.ExchangeAmount, "frozen_deposit": preference.FrozenDeposit,
				"exchange_rate":  formatLedgerAmount(policy.ExchangeRate),
				"exchange_tiers": bankExchangeTierDTOs(policy.ExchangeTiers), "tier_allocations": allocations,
				"exchange_local_date": localDate, "daily_permanent_before": formatLedgerAmount(dailyBefore),
				"daily_permanent_after": formatLedgerAmount(dailyAfter),
			})); err != nil {
			return result, fmt.Errorf("record bank auto exchange ledger: %w", err)
		}
		result.Exchanged = true
		result.PermanentSpent, err = normalizeLedgerAmount(result.PermanentSpent + amount)
		if err != nil {
			return result, err
		}
		result.TemporaryGranted, err = normalizeLedgerAmount(result.TemporaryGranted + temporaryAmount)
		if err != nil {
			return result, err
		}
		result.PermanentBalance = balanceAfter
		available, dailyBefore = availableAfter, dailyAfter
	}
	return result, nil
}

func (s *BankService) AutoExchangeIfNeeded(ctx context.Context, userID int64) error {
	if s == nil || s.db == nil {
		return errors.New("bank service database is nil")
	}
	if userID <= 0 {
		return ErrUserNotFound
	}
	if err := s.SettleDueForUser(ctx, userID); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin bank auto exchange transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	result, err := BankAutoExchangeInTx(ctx, tx, userID)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit bank auto exchange transaction: %w", err)
	}
	if result.Exchanged || result.Settled {
		s.invalidateBankCredits(ctx, userID, true)
	}
	return nil
}

func loadBankAutoExchangePreferenceLocked(ctx context.Context, tx TemporaryCreditSQLExecutor, userID int64) (BankAutoExchangePreference, error) {
	preference := defaultBankAutoExchangePreference()
	err := scanBankAutoExchangeRow(ctx, tx, `
SELECT enabled, threshold::text, exchange_amount::text, frozen_deposit::text
FROM bank_auto_exchange_preferences WHERE user_id = $1 FOR UPDATE`, []any{userID},
		&preference.Enabled, &preference.Threshold, &preference.ExchangeAmount, &preference.FrozenDeposit)
	if errors.Is(err, sql.ErrNoRows) {
		return preference, nil
	}
	if err != nil {
		return preference, fmt.Errorf("lock bank auto exchange preference: %w", err)
	}
	return preference, nil
}

func parseBankAutoExchangePreference(preference *BankAutoExchangePreference) (threshold, amount, deposit float64, err error) {
	threshold, err = ParseStrictLedgerAmount(preference.Threshold)
	if err != nil {
		return 0, 0, 0, ErrBankAutoExchangeSettingsInvalid
	}
	amount, err = ParseStrictLedgerAmount(preference.ExchangeAmount)
	if err != nil || validateBankAutoExchangeSettings(threshold, amount) != nil {
		return 0, 0, 0, ErrBankAutoExchangeSettingsInvalid
	}
	deposit, err = ParseStrictLedgerAmount(preference.FrozenDeposit)
	expectedDeposit := float64(0)
	if preference.Enabled {
		expectedDeposit, _ = normalizeLedgerAmount(2 * amount)
	}
	if err != nil || deposit != expectedDeposit {
		return 0, 0, 0, errors.New("bank auto exchange preference has an invalid frozen deposit")
	}
	preference.Threshold = formatLedgerAmount(threshold)
	preference.ExchangeAmount = formatLedgerAmount(amount)
	preference.FrozenDeposit = formatLedgerAmount(deposit)
	return threshold, amount, deposit, nil
}

func lockBankAutoExchangeUser(ctx context.Context, tx TemporaryCreditSQLExecutor, userID int64) (balance, frozen, debt float64, err error) {
	var balanceRaw, frozenRaw, debtRaw string
	err = scanBankAutoExchangeRow(ctx, tx, `
SELECT balance::text, frozen_balance::text, temporary_credit_debt::text
FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, []any{userID}, &balanceRaw, &frozenRaw, &debtRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, ErrUserNotFound
	}
	if err != nil {
		return 0, 0, 0, fmt.Errorf("lock bank auto exchange user: %w", err)
	}
	balance, err = ParseStrictSignedLedgerAmount(balanceRaw)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse bank auto exchange balance: %w", err)
	}
	frozen, err = ParseStrictLedgerAmount(frozenRaw)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse bank auto exchange frozen balance: %w", err)
	}
	debt, err = ParseStrictLedgerAmount(debtRaw)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("parse bank auto exchange debt: %w", err)
	}
	return balance, frozen, debt, nil
}

func bankAutoExchangeTemporaryAvailable(ctx context.Context, tx TemporaryCreditSQLExecutor, userID int64, now time.Time, forUpdate bool) (float64, error) {
	query := `
SELECT COALESCE(SUM(remaining_amount), 0)::text FROM (
    SELECT remaining_amount FROM temporary_credit_grants
    WHERE user_id = $1 AND remaining_amount > 0 AND available_at <= $2 AND expires_at > $2
    ORDER BY expires_at, id`
	if forUpdate {
		query += " FOR UPDATE"
	}
	query += ") AS available_grants"
	var raw string
	if err := scanBankAutoExchangeRow(ctx, tx, query, []any{userID, now}, &raw); err != nil {
		return 0, fmt.Errorf("load bank auto exchange temporary credit: %w", err)
	}
	available, err := ParseStrictLedgerAmount(raw)
	if err != nil {
		return 0, fmt.Errorf("parse bank auto exchange temporary credit: %w", err)
	}
	return available, nil
}

// Fully drain and close a single-row result before issuing another query on
// the transaction; Ent's adapter does not expose QueryRowContext.
func scanBankAutoExchangeRow(ctx context.Context, tx TemporaryCreditSQLExecutor, query string, args []any, destinations ...any) (err error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rows.Close(); err == nil {
			err = closeErr
		}
	}()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(destinations...); err != nil {
		return err
	}
	if rows.Next() {
		return errors.New("bank auto exchange query returned multiple rows")
	}
	return rows.Err()
}
