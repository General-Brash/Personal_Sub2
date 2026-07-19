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

type checkinPolicyProviderStub struct {
	policy *DailyCheckinPolicy
	err    error
}

func (s checkinPolicyProviderStub) GetDailyCheckinPolicy(context.Context) (*DailyCheckinPolicy, error) {
	return s.policy, s.err
}

type checkinTemporaryCreditRepositoryStub struct {
	created        []CreateTemporaryCreditGrantInput
	available      float64
	earliestExpiry *time.Time
	summaryErr     error
}

func expectCheckinUserLock(mock sqlmock.Sqlmock, userID int64) {
	mock.ExpectQuery("SELECT id FROM users WHERE id = \\$1 AND deleted_at IS NULL FOR UPDATE").
		WithArgs(userID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(userID))
}

func (s *checkinTemporaryCreditRepositoryStub) CreateGrant(context.Context, TemporaryCreditGrant) (*TemporaryCreditGrant, error) {
	return nil, nil
}

func (s *checkinTemporaryCreditRepositoryStub) CreateGrantTx(_ context.Context, _ *sql.Tx, grant TemporaryCreditGrant) (*TemporaryCreditGrant, error) {
	s.created = append(s.created, CreateTemporaryCreditGrantInput{
		UserID:    grant.UserID,
		Source:    grant.Source,
		CheckinID: grant.CheckinID,
		Amount:    grant.Amount,
		Notes:     grant.Notes,
		GrantedBy: grant.GrantedBy,
	})
	grant.ID = 91
	grant.CreatedAt = time.Date(2026, time.July, 13, 9, 0, 0, 0, beijingLocation)
	return &grant, nil
}

func (s *checkinTemporaryCreditRepositoryStub) AvailableSummary(context.Context, int64) (float64, *time.Time, error) {
	return s.available, s.earliestExpiry, s.summaryErr
}

func (s *checkinTemporaryCreditRepositoryStub) ConsumeFEFO(context.Context, *sql.Tx, int64, float64, TemporaryCreditConsumptionReference) (float64, error) {
	return 0, nil
}

func enabledCheckinPolicy(amount string) *DailyCheckinPolicy {
	parsed, err := ParseStrictPositiveLedgerAmount(amount)
	if err != nil {
		panic(err)
	}
	return &DailyCheckinPolicy{
		Enabled:      true,
		MaxRewardDay: 1,
		RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: parsed}},
	}
}

func enabledCheckinPolicyWithPermanent(amount, permanentAmount string) *DailyCheckinPolicy {
	policy := enabledCheckinPolicy(amount)
	parsed, err := ParseStrictLedgerAmount(permanentAmount)
	if err != nil {
		panic(err)
	}
	policy.RewardTiers[0].PermanentAmount = parsed
	return policy
}

func expectCheckinInsert(mock sqlmock.Sqlmock, userID int64, date string, streakDay, rewardDay int, rewardAmount, permanentAmount string) {
	mock.ExpectQuery("INSERT INTO daily_checkins").
		WithArgs(userID, date, streakDay, rewardDay, rewardAmount, permanentAmount).
		WillReturnRows(sqlmock.NewRows([]string{"id", "checkin_date", "streak_day", "reward_day", "reward_amount", "permanent_reward_amount"}).
			AddRow(int64(7), date, streakDay, rewardDay, rewardAmount, permanentAmount))
}

func TestCheckinService_CheckInCreatesCheckinAndGrantInOneTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, beijingLocation)
	expiresAt := NextBeijingMidnight(now)
	creditRepo := &checkinTemporaryCreditRepositoryStub{
		available:      3.25,
		earliestExpiry: &expiresAt,
	}
	service := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: enabledCheckinPolicy("1.25000000")},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectCheckinInsert(mock, 42, "2026-07-13", 1, 1, "1.25000000", "0.00000000")
	mock.ExpectCommit()

	result, err := service.checkIn(context.Background(), 42, nil)
	require.NoError(t, err)
	require.False(t, result.AlreadyCheckedIn)
	require.Equal(t, "2026-07-13", result.CheckinDate)
	require.Equal(t, 1, result.StreakDay)
	require.Equal(t, 1, result.RewardDay)
	require.Equal(t, "1.25000000", result.RewardAmount)
	require.Equal(t, int64(91), result.TemporaryCreditGrantID)
	require.Len(t, creditRepo.created, 1)
	require.Equal(t, TemporaryCreditSourceCheckin, creditRepo.created[0].Source)
	require.Equal(t, int64(7), *creditRepo.created[0].CheckinID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_CheckInRollsBackWhenAtomicSuccessPersistenceFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, beijingLocation)
	creditRepo := &checkinTemporaryCreditRepositoryStub{}
	service := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: enabledCheckinPolicy("1.25000000")},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)
	coordinator := NewIdempotencyCoordinator(nil, DefaultIdempotencyConfig())
	claim := &IdempotencyAtomicClaim{
		coordinator:        coordinator,
		recordID:           71,
		requestFingerprint: "checkin-fingerprint",
		expiresAt:          now.Add(24 * time.Hour),
	}

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectCheckinInsert(mock, 42, "2026-07-13", 1, 1, "1.25000000", "0.00000000")
	mock.ExpectExec("UPDATE idempotency_records").
		WillReturnError(errors.New("idempotency persistence failed"))
	mock.ExpectRollback()

	_, err = service.CheckInAtomic(context.Background(), 42, claim)
	require.ErrorIs(t, err, ErrIdempotencyStoreUnavail)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_CheckInPreservesEightDecimalReward(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, beijingLocation)
	amount := 999999.99999999
	policy := &DailyCheckinPolicy{
		Enabled:      true,
		MaxRewardDay: 1,
		RewardTiers:  []DailyCheckinRewardTier{{Day: 1, Amount: amount}},
	}
	creditRepo := &checkinTemporaryCreditRepositoryStub{available: amount}
	service := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: policy},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectCheckinInsert(mock, 42, "2026-07-13", 1, 1, "999999.99999999", "0.00000000")
	mock.ExpectCommit()

	result, err := service.checkIn(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, "999999.99999999", result.RewardAmount)
	require.Len(t, creditRepo.created, 1)
	require.Equal(t, "999999.99999999", formatLedgerAmount(creditRepo.created[0].Amount))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_CheckInUsesCapturedBusinessInstantForGrantExpiry(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	beforeMidnight := time.Date(2026, time.July, 13, 23, 59, 59, 0, beijingLocation)
	afterMidnight := time.Date(2026, time.July, 14, 0, 0, 1, 0, beijingLocation)
	clockCalls := 0
	clock := func() time.Time {
		clockCalls++
		if clockCalls == 1 {
			return beforeMidnight
		}
		return afterMidnight
	}
	creditRepo := &checkinTemporaryCreditRepositoryStub{available: 1}
	service := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: enabledCheckinPolicy("1.00000000")},
		NewTemporaryCreditServiceWithClock(creditRepo, clock),
		clock,
	)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectCheckinInsert(mock, 42, "2026-07-13", 1, 1, "1.00000000", "0.00000000")
	mock.ExpectCommit()

	result, err := service.checkIn(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, NextBeijingMidnight(beforeMidnight).UTC(), result.ExpiresAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_CheckInSamplesDatabaseClockAfterUserLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	databaseNow := time.Date(2026, time.July, 17, 0, 0, 1, 0, beijingLocation)
	creditRepo := &checkinTemporaryCreditRepositoryStub{}
	service := NewCheckinService(
		db,
		checkinPolicyProviderStub{policy: enabledCheckinPolicy("1.00000000")},
		NewTemporaryCreditService(creditRepo),
	)
	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT clock_timestamp\\(\\)").
		WillReturnRows(sqlmock.NewRows([]string{"clock_timestamp"}).AddRow(databaseNow))
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnError(sql.ErrNoRows)
	expectCheckinInsert(mock, 42, "2026-07-17", 1, 1, "1.00000000", "0.00000000")
	mock.ExpectCommit()

	result, err := service.checkIn(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, "2026-07-17", result.CheckinDate)
	require.Equal(t, time.Date(2026, time.July, 17, 16, 0, 0, 0, time.UTC), result.ExpiresAt)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_CheckInReturnsExistingCheckinWithoutCreatingSecondGrant(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, beijingLocation)
	creditRepo := &checkinTemporaryCreditRepositoryStub{available: 1.25}
	disabledPolicy := enabledCheckinPolicy("1.25000000")
	disabledPolicy.Enabled = false
	service := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: disabledPolicy},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"checkin_date", "streak_day"}).AddRow("2026-07-13", 3))
	mock.ExpectQuery("SELECT id, checkin_date::text, streak_day, reward_day, reward_amount, permanent_reward_amount FROM daily_checkins").
		WithArgs(int64(42), "2026-07-13").
		WillReturnRows(sqlmock.NewRows([]string{"id", "checkin_date", "streak_day", "reward_day", "reward_amount", "permanent_reward_amount"}).
			AddRow(int64(7), "2026-07-13", 3, 1, "1.25000000", "0.75000000"))
	mock.ExpectQuery("SELECT id, expires_at FROM temporary_credit_grants").
		WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "expires_at"}).AddRow(int64(91), NextBeijingMidnight(now)))
	mock.ExpectCommit()

	result, err := service.checkIn(context.Background(), 42, nil)
	require.NoError(t, err)
	require.True(t, result.AlreadyCheckedIn)
	require.Equal(t, "0.75000000", result.PermanentRewardAmount)
	require.Equal(t, int64(91), result.TemporaryCreditGrantID)
	require.Empty(t, creditRepo.created)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_GetStatusReturnsShanghaiMonthCalendarAndNextReward(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 14, 9, 0, 0, 0, beijingLocation)
	expiresAt := NextBeijingMidnight(now)
	creditRepo := &checkinTemporaryCreditRepositoryStub{
		available:      3.5,
		earliestExpiry: &expiresAt,
	}
	policy := DefaultDailyCheckinPolicy()
	policy.Enabled = true
	service := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: &policy},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)

	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").
		WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"checkin_date", "streak_day"}).AddRow("2026-07-13", 7))
	mock.ExpectQuery("SELECT checkin_date::text, streak_day, reward_day, reward_amount, permanent_reward_amount FROM daily_checkins").
		WithArgs(int64(42), "2026-07-01", "2026-08-01").
		WillReturnRows(sqlmock.NewRows([]string{"checkin_date", "streak_day", "reward_day", "reward_amount", "permanent_reward_amount"}).
			AddRow("2026-07-13", 7, 7, "1.00000000", "0.50000000"))
	mock.ExpectQuery("SELECT COALESCE\\(SUM\\(reward_amount\\), 0\\), COALESCE\\(SUM\\(permanent_reward_amount\\), 0\\) FROM daily_checkins").
		WithArgs(int64(42), "2026-07-01", "2026-08-01").
		WillReturnRows(sqlmock.NewRows([]string{"temporary_sum", "permanent_sum"}).AddRow("7.00000000", "2.50000000"))

	status, err := service.GetStatus(context.Background(), 42, "2026-07")
	require.NoError(t, err)
	require.True(t, status.Enabled)
	require.False(t, status.TodayCheckedIn)
	require.Equal(t, 7, status.CurrentStreakDay)
	require.Equal(t, 7, status.NextRewardDay)
	require.Equal(t, "1.00000000", status.NextRewardAmount)
	require.Equal(t, "3.50000000", status.TemporaryCreditAvailable)
	require.Equal(t, expiresAt.UTC(), *status.TemporaryCreditEarliestExpiresAt)
	require.Equal(t, "7.00000000", status.MonthlyRewardTotal)
	require.Equal(t, "2.50000000", status.MonthlyPermanentRewardTotal)
	require.Len(t, status.Calendar, 1)
	require.Equal(t, "2026-07-13", status.Calendar[0].CheckinDate)
	require.Equal(t, "0.50000000", status.Calendar[0].PermanentRewardAmount)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_CheckInAddsPermanentRewardInSameTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, beijingLocation)
	creditRepo := &checkinTemporaryCreditRepositoryStub{}
	svc := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: enabledCheckinPolicyWithPermanent("1.25000000", "0.50000000")},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").WithArgs(int64(42)).WillReturnError(sql.ErrNoRows)
	expectCheckinInsert(mock, 42, "2026-07-13", 1, 1, "1.25000000", "0.50000000")
	mock.ExpectExec("UPDATE users SET balance = balance \\+ \\$1").
		WithArgs("0.50000000", int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := svc.checkIn(context.Background(), 42, nil)
	require.NoError(t, err)
	require.Equal(t, "0.50000000", result.PermanentRewardAmount)
	require.Len(t, creditRepo.created, 1)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCheckinService_CheckInRollsBackWhenPermanentRewardFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	now := time.Date(2026, time.July, 13, 12, 0, 0, 0, beijingLocation)
	creditRepo := &checkinTemporaryCreditRepositoryStub{}
	svc := NewCheckinServiceWithClock(
		db,
		checkinPolicyProviderStub{policy: enabledCheckinPolicyWithPermanent("1.25000000", "0.50000000")},
		NewTemporaryCreditServiceWithClock(creditRepo, func() time.Time { return now }),
		func() time.Time { return now },
	)

	mock.ExpectBegin()
	expectCheckinUserLock(mock, 42)
	mock.ExpectQuery("SELECT checkin_date::text, streak_day FROM daily_checkins").WithArgs(int64(42)).WillReturnError(sql.ErrNoRows)
	expectCheckinInsert(mock, 42, "2026-07-13", 1, 1, "1.25000000", "0.50000000")
	mock.ExpectExec("UPDATE users SET balance = balance \\+ \\$1").
		WithArgs("0.50000000", int64(42)).
		WillReturnError(errors.New("balance write failed"))
	mock.ExpectRollback()

	_, err = svc.checkIn(context.Background(), 42, nil)
	require.ErrorContains(t, err, "add daily checkin permanent balance")
	require.Empty(t, creditRepo.created)
	require.NoError(t, mock.ExpectationsWereMet())
}
