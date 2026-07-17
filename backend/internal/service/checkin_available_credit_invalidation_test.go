package service

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestCheckinServiceCheckInInvalidatesAvailableCreditOnlyAfterCommit(t *testing.T) {
	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, beijingLocation)

	t.Run("committed checkin invalidates", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		creditSvc := NewTemporaryCreditServiceWithAvailableCreditInvalidator(
			&checkinTemporaryCreditRepositoryStub{available: 1.25},
			invalidator,
		)
		creditSvc.now = func() time.Time { return now }
		svc := NewCheckinServiceWithClock(db, checkinPolicyProviderStub{policy: enabledCheckinPolicy("1.25000000")}, creditSvc, func() time.Time { return now })

		mock.ExpectBegin()
		expectCheckinUserLock(mock, 42)
		mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").WithArgs(int64(42)).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("INSERT INTO daily_checkins").
			WithArgs(int64(42), "2026-07-13", 1, 1, "1.25000000").
			WillReturnRows(sqlmock.NewRows([]string{"id", "checkin_date", "streak_day", "reward_day", "reward_amount"}).
				AddRow(int64(7), "2026-07-13", 1, 1, "1.25000000"))
		mock.ExpectCommit()

		_, err = svc.checkIn(context.Background(), 42, nil)

		require.NoError(t, err)
		require.Equal(t, 1, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("rolled back checkin does not invalidate", func(t *testing.T) {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })

		invalidator := &temporaryCreditAvailableCreditInvalidatorStub{}
		creditSvc := NewTemporaryCreditServiceWithAvailableCreditInvalidator(&checkinTemporaryCreditRepositoryStub{}, invalidator)
		creditSvc.now = func() time.Time { return now }
		svc := NewCheckinServiceWithClock(db, checkinPolicyProviderStub{policy: enabledCheckinPolicy("1.25000000")}, creditSvc, func() time.Time { return now })

		mock.ExpectBegin()
		expectCheckinUserLock(mock, 42)
		mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").WithArgs(int64(42)).WillReturnError(sql.ErrNoRows)
		mock.ExpectQuery("INSERT INTO daily_checkins").
			WithArgs(int64(42), "2026-07-13", 1, 1, "1.25000000").
			WillReturnRows(sqlmock.NewRows([]string{"id", "checkin_date", "streak_day", "reward_day", "reward_amount"}).
				AddRow(int64(7), "2026-07-13", 1, 1, "1.25000000"))
		mock.ExpectCommit().WillReturnError(errors.New("commit failed"))

		_, err = svc.checkIn(context.Background(), 42, nil)

		require.Error(t, err)
		require.Equal(t, 0, invalidator.calls)
		require.NoError(t, mock.ExpectationsWereMet())
	})
}
