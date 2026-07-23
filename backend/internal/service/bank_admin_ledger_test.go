package service

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestListAdminLedgerReturnsEconomicTransactionAmount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM bank_ledger").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(6))
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{
		"id", "user_id", "username", "email", "operation", "loan_id", "grant_id",
		"permanent_delta", "temporary_delta", "debt_delta", "debt_before", "debt_after",
		"permanent_balance_before", "permanent_balance_after",
		"temporary_balance_before", "temporary_balance_after", "metadata", "created_at",
	}).
		AddRow(1, 42, "alice", "alice@example.test", "exchange", nil, 7,
			"-50.00000000", "95.00000000", "0.00000000", "0.00000000", "0.00000000",
			"100.00000000", "50.00000000", "0.00000000", "95.00000000", []byte(`{}`), now).
		AddRow(2, 42, "alice", "alice@example.test", "advance", 8, 9,
			"0.00000000", "20.00000000", "20.00000000", "0.00000000", "20.00000000",
			"50.00000000", "50.00000000", "95.00000000", "115.00000000", []byte(`{}`), now).
		AddRow(3, 42, "alice", "alice@example.test", "early_repay_permanent", 8, nil,
			"-2.00000000", "0.00000000", "-4.00000000", "20.00000000", "16.00000000",
			"50.00000000", "48.00000000", "115.00000000", "115.00000000", []byte(`{}`), now).
		AddRow(4, 42, "alice", "alice@example.test", "permanent_settlement", 8, nil,
			"-8.00000000", "0.00000000", "-16.00000000", "16.00000000", "0.00000000",
			"48.00000000", "40.00000000", "115.00000000", "115.00000000", []byte(`{}`), now).
		AddRow(5, 42, "alice", "alice@example.test", "early_repay_temporary", 8, nil,
			"0.00000000", "-3.00000000", "-6.00000000", "16.00000000", "10.00000000",
			"40.00000000", "40.00000000", "115.00000000", "112.00000000", []byte(`{}`), now).
		AddRow(6, 42, "alice", "alice@example.test", "unused_advance_repayment", 8, 9,
			"0.00000000", "-8.00000000", "-6.00000000", "10.00000000", "4.00000000",
			"40.00000000", "40.00000000", "112.00000000", "104.00000000", []byte(`{}`), now)
	mock.ExpectQuery("SELECT ledger.id").
		WithArgs(adminBankLedgerPageSize, 0).
		WillReturnRows(rows)

	items, total, err := NewBankService(db, nil, nil).ListAdminLedger(context.Background(), 0, 1)
	require.NoError(t, err)
	require.Equal(t, int64(6), total)
	require.Len(t, items, 6)
	require.Equal(t, "50.00000000", items[0].TransactionAmount)
	require.Equal(t, "bank:1", items[0].RowID)
	require.Equal(t, "bank", items[0].Source)
	require.Empty(t, items[0].Currency)
	require.Equal(t, LedgerUnitCredit, items[0].Unit)
	require.Equal(t, "20.00000000", items[1].TransactionAmount)
	require.Equal(t, "4.00000000", items[2].TransactionAmount)
	require.Equal(t, "16.00000000", items[3].TransactionAmount)
	require.Equal(t, "6.00000000", items[4].TransactionAmount)
	require.Equal(t, "6.00000000", items[5].TransactionAmount)
	require.NoError(t, mock.ExpectationsWereMet())
}
