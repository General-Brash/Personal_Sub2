package service

import (
	"context"
	"database/sql"
	"net/http"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func expectBankPolicy(mock sqlmock.Sqlmock, policy BankPolicy) {
	mock.ExpectQuery(regexp.QuoteMeta("SELECT key, value FROM settings WHERE key IN ($1,$2,$3,$4,$5)")).
		WithArgs(
			SettingKeyBankAdvanceMinAmount,
			SettingKeyBankAdvanceMaxAmount,
			SettingKeyBankDebtGraceDays,
			SettingKeyBankDebtConversionRatio,
			SettingKeyBankExchangeRate,
		).
		WillReturnRows(sqlmock.NewRows([]string{"key", "value"}).
			AddRow(SettingKeyBankAdvanceMinAmount, formatLedgerAmount(policy.AdvanceMinAmount)).
			AddRow(SettingKeyBankAdvanceMaxAmount, formatLedgerAmount(policy.AdvanceMaxAmount)).
			AddRow(SettingKeyBankDebtGraceDays, policy.DebtGraceDays).
			AddRow(SettingKeyBankDebtConversionRatio, formatLedgerAmount(policy.DebtConversionRatio)).
			AddRow(SettingKeyBankExchangeRate, formatLedgerAmount(policy.ExchangeRate)))
}

func TestBankPolicyDefaultsAndValidation(t *testing.T) {
	policy := DefaultBankPolicy()
	require.Equal(t, float64(5), policy.AdvanceMinAmount)
	require.Equal(t, float64(20), policy.AdvanceMaxAmount)
	require.Equal(t, 3, policy.DebtGraceDays)
	require.Equal(t, float64(1), policy.DebtConversionRatio)
	require.Equal(t, float64(1), policy.ExchangeRate)
	require.NoError(t, policy.Validate())

	policy.AdvanceMaxAmount = 4
	require.ErrorIs(t, policy.Validate(), ErrBankPolicyInvalid)
}

func TestBankExchangeMaintenanceWindowBoundaries(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		hour    int
		minute  int
		second  int
		blocked bool
	}{
		{name: "23:54:59 allowed", hour: 23, minute: 54, second: 59},
		{name: "23:55:00 blocked", hour: 23, minute: 55, blocked: true},
		{name: "00:04:59 blocked", hour: 0, minute: 4, second: 59, blocked: true},
		{name: "00:05:00 allowed", hour: 0, minute: 5},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			at := time.Date(2026, time.July, 20, testCase.hour, testCase.minute, testCase.second, 0, beijingLocation).UTC()
			require.Equal(t, testCase.blocked, bankExchangeInMaintenanceWindow(at))
		})
	}

	require.Equal(t, http.StatusForbidden, infraerrors.Code(ErrBankExchangeMaintenanceWindow))
	require.Equal(t, "BANK_EXCHANGE_MAINTENANCE_WINDOW", infraerrors.Reason(ErrBankExchangeMaintenanceWindow))
	require.Equal(t, map[string]string{
		"timezone":     "Asia/Shanghai",
		"window_start": "23:55:00",
		"window_end":   "00:05:00",
	}, ErrBankExchangeMaintenanceWindow.Metadata)
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
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("30.00000000", "10.00000000", dueAt))
	expectBankPolicy(mock, policy)
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(insideNow))
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
	mock.ExpectQuery("UPDATE users").
		WithArgs("5.00000000", userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("15.00000000"))
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

func TestExchangeAtomicRejectsMaintenanceWindowInsideTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	userID := int64(42)
	blockedAt := time.Date(2026, time.July, 19, 23, 55, 0, 0, beijingLocation)
	policy := DefaultBankPolicy()
	repo := &temporaryCreditRepositoryRecorder{}
	service := NewBankService(db, repo, nil)
	claim := newAtomicClaimForTest(blockedAt.Add(24 * time.Hour))

	mock.ExpectBegin()
	expectBankPolicy(mock, policy)
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("30.00000000", "0.00000000", nil))
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(blockedAt.Add(-time.Second)))
	mock.ExpectCommit()

	mock.ExpectBegin()
	mock.ExpectQuery("SELECT balance, temporary_credit_debt, temporary_credit_debt_due_at").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "temporary_credit_debt", "temporary_credit_debt_due_at"}).
			AddRow("30.00000000", "0.00000000", nil))
	expectBankPolicy(mock, policy)
	mock.ExpectQuery(`SELECT clock_timestamp\(\)`).
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(blockedAt))
	mock.ExpectRollback()

	result, err := service.ExchangeAtomic(context.Background(), userID, 5, claim)

	require.Nil(t, result)
	require.ErrorIs(t, err, ErrBankExchangeMaintenanceWindow)
	require.Equal(t, http.StatusForbidden, infraerrors.Code(err))
	require.Nil(t, repo.created)
	require.NoError(t, mock.ExpectationsWereMet())
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
