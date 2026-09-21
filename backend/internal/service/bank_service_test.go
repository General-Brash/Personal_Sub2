package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func expectBankPolicy(mock sqlmock.Sqlmock, policy BankPolicy) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT key, value FROM settings WHERE key IN ($1,$2,$3,$4,$5,$6,$7,$8,$9)")).
		WithArgs(
			SettingKeyBankAdvanceMinAmount,
			SettingKeyBankAdvanceMaxAmount,
			SettingKeyBankDebtGraceDays,
			SettingKeyBankDebtConversionRatio,
			SettingKeyBankExchangeRate,
			SettingKeyBankExchangeTiers,
			SettingKeyBankUnusedAdvanceDebtReductionRatio,
			SettingKeyBankEarlyRepayTemporaryRatio,
			SettingKeyBankEarlyRepayPermanentRatio,
		).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
			AddRow(SettingKeyBankAdvanceMinAmount, formatLedgerAmount(policy.AdvanceMinAmount)).
			AddRow(SettingKeyBankAdvanceMaxAmount, formatLedgerAmount(policy.AdvanceMaxAmount)).
			AddRow(SettingKeyBankDebtGraceDays, policy.DebtGraceDays).
			AddRow(SettingKeyBankDebtConversionRatio, formatLedgerAmount(policy.DebtConversionRatio)).
			AddRow(SettingKeyBankExchangeRate, formatLedgerAmount(policy.ExchangeRate)).
			AddRow(SettingKeyBankExchangeTiers, marshalBankExchangeTiers(policy.ExchangeTiers)).
			AddRow(SettingKeyBankUnusedAdvanceDebtReductionRatio, formatLedgerAmount(policy.UnusedAdvanceDebtReductionRatio)).
			AddRow(SettingKeyBankEarlyRepayTemporaryRatio, formatLedgerAmount(policy.EarlyRepayTemporaryRatio)).
			AddRow(SettingKeyBankEarlyRepayPermanentRatio, formatLedgerAmount(policy.EarlyRepayPermanentRatio)))
}

func expectNoExpiredBankAdvance(mock sqlmock.Sqlmock, userID int64, now time.Time) {
	mock.ExpectQuery("SELECT loan.id, loan.grant_id, loan.debt_remaining, credit_grant.remaining_amount").
		WithArgs(userID, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "grant_id", "debt_remaining", "remaining_amount"}))
}

func expectBankSettlementNoop(mock sqlmock.Sqlmock, policy BankPolicy, userID int64, balance, debt string, dueAt, now time.Time) {
	mock.ExpectBegin()
	expectBankPolicy(mock, policy)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow(balance, debt, dueAt))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectNoExpiredBankAdvance(mock, userID, now)
	mock.ExpectCommit()
}

func TestBankPolicyDefaultsAndValidation(t *testing.T) {
	policy := DefaultBankPolicy()
	require.Equal(t, float64(5), policy.AdvanceMinAmount)
	require.Equal(t, float64(20), policy.AdvanceMaxAmount)
	require.Equal(t, 3, policy.DebtGraceDays)
	require.Equal(t, float64(1), policy.DebtConversionRatio)
	require.Equal(t, float64(1), policy.ExchangeRate)
	require.Equal(t, float64(0.75), policy.UnusedAdvanceDebtReductionRatio)
	require.Equal(t, float64(1), policy.EarlyRepayTemporaryRatio)
	require.Equal(t, float64(2), policy.EarlyRepayPermanentRatio)
	require.NoError(t, policy.Validate())

	policy.AdvanceMaxAmount = 4
	require.ErrorIs(t, policy.Validate(), ErrBankPolicyInvalid)
}

func TestSettleBankDebtLockedBeforeDueDoesNothing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	now := time.Date(2026, time.July, 19, 8, 0, 0, 0, time.UTC)
	settled, err := settleBankDebtLocked(context.Background(), tx, 42, 10, 5, sql.NullTime{Time: now.Add(time.Hour), Valid: true}, DefaultBankPolicy(), now)

	require.NoError(t, err)
	require.False(t, settled)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettleBankDebtLockedDeductsPermanentBalanceOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	now := time.Date(2026, time.July, 19, 8, 0, 0, 0, time.UTC)
	policy := DefaultBankPolicy()
	policy.DebtConversionRatio = 1.5
	mock.ExpectQuery("UPDATE bank_loans").
		WithArgs(now, "7.50000000", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectExec("UPDATE users").
		WithArgs("7.50000000", now, int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(int64(42), int64(9), "-7.50000000", "-5.00000000", "5.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	settled, err := settleBankDebtLocked(context.Background(), tx, 42, 2, 5, sql.NullTime{Time: now.Add(-time.Second), Valid: true}, policy, now)

	require.NoError(t, err)
	require.True(t, settled)
	settled, err = settleBankDebtLocked(context.Background(), tx, 42, -5.5, 0, sql.NullTime{}, policy, now.Add(time.Second))
	require.NoError(t, err)
	require.False(t, settled)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyTemporaryCreditDebtOffsetTxPartiallyRepaysDebt(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	now := time.Date(2026, time.July, 19, 8, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("20.00000000", "10.00000000", now.Add(time.Second)))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectBankPolicy(mock, DefaultBankPolicy())
	expectNoExpiredBankAdvance(mock, int64(42), now)
	mock.ExpectQuery("UPDATE temporary_credit_grants").
		WithArgs("4.00000000", int64(8), int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"remaining_amount"}).AddRow("0.00000000"))
	mock.ExpectQuery("UPDATE users").
		WithArgs("4.00000000", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"temporary_credit_debt"}).AddRow("6.00000000"))
	mock.ExpectQuery("UPDATE bank_loans").
		WithArgs("4.00000000", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(3)))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(int64(42), int64(3), int64(8), "-4.00000000", "-4.00000000", "10.00000000", "6.00000000").
		WillReturnResult(sqlmock.NewResult(1, 1))

	remaining, offset, err := ApplyTemporaryCreditDebtOffsetTx(context.Background(), tx, 42, 8, 4)

	require.NoError(t, err)
	require.Zero(t, remaining)
	require.Equal(t, float64(4), offset)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestApplyTemporaryCreditDebtOffsetTxSettlesDebtDueAtDatabaseNowBeforeOffset(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	now := time.Date(2026, time.July, 19, 8, 0, 0, 0, time.UTC)
	policy := DefaultBankPolicy()
	policy.DebtConversionRatio = 1.5
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("2.00000000", "10.00000000", now))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectBankPolicy(mock, policy)
	expectNoExpiredBankAdvance(mock, int64(42), now)
	mock.ExpectQuery("UPDATE bank_loans").
		WithArgs(now, "15.00000000", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectExec("UPDATE users").
		WithArgs("15.00000000", now, int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(int64(42), int64(9), "-15.00000000", "-10.00000000", "10.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))

	remaining, offset, err := ApplyTemporaryCreditDebtOffsetTx(context.Background(), tx, 42, 8, 4)

	require.NoError(t, err)
	require.Equal(t, float64(4), remaining)
	require.Zero(t, offset)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExchangeAtomicSettlesDebtThatBecomesDueInsideTransactionBeforeDeduction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	userID := int64(42)
	loanID := int64(9)
	dueAt := time.Date(2026, time.July, 19, 10, 0, 0, 0, beijingLocation)
	outsideNow := dueAt.Add(-time.Second)
	insideNow := dueAt
	policy := DefaultBankPolicy()
	repo := &temporaryCreditRepositoryRecorder{}
	service := NewBankService(db, repo, nil)
	claim := newAtomicClaimForTest(insideNow.Add(24 * time.Hour))

	mock.ExpectBegin()
	expectBankPolicy(mock, policy)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("30.00000000", "10.00000000", dueAt))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(outsideNow))
	expectNoExpiredBankAdvance(mock, userID, outsideNow)
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("30.00000000", "10.00000000", dueAt))
	expectBankPolicy(mock, policy)
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(insideNow))
	expectNoExpiredBankAdvance(mock, userID, insideNow)
	mock.ExpectQuery("UPDATE bank_loans").
		WithArgs(insideNow, "10.00000000", userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(loanID))
	mock.ExpectExec("UPDATE users").
		WithArgs("10.00000000", insideNow, userID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(userID, loanID, "-10.00000000", "-10.00000000", "10.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("20.00000000", "0.00000000", nil))
	mock.ExpectQuery("SELECT permanent_exchanged::text FROM bank_exchange_daily_usage").
		WithArgs(userID, "2026-07-19").
		WillReturnRows(sqlmock.NewRows([]string{"permanent_exchanged"}).AddRow("0.00000000"))
	mock.ExpectQuery("UPDATE users").
		WithArgs("5.00000000", userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("15.00000000"))
	mock.ExpectQuery("INSERT INTO bank_exchange_daily_usage").
		WithArgs(userID, "2026-07-19", "5.00000000").
		WillReturnRows(sqlmock.NewRows([]string{"permanent_exchanged"}).AddRow("5.00000000"))
	mock.ExpectQuery("SELECT balance, temporary_credit_debt FROM users").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt"}).
			AddRow("15.00000000", "0.00000000"))
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(remaining_amount\\), 0\\)").
		WithArgs(userID, insideNow).
		WillReturnRows(sqlmock.NewRows([]string{"available"}).AddRow("5.00000000"))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(userID, int64(42), "-5.00000000", "5.00000000", "0.00000000", "0.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE idempotency_records").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := service.ExchangeAtomic(context.Background(), userID, 5, claim)

	require.NoError(t, err)
	require.Equal(t, "5.00000000", result.PermanentSpent)
	require.Equal(t, "5.00000000", result.TemporaryGranted)
	require.Equal(t, "15.00000000", result.PermanentBalance)
	require.Equal(t, "0.00000000", result.TemporaryDebt)
	require.NotNil(t, repo.created)
	require.Equal(t, TemporaryCreditSourceBankExchange, repo.created.Source)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestExchangeAtomicAllowsDayBoundaryAndUsesDatabaseBusinessDate(t *testing.T) {
	for _, testCase := range []struct {
		name         string
		businessNow  time.Time
		expectedDate string
	}{
		{
			name:         "late evening remains on current Beijing date",
			businessNow:  time.Date(2026, time.July, 19, 23, 55, 0, 0, beijingLocation),
			expectedDate: "2026-07-19",
		},
		{
			name:         "early morning uses next Beijing date",
			businessNow:  time.Date(2026, time.July, 20, 0, 4, 59, 0, beijingLocation),
			expectedDate: "2026-07-20",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			userID := int64(42)
			policy := DefaultBankPolicy()
			repo := &temporaryCreditRepositoryRecorder{}
			service := NewBankService(db, repo, nil)
			claim := newAtomicClaimForTest(testCase.businessNow.Add(24 * time.Hour))
			eligibilityNow := testCase.businessNow.Add(-time.Second)

			mock.ExpectBegin()
			expectBankPolicy(mock, policy)
			mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
				WithArgs(userID).
				WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
					AddRow("30.00000000", "0.00000000", nil))
			mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
				WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(eligibilityNow))
			expectNoExpiredBankAdvance(mock, userID, eligibilityNow)
			mock.ExpectCommit()

			mock.ExpectBegin()
			mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
				WithArgs(userID).
				WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
					AddRow("30.00000000", "0.00000000", nil))
			expectBankPolicy(mock, policy)
			mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
				WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(testCase.businessNow))
			expectNoExpiredBankAdvance(mock, userID, testCase.businessNow)
			mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
				WithArgs(userID).
				WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
					AddRow("30.00000000", "0.00000000", nil))
			mock.ExpectQuery("SELECT permanent_exchanged::text FROM bank_exchange_daily_usage").
				WithArgs(userID, testCase.expectedDate).
				WillReturnRows(sqlmock.NewRows([]string{"permanent_exchanged"}).AddRow("0.00000000"))
			mock.ExpectQuery("UPDATE users").
				WithArgs("5.00000000", userID).
				WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("25.00000000"))
			mock.ExpectQuery("INSERT INTO bank_exchange_daily_usage").
				WithArgs(userID, testCase.expectedDate, "5.00000000").
				WillReturnRows(sqlmock.NewRows([]string{"permanent_exchanged"}).AddRow("5.00000000"))
			mock.ExpectQuery("SELECT balance, temporary_credit_debt FROM users").
				WithArgs(userID).
				WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt"}).
					AddRow("25.00000000", "0.00000000"))
			mock.ExpectQuery("SELECT COALESCE\\(SUM\\(remaining_amount\\), 0\\)").
				WithArgs(userID, testCase.businessNow).
				WillReturnRows(sqlmock.NewRows([]string{"available"}).AddRow("5.00000000"))
			mock.ExpectExec("INSERT INTO bank_ledger").
				WithArgs(userID, int64(42), "-5.00000000", "5.00000000", "0.00000000", "0.00000000", sqlmock.AnyArg()).
				WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE idempotency_records").
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			result, err := service.ExchangeAtomic(context.Background(), userID, 5, claim)

			require.NoError(t, err)
			require.Equal(t, "5.00000000", result.PermanentSpent)
			require.Equal(t, testCase.expectedDate, result.ExchangeProgress.Date)
			require.NotNil(t, repo.created)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCheckPermanentBalanceEligibilitySettlesDueDebtBeforeRejecting(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 19, 8, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	expectBankPolicy(mock, DefaultBankPolicy())
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("1.00000000", "2.00000000", now.Add(-time.Second)))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectNoExpiredBankAdvance(mock, int64(42), now)
	mock.ExpectQuery("UPDATE bank_loans").
		WithArgs(now, "2.00000000", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectExec("UPDATE users").
		WithArgs("2.00000000", now, int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(int64(42), int64(9), "-2.00000000", "-2.00000000", "2.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	mock.ExpectQuery("SELECT balance FROM users").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("-1.00000000"))

	svc := NewBankService(db, nil, nil)
	err = svc.CheckPermanentBalanceEligibility(context.Background(), 42)

	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettleUnusedAdvanceLockedReducesDebtAtConfiguredRatioOnce(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	now := time.Date(2026, time.July, 21, 16, 0, 0, 0, time.UTC)
	policy := DefaultBankPolicy()
	mock.ExpectQuery("SELECT loan.id, loan.grant_id, loan.debt_remaining, credit_grant.remaining_amount").
		WithArgs(int64(42), now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "grant_id", "debt_remaining", "remaining_amount"}).
			AddRow(int64(9), int64(8), "10.00000000", "8.00000000"))
	mock.ExpectQuery("UPDATE users").
		WithArgs("6.00000000", now, int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"temporary_credit_debt"}).AddRow("4.00000000"))
	mock.ExpectExec("UPDATE temporary_credit_grants").
		WithArgs(now, int64(8)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE bank_loans").
		WithArgs("6.00000000", now, "8.00000000", int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(int64(42), int64(9), int64(8), "-8.00000000", "-6.00000000", "10.00000000", "4.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT loan.id, loan.grant_id, loan.debt_remaining, credit_grant.remaining_amount").
		WithArgs(int64(42), now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "grant_id", "debt_remaining", "remaining_amount"}))

	debtAfter, processed, err := settleUnusedAdvanceLocked(context.Background(), tx, 42, 10, policy, now)
	require.NoError(t, err)
	require.True(t, processed)
	require.Equal(t, float64(4), debtAfter)
	debtAfter, processed, err = settleUnusedAdvanceLocked(context.Background(), tx, 42, debtAfter, policy, now)
	require.NoError(t, err)
	require.False(t, processed)
	require.Equal(t, float64(4), debtAfter)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestReconcileBankDebtLockedProcessesOlderRepaidLoanBeforeActiveLoan(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectBegin()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)

	userID := int64(42)
	now := time.Date(2026, time.July, 21, 16, 0, 0, 0, time.UTC)
	policy := DefaultBankPolicy()
	mock.ExpectQuery("SELECT loan.id, loan.grant_id, loan.debt_remaining, credit_grant.remaining_amount").
		WithArgs(userID, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "grant_id", "debt_remaining", "remaining_amount"}).
			AddRow(int64(8), int64(7), "0.00000000", "2.00000000"))
	mock.ExpectExec("UPDATE temporary_credit_grants").
		WithArgs(now, int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE bank_loans").
		WithArgs("0.00000000", now, "2.00000000", int64(8)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(userID, int64(8), int64(7), "-2.00000000", "0.00000000", "10.00000000", "10.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT loan.id, loan.grant_id, loan.debt_remaining, credit_grant.remaining_amount").
		WithArgs(userID, now).
		WillReturnRows(sqlmock.NewRows([]string{"id", "grant_id", "debt_remaining", "remaining_amount"}).
			AddRow(int64(9), int64(10), "10.00000000", "8.00000000"))
	mock.ExpectQuery("UPDATE users").
		WithArgs("6.00000000", now, userID).
		WillReturnRows(sqlmock.NewRows([]string{"temporary_credit_debt"}).AddRow("4.00000000"))
	mock.ExpectExec("UPDATE temporary_credit_grants").
		WithArgs(now, int64(10)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE bank_loans").
		WithArgs("6.00000000", now, "8.00000000", int64(9)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(userID, int64(9), int64(10), "-8.00000000", "-6.00000000", "10.00000000", "4.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	expectNoExpiredBankAdvance(mock, userID, now)

	debtAfter, unusedProcessed, forcedSettled, err := reconcileBankDebtLocked(
		context.Background(), tx, userID, 20, 10,
		sql.NullTime{Time: now.Add(time.Hour), Valid: true}, policy, now,
	)
	require.NoError(t, err)
	require.True(t, unusedProcessed)
	require.False(t, forcedSettled)
	require.Equal(t, float64(4), debtAfter)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSettleDueContinuesAfterPerUserFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	userOne := int64(41)
	userTwo := int64(42)
	now := time.Date(2026, time.July, 21, 16, 0, 0, 0, time.UTC)
	policy := DefaultBankPolicy()

	mock.ExpectQuery("SELECT id FROM users").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userOne).AddRow(userTwo))
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT key, value FROM settings").WillReturnError(errors.New("first user failed"))
	mock.ExpectRollback()
	mock.ExpectBegin()
	expectBankPolicy(mock, policy)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userTwo).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("5.00000000", "0.00000000", nil))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectNoExpiredBankAdvance(mock, userTwo, now)
	mock.ExpectCommit()

	err = NewBankService(db, nil, nil).SettleDue(context.Background())
	require.ErrorContains(t, err, "user 41")
	require.ErrorContains(t, err, "first user failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCapBankRepaymentCreditUsesEightDecimalCeiling(t *testing.T) {
	amount, err := capBankRepaymentCredit(10, 3, 2)
	require.NoError(t, err)
	require.Equal(t, "1.50000000", formatLedgerAmount(amount))

	amount, err = capBankRepaymentCredit(10, 1, 3)
	require.NoError(t, err)
	require.Equal(t, "0.33333334", formatLedgerAmount(amount))
	reduced, err := normalizeLedgerAmount(amount * 3)
	require.NoError(t, err)
	require.GreaterOrEqual(t, reduced, float64(1))

	amount, err = capBankRepaymentCredit(0.25, 3, 2)
	require.NoError(t, err)
	require.Equal(t, "0.25000000", formatLedgerAmount(amount))
}

func TestRepayAtomicPermanentCapsSpendAndDoesNotOverdraw(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	userID := int64(42)
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(48 * time.Hour)
	policy := DefaultBankPolicy()
	service := NewBankService(db, &temporaryCreditRepositoryRecorder{}, nil)
	claim := newAtomicClaimForTest(now.Add(24 * time.Hour))

	expectBankSettlementNoop(mock, policy, userID, "10.00000000", "4.00000000", dueAt, now)
	mock.ExpectBegin()
	expectBankPolicy(mock, policy)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("10.00000000", "4.00000000", dueAt))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectNoExpiredBankAdvance(mock, userID, now)
	mock.ExpectQuery("UPDATE users").
		WithArgs("2.00000000", "4.00000000", now, userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt"}).AddRow("8.00000000", "0.00000000"))
	mock.ExpectQuery("UPDATE bank_loans").
		WithArgs("4.00000000", now, userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(userID, "early_repay_permanent", int64(9), "-2.00000000", "0.00000000", "-4.00000000", "4.00000000", "0.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(remaining_amount\\), 0\\)").
		WithArgs(userID, now).
		WillReturnRows(sqlmock.NewRows([]string{"available"}).AddRow("5.00000000"))
	mock.ExpectExec("UPDATE idempotency_records").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := service.RepayAtomic(context.Background(), userID, BankRepaySourcePermanent, 10, claim)
	require.NoError(t, err)
	require.Equal(t, "2.00000000", result.CreditSpent)
	require.Equal(t, "4.00000000", result.DebtReduced)
	require.Equal(t, "0.00000000", result.TemporaryDebt)
	require.Equal(t, "8.00000000", result.PermanentBalance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepayAtomicTemporaryUsesCappedFEFOAmount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	userID := int64(42)
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(48 * time.Hour)
	policy := DefaultBankPolicy()
	repo := &temporaryCreditRepositoryRecorder{}
	service := NewBankService(db, repo, nil)
	claim := newAtomicClaimForTest(now.Add(24 * time.Hour))

	expectBankSettlementNoop(mock, policy, userID, "10.00000000", "3.00000000", dueAt, now)
	mock.ExpectBegin()
	expectBankPolicy(mock, policy)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("10.00000000", "3.00000000", dueAt))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectNoExpiredBankAdvance(mock, userID, now)
	mock.ExpectQuery("UPDATE users").
		WithArgs("3.00000000", now, userID).
		WillReturnRows(sqlmock.NewRows([]string{"temporary_credit_debt"}).AddRow("0.00000000"))
	mock.ExpectQuery("UPDATE bank_loans").
		WithArgs("3.00000000", now, userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(9)))
	mock.ExpectExec("INSERT INTO bank_ledger").
		WithArgs(userID, "early_repay_temporary", int64(9), "0.00000000", "-3.00000000", "-3.00000000", "3.00000000", "0.00000000", sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(remaining_amount\\), 0\\)").
		WithArgs(userID, now).
		WillReturnRows(sqlmock.NewRows([]string{"available"}).AddRow("7.00000000"))
	mock.ExpectExec("UPDATE idempotency_records").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := service.RepayAtomic(context.Background(), userID, BankRepaySourceTemporary, 10, claim)
	require.NoError(t, err)
	require.Equal(t, "3.00000000", result.CreditSpent)
	require.Equal(t, "3.00000000", result.DebtReduced)
	require.Equal(t, "0.00000000", result.TemporaryDebt)
	require.Equal(t, float64(3), repo.consumeAmount)
	require.Equal(t, "bank-repay:91", repo.consumeReference.RequestID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRepayAtomicPermanentRejectsNegativeBalance(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	userID := int64(42)
	now := time.Date(2026, time.July, 21, 12, 0, 0, 0, time.UTC)
	dueAt := now.Add(48 * time.Hour)
	policy := DefaultBankPolicy()
	service := NewBankService(db, &temporaryCreditRepositoryRecorder{}, nil)
	claim := newAtomicClaimForTest(now.Add(24 * time.Hour))

	expectBankSettlementNoop(mock, policy, userID, "1.00000000", "4.00000000", dueAt, now)
	mock.ExpectBegin()
	expectBankPolicy(mock, policy)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("1.00000000", "4.00000000", dueAt))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(now))
	expectNoExpiredBankAdvance(mock, userID, now)
	mock.ExpectRollback()

	result, err := service.RepayAtomic(context.Background(), userID, BankRepaySourcePermanent, 10, claim)
	require.Nil(t, result)
	require.ErrorIs(t, err, ErrBankPermanentInsufficient)
	require.NoError(t, mock.ExpectationsWereMet())
}
