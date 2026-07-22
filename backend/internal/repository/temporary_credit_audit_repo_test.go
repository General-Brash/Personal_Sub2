package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestTemporaryCreditAuditRepositoryUsesStableSortFixedAmountsAndUTC(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	location := time.FixedZone("CST", 8*60*60)
	createdAt := time.Date(2026, time.July, 16, 23, 0, 0, 0, location)
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM temporary_credit_grants WHERE user_id = \\$1").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectQuery(`(?s)WHEN available_at > clock_timestamp\(\) THEN 'unused'.*WHEN remaining_amount = 0 THEN 'depleted'.*WHEN expires_at <= clock_timestamp\(\) THEN 'expired'.*ORDER BY created_at DESC, id DESC`).
		WithArgs(int64(42), 20, int64(20)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "source", "checkin_id", "amount", "remaining_amount",
			"available_at", "expires_at", "status", "notes", "granted_by", "created_at", "updated_at",
		}).
			AddRow(9, 42, "admin_grant", nil, "2.00000000", "1.50000000", createdAt, createdAt.Add(time.Hour), "active", "campaign", 99, createdAt, createdAt).
			AddRow(8, 42, "checkin", 7, "1.00000000", "0.00000000", createdAt, createdAt.Add(time.Hour), "depleted", "", nil, createdAt, createdAt))

	repo := NewTemporaryCreditAuditRepository(db)
	items, total, err := repo.ListByUser(context.Background(), 42, 2, 20)

	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, items, 2)
	require.Equal(t, int64(9), items[0].ID)
	require.Equal(t, "2.00000000", items[0].Amount)
	require.Equal(t, "1.50000000", items[0].RemainingAmount)
	require.Equal(t, int64(99), *items[0].GrantedBy)
	require.Nil(t, items[0].CheckinID)
	require.Equal(t, time.UTC, items[0].ExpiresAt.Location())
	require.Equal(t, time.UTC, items[0].AvailableAt.Location())
	require.Equal(t, "active", string(items[0].Status))
	require.Equal(t, time.UTC, items[0].CreatedAt.Location())
	require.Equal(t, "", items[1].Notes)
	require.Equal(t, int64(7), *items[1].CheckinID)
	require.Nil(t, items[1].GrantedBy)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTemporaryCreditAuditRepositoryReturnsEmptyArray(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM temporary_credit_grants WHERE user_id = \\$1").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("ORDER BY created_at DESC, id DESC").
		WithArgs(int64(42), 20, int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "source", "checkin_id", "amount", "remaining_amount",
			"available_at", "expires_at", "status", "notes", "granted_by", "created_at", "updated_at",
		}))

	items, total, err := NewTemporaryCreditAuditRepository(db).ListByUser(context.Background(), 42, 1, 20)
	require.NoError(t, err)
	require.Zero(t, total)
	require.NotNil(t, items)
	require.Empty(t, items)
	require.NoError(t, mock.ExpectationsWereMet())
}
