package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestListLedgerPaginatesCurrentUserWithStableOrdering(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		page       int
		offset     int64
		rowIDs     []int64
		wantFirst  int64
		wantLength int
	}{
		{name: "first page", page: 1, offset: 0, rowIDs: []int64{7, 6, 5, 4, 3}, wantFirst: 7, wantLength: 5},
		{name: "second page", page: 2, offset: 5, rowIDs: []int64{2, 1}, wantFirst: 2, wantLength: 2},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })

			const userID int64 = 42
			mock.ExpectQuery(`SELECT COUNT\(\*\) FROM bank_ledger WHERE user_id = \$1`).
				WithArgs(userID).
				WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))
			mock.ExpectQuery(`(?s)SELECT id, operation.*FROM bank_ledger.*WHERE user_id = \$1.*ORDER BY created_at DESC, id DESC.*LIMIT \$2 OFFSET \$3`).
				WithArgs(userID, UserBankLedgerPageSize, testCase.offset).
				WillReturnRows(newBankLedgerRows(testCase.rowIDs...))

			items, total, err := NewBankService(db, nil, nil).ListLedger(context.Background(), userID, testCase.page)

			require.NoError(t, err)
			require.Equal(t, int64(7), total)
			require.Len(t, items, testCase.wantLength)
			require.Equal(t, testCase.wantFirst, items[0].ID)
			require.Equal(t, "bank:"+strconv.FormatInt(testCase.wantFirst, 10), items[0].RowID)
			require.Equal(t, "bank", items[0].Source)
			require.Empty(t, items[0].Currency)
			require.Equal(t, LedgerUnitCredit, items[0].Unit)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestListLedgerReturnsEmptyPageBeyondTotalWithoutLargeOffsetQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const userID int64 = 42
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM bank_ledger WHERE user_id = \$1`).
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(7))

	items, total, err := NewBankService(db, nil, nil).ListLedger(context.Background(), userID, int(^uint(0)>>1))

	require.NoError(t, err)
	require.Equal(t, int64(7), total)
	require.NotNil(t, items)
	require.Empty(t, items)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestListLedgerRejectsInvalidUserAndPage(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	svc := NewBankService(db, nil, nil)

	items, total, err := svc.ListLedger(context.Background(), 0, 1)
	require.ErrorIs(t, err, ErrUserNotFound)
	require.Nil(t, items)
	require.Zero(t, total)

	items, total, err = svc.ListLedger(context.Background(), 42, 0)
	require.ErrorIs(t, err, ErrBankLedgerPageInvalid)
	require.Nil(t, items)
	require.Zero(t, total)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newBankLedgerRows(ids ...int64) *sqlmock.Rows {
	rows := sqlmock.NewRows([]string{
		"id", "operation", "loan_id", "grant_id", "permanent_delta", "temporary_delta",
		"debt_delta", "debt_before", "debt_after", "permanent_balance_before",
		"permanent_balance_after", "temporary_balance_before", "temporary_balance_after",
		"metadata", "created_at",
	})
	createdAt := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	for _, id := range ids {
		rows.AddRow(
			id, "exchange", nil, nil, "-1.00000000", "2.00000000",
			"0.00000000", "0.00000000", "0.00000000", "10.00000000",
			"9.00000000", "20.00000000", "22.00000000", []byte(`{"rate":"2.00000000"}`), createdAt,
		)
	}
	return rows
}
