package repository

import (
	"context"
	"database/sql"
	"fmt"
	"math"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AffiliateRebateOutboxRepository = (*affiliateRepository)(nil)

func (r *affiliateRepository) EnqueueAffiliateRebateJob(ctx context.Context, input service.AffiliateRebateJobInput) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.ExecContext(ctx, `
INSERT INTO affiliate_rebate_jobs (
    invitee_user_id,
    source_redeem_code_id,
    source_kind,
    base_amount,
    status,
    next_retry_at,
    created_at,
    updated_at
)
VALUES ($1, $2, $3, $4, 'pending', NOW(), NOW(), NOW())
ON CONFLICT (source_redeem_code_id) DO NOTHING`,
		input.InviteeUserID,
		input.SourceRedeemCodeID,
		input.SourceKind,
		normalizeAffiliateLedgerAmount(input.BaseAmount),
	)
	if err != nil {
		return fmt.Errorf("enqueue affiliate rebate job: %w", err)
	}
	return nil
}

func (r *affiliateRepository) AccrueQuotaCapped(
	ctx context.Context,
	inviterID int64,
	inviteeUserID int64,
	requestedAmount float64,
	perInviteeCap float64,
	freezeHours int,
	sourceOrderID *int64,
	sourceRedeemCodeID *int64,
) (service.AffiliateAccrualResult, error) {
	requestedAmount = normalizeAffiliateLedgerAmount(requestedAmount)
	perInviteeCap = normalizeAffiliateLedgerAmount(perInviteeCap)
	if requestedAmount <= 0 {
		return service.AffiliateAccrualResult{}, nil
	}

	var result service.AffiliateAccrualResult
	err := r.withTx(ctx, func(txCtx context.Context, txClient *dbent.Client) error {
		if _, err := ensureUserAffiliateWithClient(txCtx, txClient, inviterID); err != nil {
			return err
		}

		// The inviter row is the serialization point for every accrual. The cap
		// is computed only after this lock, so concurrent invitee jobs cannot
		// both consume the same remaining allowance.
		var lockedInviterID int64
		if err := scanAffiliateSingleRow(txCtx, txClient,
			`SELECT user_id FROM user_affiliates WHERE user_id = $1 FOR UPDATE`,
			[]any{inviterID}, &lockedInviterID); err != nil {
			return fmt.Errorf("lock inviter affiliate: %w", err)
		}

		if sourceRedeemCodeID != nil {
			var existingAmount float64
			err := scanAffiliateSingleRow(txCtx, txClient, `
SELECT amount::double precision
FROM user_affiliate_ledger
WHERE action = 'accrue'
  AND source_redeem_code_id = $1
LIMIT 1`, []any{*sourceRedeemCodeID}, &existingAmount)
			if err == nil {
				result = service.AffiliateAccrualResult{
					Amount:    normalizeAffiliateLedgerAmount(existingAmount),
					Duplicate: true,
				}
				return nil
			}
			if err != sql.ErrNoRows {
				return fmt.Errorf("query affiliate redeem dedup: %w", err)
			}
		}

		amount := requestedAmount
		if perInviteeCap > 0 {
			var existing float64
			if err := scanAffiliateSingleRow(txCtx, txClient, `
SELECT COALESCE(SUM(amount), 0)::double precision
FROM user_affiliate_ledger
WHERE user_id = $1
  AND source_user_id = $2
  AND action = 'accrue'`, []any{inviterID, inviteeUserID}, &existing); err != nil {
				return fmt.Errorf("query affiliate cap usage: %w", err)
			}
			remaining := normalizeAffiliateLedgerAmount(perInviteeCap - existing)
			if remaining <= 0 {
				return nil
			}
			if amount > remaining {
				amount = remaining
			}
		}
		amount = normalizeAffiliateLedgerAmount(amount)
		if amount <= 0 {
			return nil
		}

		inserted, err := insertAffiliateAccrualLedger(
			txCtx,
			txClient,
			inviterID,
			inviteeUserID,
			amount,
			freezeHours,
			sourceOrderID,
			sourceRedeemCodeID,
		)
		if err != nil {
			return err
		}
		if !inserted {
			var existingAmount float64
			if sourceRedeemCodeID == nil {
				return fmt.Errorf("affiliate ledger insert was not applied")
			}
			if err := scanAffiliateSingleRow(txCtx, txClient, `
SELECT amount::double precision
FROM user_affiliate_ledger
WHERE action = 'accrue'
  AND source_redeem_code_id = $1
LIMIT 1`, []any{*sourceRedeemCodeID}, &existingAmount); err != nil {
				return fmt.Errorf("load duplicate affiliate ledger: %w", err)
			}
			result = service.AffiliateAccrualResult{
				Amount:    normalizeAffiliateLedgerAmount(existingAmount),
				Duplicate: true,
			}
			return nil
		}

		var updateSQL string
		if freezeHours > 0 {
			updateSQL = `
UPDATE user_affiliates
SET aff_frozen_quota = aff_frozen_quota + $1,
    aff_history_quota = aff_history_quota + $1,
    updated_at = NOW()
WHERE user_id = $2`
		} else {
			updateSQL = `
UPDATE user_affiliates
SET aff_quota = aff_quota + $1,
    aff_history_quota = aff_history_quota + $1,
    updated_at = NOW()
WHERE user_id = $2`
		}
		updateResult, err := txClient.ExecContext(txCtx, updateSQL, amount, inviterID)
		if err != nil {
			return fmt.Errorf("credit affiliate quota: %w", err)
		}
		affected, err := updateResult.RowsAffected()
		if err != nil {
			return err
		}
		if affected != 1 {
			return service.ErrAffiliateProfileNotFound
		}

		result = service.AffiliateAccrualResult{Amount: amount, Applied: true}
		return nil
	})
	if err != nil {
		return service.AffiliateAccrualResult{}, err
	}
	return result, nil
}

func insertAffiliateAccrualLedger(
	ctx context.Context,
	client *dbent.Client,
	inviterID int64,
	inviteeUserID int64,
	amount float64,
	freezeHours int,
	sourceOrderID *int64,
	sourceRedeemCodeID *int64,
) (bool, error) {
	if sourceRedeemCodeID != nil {
		rows, err := client.QueryContext(ctx, `
INSERT INTO user_affiliate_ledger (
    user_id,
    action,
    amount,
    source_user_id,
    source_order_id,
    source_redeem_code_id,
    frozen_until,
    created_at,
    updated_at
)
VALUES (
    $1,
    'accrue',
    $2,
    $3,
    $4,
    $5,
    CASE WHEN $6 > 0 THEN NOW() + ($6 * INTERVAL '1 hour') ELSE NULL END,
    NOW(),
    NOW()
)
ON CONFLICT (source_redeem_code_id)
    WHERE action = 'accrue' AND source_redeem_code_id IS NOT NULL
DO NOTHING
RETURNING id`, inviterID, amount, inviteeUserID, nullableInt64Arg(sourceOrderID), *sourceRedeemCodeID, freezeHours)
		if err != nil {
			return false, fmt.Errorf("insert affiliate redeem ledger: %w", err)
		}
		defer func() { _ = rows.Close() }()
		inserted := rows.Next()
		if err := rows.Err(); err != nil {
			return false, err
		}
		return inserted, rows.Close()
	}

	_, err := client.ExecContext(ctx, `
INSERT INTO user_affiliate_ledger (
    user_id,
    action,
    amount,
    source_user_id,
    source_order_id,
    source_redeem_code_id,
    frozen_until,
    created_at,
    updated_at
)
VALUES (
    $1,
    'accrue',
    $2,
    $3,
    $4,
    NULL,
    CASE WHEN $5 > 0 THEN NOW() + ($5 * INTERVAL '1 hour') ELSE NULL END,
    NOW(),
    NOW()
)`, inviterID, amount, inviteeUserID, nullableInt64Arg(sourceOrderID), freezeHours)
	if err != nil {
		return false, fmt.Errorf("insert affiliate accrue ledger: %w", err)
	}
	return true, nil
}

func scanAffiliateSingleRow(ctx context.Context, client affiliateQueryExecer, query string, args []any, dest ...any) error {
	rows, err := client.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := rows.Scan(dest...); err != nil {
		return err
	}
	return rows.Err()
}

func normalizeAffiliateLedgerAmount(value float64) float64 {
	return math.Round(value*1e8) / 1e8
}
