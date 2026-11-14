package repository

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type usageBillingRepository struct {
	db                         *sql.DB
	availableCreditInvalidator service.AvailableCreditInvalidator
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) service.UsageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) SetAvailableCreditInvalidator(invalidator service.AvailableCreditInvalidator) {
	if r != nil {
		r.availableCreditInvalidator = invalidator
	}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (*service.UsageBillingApplyResult, error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}

	if err := cmd.Normalize(); err != nil {
		return nil, err
	}
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}
	if err := validateUsageBillingUsageLog(cmd); err != nil {
		return nil, err
	}

	var result *service.UsageBillingApplyResult
	var committedUsageLog *service.UsageLog
	err := runUsageBillingTransaction(ctx, r.db, func(tx *sql.Tx) (bool, error) {
		// createSingle mutates ID, time and normalized log fields. Each attempt
		// starts from the caller's unchanged value and publishes only after commit.
		attemptCmd := *cmd
		if cmd.UsageLog != nil {
			usageLog := *cmd.UsageLog
			attemptCmd.UsageLog = &usageLog
		}
		if err := lockUsageBillingRows(ctx, tx, &attemptCmd); err != nil {
			return false, err
		}
		if err := insertUsageBillingUsageLog(ctx, tx, &attemptCmd); err != nil {
			return false, err
		}
		applied, err := r.claimUsageBillingKey(ctx, tx, &attemptCmd)
		if err != nil {
			return false, err
		}
		result = &service.UsageBillingApplyResult{Applied: applied}
		if !applied {
			return false, nil
		}
		if err := r.applyUsageBillingEffects(ctx, tx, &attemptCmd, result); err != nil {
			return false, err
		}
		committedUsageLog = attemptCmd.UsageLog
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	if result.Applied && cmd.UsageLog != nil {
		*cmd.UsageLog = *committedUsageLog
	}
	return result, nil
}

func lockUsageBillingRows(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	// Lock mutable parents before INSERT can acquire foreign-key KEY SHARE:
	// user, API key, subscription, account. Bank and grant rows follow the user.
	if cmd.UserID > 0 || cmd.BalanceCost > 0 {
		// Allocate still locks users FOR UPDATE for balance billing; acquire
		// that mode now rather than upgrade after foreign-key checks. Other
		// billing paths also lock the user before any API key/subscription.
		if err := lockUsageBillingUser(ctx, tx, cmd.UserID, cmd.BalanceCost > 0); err != nil {
			return err
		}
	}
	var id int64
	if cmd.APIKeyQuotaCost > 0 || cmd.APIKeyRateLimitCost > 0 {
		err := tx.QueryRowContext(ctx, `SELECT id FROM api_keys WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`, cmd.APIKeyID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrAPIKeyNotFound
		}
		if err != nil {
			return err
		}
	}
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		err := tx.QueryRowContext(ctx, `SELECT id FROM user_subscriptions WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`, *cmd.SubscriptionID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrSubscriptionNotFound
		}
		if err != nil {
			return err
		}
	}
	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`, cmd.AccountID).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrAccountNotFound
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func lockUsageBillingUser(ctx context.Context, tx *sql.Tx, userID int64, forUpdate bool) error {
	query := `SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR NO KEY UPDATE`
	if forUpdate {
		query = `SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	}
	var id int64
	err := tx.QueryRowContext(ctx, query, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUserNotFound
	}
	return err
}

func validateUsageBillingUsageLog(cmd *service.UsageBillingCommand) error {
	if cmd == nil || cmd.UsageLog == nil {
		return nil
	}
	if cmd.UsageLog.UserID != cmd.UserID {
		return errors.New("usage billing usage log user_id mismatch")
	}
	if cmd.UsageLog.APIKeyID != cmd.APIKeyID {
		return errors.New("usage billing usage log api_key_id mismatch")
	}
	if strings.TrimSpace(cmd.UsageLog.RequestID) != cmd.RequestID {
		return errors.New("usage billing usage log request_id mismatch")
	}
	return nil
}

func insertUsageBillingUsageLog(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	if cmd == nil || cmd.UsageLog == nil {
		return nil
	}
	_, err := (&usageLogRepository{sql: tx}).createSingle(ctx, tx, cmd.UsageLog)
	return err
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
		UNION ALL SELECT $3::text FROM usage_request_archive WHERE request_id=$1 AND api_key_id=$2
		LIMIT 1
	`, requestID, apiKeyID, requestFingerprint).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, batchImageHoldOperationReserve)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, batchImageHoldOperationCapture)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	return r.applyBatchImageBalanceHold(ctx, cmd, batchImageHoldOperationRelease)
}

func (r *usageBillingRepository) applyBatchImageBalanceHold(
	ctx context.Context,
	cmd *service.BatchImageBalanceHoldCommand,
	operation batchImageHoldOperation,
) (*service.BatchImageBalanceHoldResult, error) {
	if cmd == nil {
		return &service.BatchImageBalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	if err := cmd.Normalize(); err != nil {
		return nil, err
	}
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	var result *service.BatchImageBalanceHoldResult
	var committedCmd service.BatchImageBalanceHoldCommand
	err := runUsageBillingTransaction(ctx, r.db, func(tx *sql.Tx) (bool, error) {
		attemptCmd := *cmd
		var err error
		result, err = r.applyBatchImageCreditHoldOperation(ctx, tx, &attemptCmd, operation)
		if err != nil {
			return false, err
		}
		if result == nil {
			result = &service.BatchImageBalanceHoldResult{}
		}
		if operation == batchImageHoldOperationCapture && result.Applied && attemptCmd.DynamicRateSnapshot != nil {
			groupID := attemptCmd.DynamicRateSnapshot.GroupID
			dynamicCmd := &service.UsageBillingCommand{RequestID: attemptCmd.RequestID, APIKeyID: attemptCmd.APIKeyID, UserID: attemptCmd.UserID, BalanceCost: attemptCmd.ActualAmount, DynamicRateSnapshot: attemptCmd.DynamicRateSnapshot, UsageLog: &service.UsageLog{GroupID: &groupID}}
			actual := decimal.NewFromFloat(result.TemporaryCapturedAmount).Add(decimal.NewFromFloat(result.PermanentCapturedAmount))
			if err := incrementDynamicRateUsage(ctx, tx, dynamicCmd, actual); err != nil {
				return false, err
			}
		}
		committedCmd = attemptCmd
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	*cmd = committedCmd
	if result.AutoExchange != nil && r.availableCreditInvalidator != nil {
		if invalidator, ok := r.availableCreditInvalidator.(interface {
			InvalidateUserBalance(context.Context, int64) error
		}); ok {
			if err := invalidator.InvalidateUserBalance(ctx, cmd.UserID); err != nil {
				logger.LegacyPrintf("repository.usage_billing", "invalidate balance cache after batch image auto exchange failed: user=%d err=%v", cmd.UserID, err)
			}
		}
		if err := r.availableCreditInvalidator.InvalidateAvailableCredit(ctx, cmd.UserID); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "invalidate available credit after batch image auto exchange failed: user=%d err=%v", cmd.UserID, err)
		}
	}
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	dynamicWalletDelta := decimal.Zero
	if cmd.BalanceCost > 0 {
		reference, err := temporaryCreditReferenceForUsageBilling(cmd)
		if err != nil {
			return err
		}
		autoExchange, err := service.BankAutoExchangeInTx(ctx, tx, cmd.UserID)
		if err != nil {
			return err
		}
		if autoExchange.Exchanged || autoExchange.Settled {
			result.AutoExchange = &autoExchange
			result.NewBalance = &autoExchange.PermanentBalance
		}
		permanentBalanceCost, err := service.NewTemporaryCreditAllocationExecutor().Allocate(
			ctx,
			tx,
			cmd.UserID,
			cmd.BalanceCost,
			reference,
		)
		if err != nil {
			return err
		}
		result.PermanentBalanceDeduction = &permanentBalanceCost
		temporaryBalanceCost := cmd.BalanceCost - permanentBalanceCost
		if temporaryBalanceCost < 0 {
			temporaryBalanceCost = 0
		}
		// The dynamic wallet counter records the actual split charge: FEFO
		// temporary-credit consumption plus permanent-balance deduction. It
		// never infers this from Cost.ActualCost or from account cost.
		dynamicWalletDelta = decimal.NewFromFloat(temporaryBalanceCost + permanentBalanceCost)
		if permanentBalanceCost > 0 {
			newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, permanentBalanceCost)
			if err != nil {
				return err
			}
			result.NewBalance = &newBalance
			result.BalanceOverdrafted = !sufficient
		}
	}

	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		if err := incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost); err != nil {
			return err
		}
	}

	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost)
		if err != nil {
			return err
		}
		result.QuotaState = quotaState
	}

	if err := incrementDynamicRateUsage(ctx, tx, cmd, dynamicWalletDelta); err != nil {
		return err
	}

	return nil
}

func temporaryCreditReferenceForUsageBilling(cmd *service.UsageBillingCommand) (service.TemporaryCreditConsumptionReference, error) {
	if cmd == nil {
		return service.TemporaryCreditConsumptionReference{}, errors.New("usage billing command is required")
	}
	if cmd.UsageLog == nil {
		return service.TemporaryCreditConsumptionReference{RequestID: cmd.RequestID}, nil
	}
	if cmd.UsageLog.ID <= 0 {
		return service.TemporaryCreditConsumptionReference{}, errors.New("usage billing usage log id is required")
	}
	usageLogID := cmd.UsageLog.ID
	return service.TemporaryCreditConsumptionReference{UsageLogID: &usageLogID}, nil
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions us
		SET
			daily_usage_usd = us.daily_usage_usd + $1,
			weekly_usage_usd = us.weekly_usage_usd + $1,
			monthly_usage_usd = us.monthly_usage_usd + $1,
			updated_at = NOW()
		FROM groups g
		WHERE us.id = $2
			AND us.deleted_at IS NULL
			AND us.group_id = g.id
			AND g.deleted_at IS NULL
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		return false, service.ErrAPIKeyNotFound
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrAPIKeyNotFound
	}
	return nil
}

func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	rows, err := tx.QueryContext(ctx,
		`UPDATE accounts SET extra = (
			COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN `+dailyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN `+dailyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_daily_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+dailyExpiredExpr+` AND `+nextDailyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', `+nextDailyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN `+weeklyExpiredExpr+`
					THEN `+nowUTC+`
					ELSE COALESCE(extra->>'quota_weekly_start', `+nowUTC+`) END
				)
				|| CASE WHEN `+weeklyExpiredExpr+` AND `+nextWeeklyResetAtExpr+` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', `+nextWeeklyResetAtExpr+`)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END
		), updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`,
		amount, accountID)
	if err != nil {
		return nil, err
	}

	var state service.AccountQuotaState
	if rows.Next() {
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	} else {
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		return nil, service.ErrAccountNotFound
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return &state, nil
}
