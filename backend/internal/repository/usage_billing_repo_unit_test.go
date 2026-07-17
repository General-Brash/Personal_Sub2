//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const (
	conditionalBalanceDeductSQL = `(?s)UPDATE users\s+SET balance = balance - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL AND balance >= \$1\s+RETURNING balance`
	overdraftBalanceDeductSQL   = `(?s)UPDATE users\s+SET balance = balance - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL\s+RETURNING balance`
	reserveBatchImageHoldSQL    = `(?s)UPDATE users\s+SET balance = balance - \$1,\s+frozen_balance = COALESCE\(frozen_balance, 0\) \+ \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL AND balance >= \$1\s+RETURNING balance, frozen_balance`
	captureBatchImageHoldSQL    = `(?s)UPDATE users\s+SET balance = balance\s+\+ CASE WHEN \$1 > \$2 THEN \$1 - \$2 ELSE 0 END\s+- CASE WHEN \$2 > \$1 THEN \$2 - \$1 ELSE 0 END,\s+frozen_balance = COALESCE\(frozen_balance, 0\) - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$3 AND deleted_at IS NULL AND COALESCE\(frozen_balance, 0\) >= \$1\s+RETURNING balance, frozen_balance`
	releaseBatchImageHoldSQL    = `(?s)UPDATE users\s+SET balance = balance \+ \$1,\s+frozen_balance = COALESCE\(frozen_balance, 0\) - \$1,\s+updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL AND COALESCE\(frozen_balance, 0\) >= \$1\s+RETURNING balance, frozen_balance`
	userExistsForBillingSQL     = `(?s)SELECT 1\s+FROM users\s+WHERE id = \$1 AND deleted_at IS NULL`
)

func TestDeductUsageBillingBalance_UsesSufficientBalanceGuard(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(2.5, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(7.5))
	mock.ExpectCommit()

	newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, 42, 2.5)
	require.NoError(t, err)
	require.True(t, sufficient)
	require.InDelta(t, 7.5, newBalance, 1e-12)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeductUsageBillingBalance_RecordsOverdraftWhenGuardMisses(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(float64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(overdraftBalanceDeductSQL).
		WithArgs(float64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(-5.0))
	mock.ExpectCommit()

	newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, 42, 10)
	require.NoError(t, err)
	require.False(t, sufficient)
	require.InDelta(t, -5, newBalance, 1e-12)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyUsageBillingEffects_FlagsBalanceOverdraft(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)SELECT id, remaining_amount\s+FROM temporary_credit_grants\s+WHERE user_id = \$1 AND remaining_amount > 0 AND expires_at > clock_timestamp\(\)\s+ORDER BY expires_at ASC, id ASC\s+FOR UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}))
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(float64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(overdraftBalanceDeductSQL).
		WithArgs(float64(10), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(-5.0))
	mock.ExpectCommit()

	result := &service.UsageBillingApplyResult{Applied: true}
	err = (&usageBillingRepository{}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "usage-billing-overdraft",
		UserID:      42,
		BalanceCost: 10,
	}, result)
	require.NoError(t, err)
	require.NotNil(t, result.NewBalance)
	require.NotNil(t, result.PermanentBalanceDeduction)
	require.InDelta(t, 10, *result.PermanentBalanceDeduction, 1e-12)
	require.InDelta(t, -5, *result.NewBalance, 1e-12)
	require.True(t, result.BalanceOverdrafted)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyUsageBillingEffects_ConsumesTemporaryCreditBeforePermanentBalance(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`(?s)SELECT id, remaining_amount\s+FROM temporary_credit_grants\s+WHERE user_id = \$1 AND remaining_amount > 0 AND expires_at > clock_timestamp\(\)\s+ORDER BY expires_at ASC, id ASC\s+FOR UPDATE`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(int64(7), "1.50000000"))
	mock.ExpectQuery(`(?s)UPDATE temporary_credit_grants\s+SET remaining_amount = remaining_amount - \$1,\s+updated_at = clock_timestamp\(\)\s+WHERE id = \$2 AND remaining_amount >= \$1 AND expires_at > clock_timestamp\(\)\s+RETURNING \$1::numeric`).
		WithArgs("1.50000000", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow("1.50000000"))
	mock.ExpectExec(`(?s)INSERT INTO temporary_credit_consumptions \(grant_id, usage_log_id, request_id, amount\)\s+VALUES \(\$1, \$2, \$3, \$4\)`).
		WithArgs(int64(7), nil, "req-1", "1.50000000").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result := &service.UsageBillingApplyResult{Applied: true}
	err = (&usageBillingRepository{db: db}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-1",
		APIKeyID:    9,
		UserID:      42,
		BalanceCost: 1.5,
	}, result)
	require.NoError(t, err)
	require.Nil(t, result.NewBalance, "temporary credit fully covers the request")
	require.NotNil(t, result.PermanentBalanceDeduction)
	require.InDelta(t, 0, *result.PermanentBalanceDeduction, 1e-12)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyUsageBillingEffects_UsesUsageLogIDWithoutRawRequestID(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT id, remaining_amount`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(int64(7), "1.50000000"))
	mock.ExpectQuery(`UPDATE temporary_credit_grants`).
		WithArgs("1.50000000", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow("1.50000000"))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(7), int64(101), nil, "1.50000000").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	result := &service.UsageBillingApplyResult{Applied: true}
	err = (&usageBillingRepository{db: db}).applyUsageBillingEffects(ctx, tx, &service.UsageBillingCommand{
		RequestID:   "req-1",
		APIKeyID:    9,
		UserID:      42,
		UsageLog:    &service.UsageLog{ID: 101},
		BalanceCost: 1.5,
	}, result)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryApply_RollsBackWhenUsageLogInsertFails(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO usage_logs`).
		WillReturnError(errors.New("usage log insert failed"))
	mock.ExpectRollback()

	_, err = (&usageBillingRepository{db: db}).Apply(ctx, &service.UsageBillingCommand{
		RequestID: "req-log-failure",
		APIKeyID:  9,
		UserID:    42,
		AccountID: 77,
		UsageLog: &service.UsageLog{
			UserID:    42,
			APIKeyID:  9,
			AccountID: 77,
			RequestID: "req-log-failure",
			Model:     "gpt-5",
		},
	})
	require.ErrorContains(t, err, "usage log insert failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUsageBillingRepositoryApplyRejectsMismatchedUsageLogIdentity(t *testing.T) {
	t.Run("request id surrounding whitespace is equivalent after normalization", func(t *testing.T) {
		cmd := &service.UsageBillingCommand{
			RequestID: "  req-contract  ",
			APIKeyID:  9,
			UserID:    42,
			UsageLog: &service.UsageLog{
				UserID:    42,
				APIKeyID:  9,
				RequestID: "\t req-contract \n",
			},
		}

		require.NoError(t, cmd.Normalize())
		require.Equal(t, "req-contract", cmd.RequestID)
		require.NoError(t, validateUsageBillingUsageLog(cmd))
	})

	tests := []struct {
		name    string
		log     service.UsageLog
		wantErr string
	}{
		{
			name:    "user id",
			log:     service.UsageLog{UserID: 0, APIKeyID: 9, RequestID: "req-contract"},
			wantErr: "user_id mismatch",
		},
		{
			name:    "api key id",
			log:     service.UsageLog{UserID: 42, APIKeyID: 0, RequestID: "req-contract"},
			wantErr: "api_key_id mismatch",
		},
		{
			name:    "request id",
			log:     service.UsageLog{UserID: 42, APIKeyID: 9, RequestID: "other-request"},
			wantErr: "request_id mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()

			_, err = (&usageBillingRepository{db: db}).Apply(context.Background(), &service.UsageBillingCommand{
				RequestID: "req-contract",
				APIKeyID:  9,
				UserID:    42,
				UsageLog:  &tt.log,
			})

			require.ErrorContains(t, err, tt.wantErr)
			require.NoError(t, mock.ExpectationsWereMet(), "validation must happen before opening a transaction")
		})
	}
}

func TestUsageBillingRepositoryApply_RollsBackUsageLogAndTemporaryCreditWhenPermanentBalanceFails(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	mock.ExpectQuery(`INSERT INTO usage_logs`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow(int64(101), time.Now()))
	mock.ExpectQuery(`INSERT INTO usage_billing_dedup`).
		WithArgs("req-permanent-failure", int64(9), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(1)))
	mock.ExpectQuery(`SELECT request_fingerprint\s+FROM usage_billing_dedup_archive`).
		WithArgs("req-permanent-failure", int64(9)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT id, remaining_amount`).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "remaining_amount"}).AddRow(int64(7), "0.25000000"))
	mock.ExpectQuery(`UPDATE temporary_credit_grants`).
		WithArgs("0.25000000", int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"deducted"}).AddRow("0.25000000"))
	mock.ExpectExec(`INSERT INTO temporary_credit_consumptions`).
		WithArgs(int64(7), int64(101), nil, "0.25000000").
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(sqlmock.AnyArg(), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(overdraftBalanceDeductSQL).
		WithArgs(sqlmock.AnyArg(), int64(42)).
		WillReturnError(errors.New("permanent balance write failed"))
	mock.ExpectRollback()

	_, err = (&usageBillingRepository{db: db}).Apply(ctx, &service.UsageBillingCommand{
		RequestID:   "req-permanent-failure",
		APIKeyID:    9,
		UserID:      42,
		AccountID:   77,
		BalanceCost: 1,
		UsageLog: &service.UsageLog{
			UserID:    42,
			APIKeyID:  9,
			AccountID: 77,
			RequestID: "req-permanent-failure",
			Model:     "gpt-5",
		},
	})
	require.ErrorContains(t, err, "permanent balance write failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIncrementUsageBillingAccountQuota_UsesFloatStateForFirstCrossing(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)

	firstAmount := 8.125
	mock.ExpectQuery(`(?s)UPDATE accounts SET extra =`).
		WithArgs(sqlmock.AnyArg(), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"quota_used", "quota_limit",
			"quota_daily_used", "quota_daily_limit",
			"quota_weekly_used", "quota_weekly_limit",
		}).AddRow(12.125, 10.0, 0.0, 0.0, 0.0, 0.0))
	mock.ExpectExec(`(?s)INSERT INTO scheduler_outbox`).
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(42), nil, nil, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	state, err := incrementUsageBillingAccountQuota(ctx, tx, 42, firstAmount)
	require.NoError(t, err)
	require.Equal(t, 12.125, state.TotalUsed)

	secondAmount := 2.0
	mock.ExpectQuery(`(?s)UPDATE accounts SET extra =`).
		WithArgs(sqlmock.AnyArg(), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{
			"quota_used", "quota_limit",
			"quota_daily_used", "quota_daily_limit",
			"quota_weekly_used", "quota_weekly_limit",
		}).AddRow(14.125, 10.0, 0.0, 0.0, 0.0, 0.0))

	state, err = incrementUsageBillingAccountQuota(ctx, tx, 42, secondAmount)
	require.NoError(t, err)
	require.Equal(t, 14.125, state.TotalUsed)

	mock.ExpectCommit()
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestDeductUsageBillingBalance_ReturnsUserNotFoundWhenNoUserUpdated(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(conditionalBalanceDeductSQL).
		WithArgs(float64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(overdraftBalanceDeductSQL).
		WithArgs(float64(10), int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, _, err = deductUsageBillingBalance(ctx, tx, 42, 10)
	require.ErrorIs(t, err, service.ErrUserNotFound)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReserveUsageBillingBatchImageBalance_MovesAvailableToFrozen(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(reserveBatchImageHoldSQL).
		WithArgs(2.5, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(7.5, 2.5))
	mock.ExpectCommit()

	result, err := reserveUsageBillingBatchImageBalance(ctx, tx, &service.BatchImageBalanceHoldCommand{UserID: 42, HoldAmount: 2.5})
	require.NoError(t, err)
	require.NotNil(t, result.NewBalance)
	require.NotNil(t, result.FrozenBalance)
	require.InDelta(t, 7.5, *result.NewBalance, 0.000001)
	require.InDelta(t, 2.5, *result.FrozenBalance, 0.000001)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReserveUsageBillingBatchImageBalance_InsufficientBalance(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(reserveBatchImageHoldSQL).
		WithArgs(10.0, int64(42)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(userExistsForBillingSQL).
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectRollback()

	_, err = reserveUsageBillingBatchImageBalance(ctx, tx, &service.BatchImageBalanceHoldCommand{UserID: 42, HoldAmount: 10})
	require.ErrorIs(t, err, service.ErrBatchImageInsufficientBalance)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCaptureUsageBillingBatchImageBalance_ReleasesRemainder(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(captureBatchImageHoldSQL).
		WithArgs(1.0, 0.25, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(9.75, 0.0))
	mock.ExpectCommit()

	result, err := captureUsageBillingBatchImageBalance(ctx, tx, &service.BatchImageBalanceHoldCommand{UserID: 42, HoldAmount: 1, ActualAmount: 0.25})
	require.NoError(t, err)
	require.InDelta(t, 9.75, *result.NewBalance, 0.000001)
	require.InDelta(t, 0.0, *result.FrozenBalance, 0.000001)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCaptureUsageBillingBatchImageBalance_RejectsActualCostOverHold(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectRollback()

	_, err = captureUsageBillingBatchImageBalance(ctx, tx, &service.BatchImageBalanceHoldCommand{UserID: 42, HoldAmount: 0.5, ActualAmount: 1})
	require.ErrorIs(t, err, service.ErrBatchImageSettlementCostExceedsHold)
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReleaseUsageBillingBatchImageBalance_ReturnsFrozenToAvailable(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup\s+WHERE request_id = \$1 AND api_key_id = \$2`).
		WithArgs(service.BatchImageHoldRequestID("imgbatch_release"), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"?column?"}).AddRow(1))
	mock.ExpectQuery(releaseBatchImageHoldSQL).
		WithArgs(1.0, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "frozen_balance"}).AddRow(10.0, 0.0))
	mock.ExpectCommit()

	result, err := releaseUsageBillingBatchImageBalance(ctx, tx, &service.BatchImageBalanceHoldCommand{UserID: 42, APIKeyID: 7, BatchID: "imgbatch_release", HoldAmount: 1})
	require.NoError(t, err)
	require.InDelta(t, 10.0, *result.NewBalance, 0.000001)
	require.InDelta(t, 0.0, *result.FrozenBalance, 0.000001)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReleaseUsageBillingBatchImageBalance_SkipsWhenHoldNeverReserved(t *testing.T) {
	ctx := context.Background()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	mock.ExpectBegin()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	// dedup 与归档表均无 hold claim：说明该 job 从未成功冻结，
	// 释放必须跳过，不得从他人冻结资金池中凭空生成余额。
	mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup\s+WHERE request_id = \$1 AND api_key_id = \$2`).
		WithArgs(service.BatchImageHoldRequestID("imgbatch_phantom"), int64(7)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`SELECT 1\s+FROM usage_billing_dedup_archive\s+WHERE request_id = \$1 AND api_key_id = \$2`).
		WithArgs(service.BatchImageHoldRequestID("imgbatch_phantom"), int64(7)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectCommit()

	result, err := releaseUsageBillingBatchImageBalance(ctx, tx, &service.BatchImageBalanceHoldCommand{UserID: 42, APIKeyID: 7, BatchID: "imgbatch_phantom", HoldAmount: 1})
	require.NoError(t, err)
	require.Nil(t, result.NewBalance)
	require.Nil(t, result.FrozenBalance)
	require.NoError(t, tx.Commit())
	require.NoError(t, mock.ExpectationsWereMet())
}
