package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"
)

// ApplyTemporaryCreditDebtOffsetTx applies a newly granted temporary-credit
// amount to the user's outstanding bank debt before the grant can be spent.
// The caller must already own the surrounding transaction.  The grant itself
// is inserted by the caller before this function runs, so a failed offset
// rolls back both writes together.
func ApplyTemporaryCreditDebtOffsetTx(
	ctx context.Context,
	tx *sql.Tx,
	userID, grantID int64,
	grantAmount float64,
) (remainingAmount, offsetAmount float64, err error) {
	if tx == nil {
		return 0, 0, ErrTemporaryCreditTransactionRequired
	}
	if userID <= 0 || grantID <= 0 {
		return 0, 0, errors.New("temporary credit debt offset identifiers must be positive")
	}
	if err := ValidateTemporaryCreditAmount(grantAmount); err != nil {
		return 0, 0, err
	}
	grantAmount, _ = normalizeLedgerAmount(grantAmount)

	balance, debt, dueAt, err := lockBankUser(ctx, tx, userID)
	if err != nil {
		return 0, 0, err
	}
	debt, err = normalizeDerivedLedgerAmount(debt)
	if err != nil || debt <= ledgerAmountEpsilon {
		return grantAmount, 0, nil
	}
	if dueAt.Valid {
		var databaseNow time.Time
		if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); err != nil {
			return 0, 0, fmt.Errorf("sample temporary credit debt offset clock: %w", err)
		}
		if !dueAt.Time.After(databaseNow) {
			policy, err := loadBankPolicy(ctx, tx)
			if err != nil {
				return 0, 0, err
			}
			settled, err := settleBankDebtLocked(ctx, tx, userID, balance, debt, dueAt, policy, databaseNow)
			if err != nil {
				return 0, 0, err
			}
			if settled {
				return grantAmount, 0, nil
			}
		}
	}

	offsetAmount = math.Min(debt, grantAmount)
	offsetAmount, _ = normalizeLedgerAmount(offsetAmount)
	if offsetAmount <= ledgerAmountEpsilon {
		return grantAmount, 0, nil
	}

	if err := tx.QueryRowContext(ctx, `
UPDATE temporary_credit_grants
SET remaining_amount = remaining_amount - $1,
    updated_at = clock_timestamp()
WHERE id = $2
  AND user_id = $3
  AND remaining_amount >= $1
RETURNING remaining_amount`, formatLedgerAmount(offsetAmount), grantID, userID).Scan(&remainingAmount); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, fmt.Errorf("temporary credit grant %d changed before debt offset", grantID)
		}
		return 0, 0, fmt.Errorf("offset temporary credit grant %d: %w", grantID, err)
	}
	remainingAmount, err = normalizeDerivedLedgerAmount(remainingAmount)
	if err != nil || remainingAmount < 0 {
		return 0, 0, fmt.Errorf("invalid remaining temporary credit after debt offset")
	}

	var debtAfter float64
	if err := tx.QueryRowContext(ctx, `
UPDATE users
SET temporary_credit_debt = temporary_credit_debt - $1,
    temporary_credit_debt_due_at = CASE
        WHEN temporary_credit_debt - $1 <= 0 THEN NULL
        ELSE temporary_credit_debt_due_at
    END,
    updated_at = clock_timestamp()
WHERE id = $2 AND deleted_at IS NULL
RETURNING temporary_credit_debt`, formatLedgerAmount(offsetAmount), userID).Scan(&debtAfter); err != nil {
		return 0, 0, fmt.Errorf("update temporary credit debt: %w", err)
	}
	debtAfter, err = normalizeDerivedLedgerAmount(debtAfter)
	if err != nil || debtAfter < 0 {
		return 0, 0, fmt.Errorf("invalid temporary credit debt after offset")
	}

	var loanID sql.NullInt64
	loanErr := tx.QueryRowContext(ctx, `
UPDATE bank_loans
SET debt_remaining = GREATEST(debt_remaining - $1, 0),
    status = CASE WHEN debt_remaining - $1 <= 0 THEN 'repaid' ELSE status END,
    settled_at = CASE WHEN debt_remaining - $1 <= 0 THEN clock_timestamp() ELSE settled_at END,
    updated_at = clock_timestamp()
WHERE user_id = $2 AND status = 'active'
RETURNING id`, formatLedgerAmount(offsetAmount), userID).Scan(&loanID)
	if loanErr != nil && !errors.Is(loanErr, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("update bank loan after debt offset: %w", loanErr)
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO bank_ledger
    (user_id, operation, loan_id, grant_id, permanent_delta, temporary_delta, debt_delta, debt_before, debt_after)
VALUES ($1, 'debt_offset', $2, $3, 0, $4, $5, $6, $7)`,
		userID,
		nullableBankLedgerInt64(loanID),
		grantID,
		formatLedgerAmount(-offsetAmount),
		formatLedgerAmount(-offsetAmount),
		formatLedgerAmount(debt),
		formatLedgerAmount(debtAfter),
	); err != nil {
		return 0, 0, fmt.Errorf("record bank debt offset: %w", err)
	}

	return remainingAmount, offsetAmount, nil
}

func nullableBankLedgerInt64(value sql.NullInt64) any {
	if !value.Valid {
		return nil
	}
	return value.Int64
}
