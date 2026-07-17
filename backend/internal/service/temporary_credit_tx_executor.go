package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
)

// TemporaryCreditSQLExecutor is the transaction-bound subset shared by
// database/sql and Ent transaction clients. Allocation never starts or ends a
// transaction itself.
type TemporaryCreditSQLExecutor interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type TemporaryCreditAllocationExecutor struct{}

func NewTemporaryCreditAllocationExecutor() *TemporaryCreditAllocationExecutor {
	return &TemporaryCreditAllocationExecutor{}
}

// Allocate consumes unexpired grants in FEFO order. A consumption row is
// written only after its conditional grant update returned a deducted amount.
func (e *TemporaryCreditAllocationExecutor) Allocate(ctx context.Context, tx TemporaryCreditSQLExecutor, userID int64, amount float64, reference TemporaryCreditConsumptionReference) (float64, error) {
	reference, err := normalizeTemporaryCreditReference(reference)
	if err != nil {
		return 0, err
	}
	if tx == nil {
		return 0, ErrTemporaryCreditTransactionRequired
	}
	if userID <= 0 {
		return 0, fmt.Errorf("temporary credit user id must be positive")
	}
	if err := ValidateTemporaryCreditAmount(amount); err != nil {
		return 0, err
	}
	amount, _ = normalizeLedgerAmount(amount)

	rows, err := tx.QueryContext(ctx, `
SELECT id, remaining_amount
FROM temporary_credit_grants
WHERE user_id = $1 AND remaining_amount > 0 AND expires_at > clock_timestamp()
ORDER BY expires_at ASC, id ASC
FOR UPDATE`, userID)
	if err != nil {
		return 0, fmt.Errorf("query FEFO temporary credit grants: %w", err)
	}

	type candidate struct {
		id        int64
		available float64
	}
	candidates := make([]candidate, 0)
	for rows.Next() {
		var grantID int64
		var available float64
		if err := rows.Scan(&grantID, &available); err != nil {
			_ = rows.Close()
			return 0, fmt.Errorf("scan FEFO temporary credit grant: %w", err)
		}
		available, err = normalizeLedgerAmount(available)
		if err != nil || available <= 0 {
			_ = rows.Close()
			return 0, fmt.Errorf("invalid FEFO temporary credit grant %d amount", grantID)
		}
		candidates = append(candidates, candidate{id: grantID, available: available})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, fmt.Errorf("iterate FEFO temporary credit grants: %w", err)
	}
	if err := rows.Close(); err != nil {
		return 0, fmt.Errorf("close FEFO temporary credit grant rows: %w", err)
	}

	remaining := amount
	for _, candidate := range candidates {
		if remaining <= ledgerAmountEpsilon {
			remaining = 0
			break
		}
		portion := math.Min(candidate.available, remaining)
		portion, _ = normalizeLedgerAmount(portion)
		if portion <= 0 {
			continue
		}
		deducted, updated, err := updateTemporaryCreditGrant(ctx, tx, candidate.id, portion)
		if err != nil {
			return 0, err
		}
		if !updated {
			continue
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO temporary_credit_consumptions (grant_id, usage_log_id, request_id, amount)
VALUES ($1, $2, $3, $4)`, candidate.id, nullableTemporaryCreditInt64(reference.UsageLogID), nullableTemporaryCreditString(reference.RequestID), formatLedgerAmount(deducted)); err != nil {
			return 0, fmt.Errorf("insert FEFO temporary credit consumption for grant %d: %w", candidate.id, err)
		}
		remaining, err = normalizeLedgerAmount(remaining - deducted)
		if err != nil {
			return 0, fmt.Errorf("normalize FEFO temporary credit remainder: %w", err)
		}
	}
	return remaining, nil
}

func updateTemporaryCreditGrant(ctx context.Context, tx TemporaryCreditSQLExecutor, grantID int64, portion float64) (float64, bool, error) {
	rows, err := tx.QueryContext(ctx, `
UPDATE temporary_credit_grants
SET remaining_amount = remaining_amount - $1,
    updated_at = clock_timestamp()
WHERE id = $2 AND remaining_amount >= $1 AND expires_at > clock_timestamp()
RETURNING $1::numeric`, formatLedgerAmount(portion), grantID)
	if err != nil {
		return 0, false, fmt.Errorf("update FEFO temporary credit grant %d: %w", grantID, err)
	}
	deferredClose := func() error {
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close FEFO temporary credit update rows: %w", err)
		}
		return nil
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return 0, false, fmt.Errorf("iterate FEFO temporary credit update rows: %w", err)
		}
		if err := deferredClose(); err != nil {
			return 0, false, err
		}
		return 0, false, nil
	}
	var deducted float64
	if err := rows.Scan(&deducted); err != nil {
		_ = rows.Close()
		return 0, false, fmt.Errorf("scan FEFO temporary credit deduction: %w", err)
	}
	deducted, err = normalizeLedgerAmount(deducted)
	if err != nil || deducted <= 0 {
		_ = rows.Close()
		return 0, false, fmt.Errorf("invalid FEFO temporary credit deduction for grant %d", grantID)
	}
	if rows.Next() {
		_ = rows.Close()
		return 0, false, errors.New("unexpected multiple FEFO temporary credit deductions")
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return 0, false, fmt.Errorf("iterate FEFO temporary credit update rows: %w", err)
	}
	if err := deferredClose(); err != nil {
		return 0, false, err
	}
	return deducted, true, nil
}

func nullableTemporaryCreditInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableTemporaryCreditString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
