package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrDailyCheckinDisabled = infraerrors.BadRequest("DAILY_CHECKIN_DISABLED", "daily checkin is disabled")
	ErrCheckinMonthInvalid  = infraerrors.BadRequest("INVALID_CHECKIN_MONTH", "checkin month must use YYYY-MM")
	ErrCheckinUserNotFound  = infraerrors.NotFound("USER_NOT_FOUND", "user not found")
)

type DailyCheckinPolicyProvider interface {
	GetDailyCheckinPolicy(ctx context.Context) (*DailyCheckinPolicy, error)
}

type DailyCheckinCalendarEntry struct {
	CheckinDate  string `json:"checkin_date"`
	StreakDay    int    `json:"streak_day"`
	RewardDay    int    `json:"reward_day"`
	RewardAmount string `json:"reward_amount"`
}

type CheckinStatus struct {
	Enabled                          bool                        `json:"enabled"`
	TodayCheckedIn                   bool                        `json:"today_checked_in"`
	CurrentStreakDay                 int                         `json:"current_streak_day"`
	NextRewardDay                    int                         `json:"next_reward_day"`
	NextRewardAmount                 string                      `json:"next_reward_amount"`
	TemporaryCreditAvailable         string                      `json:"temporary_credit_available"`
	TemporaryCreditEarliestExpiresAt *time.Time                  `json:"temporary_credit_earliest_expires_at"`
	MonthlyRewardTotal               string                      `json:"monthly_reward_total"`
	Calendar                         []DailyCheckinCalendarEntry `json:"calendar"`
}

type CheckinResult struct {
	AlreadyCheckedIn       bool      `json:"already_checked_in"`
	CheckinDate            string    `json:"checkin_date"`
	StreakDay              int       `json:"streak_day"`
	RewardDay              int       `json:"reward_day"`
	RewardAmount           string    `json:"reward_amount"`
	TemporaryCreditGrantID int64     `json:"temporary_credit_grant_id"`
	ExpiresAt              time.Time `json:"expires_at"`
}

type CheckinService struct {
	db                     *sql.DB
	policyProvider         DailyCheckinPolicyProvider
	temporaryCreditService *TemporaryCreditService
	now                    func() time.Time
	transactionNow         func() time.Time
}

func NewCheckinService(db *sql.DB, policyProvider DailyCheckinPolicyProvider, temporaryCreditService *TemporaryCreditService) *CheckinService {
	return &CheckinService{
		db:                     db,
		policyProvider:         policyProvider,
		temporaryCreditService: temporaryCreditService,
		now:                    time.Now,
	}
}

func NewCheckinServiceWithClock(db *sql.DB, policyProvider DailyCheckinPolicyProvider, temporaryCreditService *TemporaryCreditService, now func() time.Time) *CheckinService {
	if now == nil {
		now = time.Now
	}
	return &CheckinService{
		db:                     db,
		policyProvider:         policyProvider,
		temporaryCreditService: temporaryCreditService,
		now:                    now,
		transactionNow:         now,
	}
}

// CheckInAtomic persists the frozen seven-field success snapshot in the same
// transaction as the check-in and temporary-credit grant.
func (s *CheckinService) CheckInAtomic(ctx context.Context, userID int64, claim *IdempotencyAtomicClaim) (*CheckinResult, error) {
	if claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	return s.checkIn(ctx, userID, claim)
}

func (s *CheckinService) checkIn(ctx context.Context, userID int64, claim *IdempotencyAtomicClaim) (*CheckinResult, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "user id must be positive")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin daily checkin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockCheckinUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	businessNow, err := s.sampleTransactionNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	checkinDate := BeijingBusinessDate(businessNow)

	lastDate, lastStreak, err := loadLatestCheckin(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if lastDate != nil && *lastDate == checkinDate {
		checkin, loadErr := loadCheckinByDate(ctx, tx, userID, checkinDate)
		if loadErr != nil {
			return nil, loadErr
		}
		grant, loadErr := loadCheckinGrant(ctx, tx, checkin.id)
		if loadErr != nil {
			return nil, loadErr
		}
		result := newCheckinResult(checkin, grant, true)
		if err := persistAtomicCheckinSuccess(ctx, tx, claim, result); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit existing daily checkin response: %w", err)
		}
		s.temporaryCreditService.invalidateAvailableCredit(ctx, userID)
		return result, nil
	}

	policy, err := s.policyProvider.GetDailyCheckinPolicy(ctx)
	if err != nil {
		return nil, fmt.Errorf("get daily checkin policy: %w", err)
	}
	if policy == nil {
		return nil, ErrDailyCheckinPolicyInvalid
	}
	if !policy.Enabled {
		return nil, ErrDailyCheckinDisabled
	}

	streakDay := NextCheckinStreak(lastDate, lastStreak, businessNow)
	rewardDay, rewardAmount, err := policy.RewardForStreak(streakDay)
	if err != nil {
		return nil, err
	}
	checkin, err := insertCheckin(ctx, tx, userID, checkinDate, streakDay, rewardDay, rewardAmount)
	if errors.Is(err, sql.ErrNoRows) {
		checkin, err = loadCheckinByDate(ctx, tx, userID, checkinDate)
		if err != nil {
			return nil, err
		}
		grant, loadErr := loadCheckinGrant(ctx, tx, checkin.id)
		if loadErr != nil {
			return nil, loadErr
		}
		result := newCheckinResult(checkin, grant, true)
		if err := persistAtomicCheckinSuccess(ctx, tx, claim, result); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit raced daily checkin response: %w", err)
		}
		s.temporaryCreditService.invalidateAvailableCredit(ctx, userID)
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	checkinID := checkin.id
	grant, err := s.temporaryCreditService.CreateGrantTx(ctx, tx, CreateTemporaryCreditGrantInput{
		UserID:      userID,
		Source:      TemporaryCreditSourceCheckin,
		CheckinID:   &checkinID,
		Amount:      rewardAmount,
		businessNow: &businessNow,
	})
	if err != nil {
		return nil, fmt.Errorf("create daily checkin temporary credit grant: %w", err)
	}
	result := newCheckinResult(checkin, grant, false)
	if err := persistAtomicCheckinSuccess(ctx, tx, claim, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit daily checkin transaction: %w", err)
	}
	s.temporaryCreditService.invalidateAvailableCredit(ctx, userID)
	return result, nil
}

func newCheckinResult(checkin *persistedCheckin, grant *TemporaryCreditGrant, alreadyCheckedIn bool) *CheckinResult {
	return &CheckinResult{
		AlreadyCheckedIn:       alreadyCheckedIn,
		CheckinDate:            checkin.date,
		StreakDay:              checkin.streakDay,
		RewardDay:              checkin.rewardDay,
		RewardAmount:           formatLedgerAmount(checkin.rewardAmount),
		TemporaryCreditGrantID: grant.ID,
		ExpiresAt:              grant.ExpiresAt().UTC(),
	}
}

func persistAtomicCheckinSuccess(ctx context.Context, tx *sql.Tx, claim *IdempotencyAtomicClaim, result *CheckinResult) error {
	if claim == nil {
		return nil
	}
	return claim.PersistSuccess(ctx, tx, result)
}

func lockCheckinUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	var persistedID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&persistedID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCheckinUserNotFound
	}
	if err != nil {
		return fmt.Errorf("lock daily checkin user: %w", err)
	}
	return nil
}

func (s *CheckinService) sampleTransactionNow(ctx context.Context, tx *sql.Tx) (time.Time, error) {
	if s.transactionNow != nil {
		return s.transactionNow(), nil
	}
	var now time.Time
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("sample daily checkin database clock: %w", err)
	}
	return now, nil
}

func (s *CheckinService) GetStatus(ctx context.Context, userID int64, requestedMonth string) (*CheckinStatus, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, infraerrors.BadRequest("INVALID_USER_ID", "user id must be positive")
	}

	now := s.now()
	monthStart, monthEnd, err := checkinMonthBounds(requestedMonth, now)
	if err != nil {
		return nil, err
	}
	policy, err := s.policyProvider.GetDailyCheckinPolicy(ctx)
	if err != nil {
		return nil, fmt.Errorf("get daily checkin policy: %w", err)
	}
	if policy == nil {
		return nil, ErrDailyCheckinPolicyInvalid
	}
	lastDate, lastStreak, err := loadLatestCheckin(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	calendar, monthlyRewardTotal, err := loadCheckinCalendar(ctx, s.db, userID, monthStart, monthEnd)
	if err != nil {
		return nil, err
	}
	available, earliestExpiry, err := s.availableTemporaryCreditSummary(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get temporary credit summary: %w", err)
	}
	if earliestExpiry != nil {
		utc := earliestExpiry.UTC()
		earliestExpiry = &utc
	}

	today := BeijingBusinessDate(now)
	previousDay := now.In(beijingLocation).AddDate(0, 0, -1).Format("2006-01-02")
	todayCheckedIn := lastDate != nil && *lastDate == today
	currentStreakDay := 0
	nextStreakDay := 1
	if todayCheckedIn {
		currentStreakDay = lastStreak
		nextStreakDay = lastStreak + 1
	} else if lastDate != nil && *lastDate == previousDay {
		currentStreakDay = lastStreak
		nextStreakDay = lastStreak + 1
	}
	nextRewardDay, nextRewardAmount, err := policy.RewardForStreak(nextStreakDay)
	if err != nil {
		return nil, err
	}

	return &CheckinStatus{
		Enabled:                          policy.Enabled,
		TodayCheckedIn:                   todayCheckedIn,
		CurrentStreakDay:                 currentStreakDay,
		NextRewardDay:                    nextRewardDay,
		NextRewardAmount:                 formatLedgerAmount(nextRewardAmount),
		TemporaryCreditAvailable:         formatLedgerAmount(available),
		TemporaryCreditEarliestExpiresAt: earliestExpiry,
		MonthlyRewardTotal:               formatLedgerAmount(monthlyRewardTotal),
		Calendar:                         calendar,
	}, nil
}

func (s *CheckinService) validateDependencies() error {
	if s == nil || s.db == nil {
		return errors.New("daily checkin database is nil")
	}
	if s.policyProvider == nil {
		return errors.New("daily checkin policy provider is nil")
	}
	if s.temporaryCreditService == nil {
		return errors.New("temporary credit service is nil")
	}
	return nil
}

func (s *CheckinService) availableTemporaryCreditSummary(ctx context.Context, userID int64) (float64, *time.Time, error) {
	if s.temporaryCreditService.repo == nil {
		return 0, nil, errors.New("temporary credit repository is nil")
	}
	return s.temporaryCreditService.repo.AvailableSummary(ctx, userID)
}

type sqlQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type persistedCheckin struct {
	id           int64
	date         string
	streakDay    int
	rewardDay    int
	rewardAmount float64
}

func loadLatestCheckin(ctx context.Context, queryer sqlQueryer, userID int64) (*string, int, error) {
	var date string
	var streakDay int
	err := queryer.QueryRowContext(ctx, `SELECT checkin_date::text, streak_day FROM daily_checkins WHERE user_id = $1 ORDER BY checkin_date DESC LIMIT 1`, userID).Scan(&date, &streakDay)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("load latest daily checkin: %w", err)
	}
	return &date, streakDay, nil
}

func insertCheckin(ctx context.Context, tx *sql.Tx, userID int64, checkinDate string, streakDay, rewardDay int, rewardAmount float64) (*persistedCheckin, error) {
	checkin := &persistedCheckin{}
	err := tx.QueryRowContext(ctx, `
INSERT INTO daily_checkins (user_id, checkin_date, streak_day, reward_day, reward_amount)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (user_id, checkin_date) DO NOTHING
RETURNING id, checkin_date::text, streak_day, reward_day, reward_amount`,
		userID, checkinDate, streakDay, rewardDay, formatLedgerAmount(rewardAmount),
	).Scan(&checkin.id, &checkin.date, &checkin.streakDay, &checkin.rewardDay, &checkin.rewardAmount)
	if err != nil {
		return nil, err
	}
	return checkin, nil
}

func loadCheckinByDate(ctx context.Context, tx *sql.Tx, userID int64, checkinDate string) (*persistedCheckin, error) {
	checkin := &persistedCheckin{}
	err := tx.QueryRowContext(ctx, `SELECT id, checkin_date::text, streak_day, reward_day, reward_amount FROM daily_checkins WHERE user_id = $1 AND checkin_date = $2`, userID, checkinDate).
		Scan(&checkin.id, &checkin.date, &checkin.streakDay, &checkin.rewardDay, &checkin.rewardAmount)
	if err != nil {
		return nil, fmt.Errorf("load existing daily checkin: %w", err)
	}
	return checkin, nil
}

func loadCheckinGrant(ctx context.Context, tx *sql.Tx, checkinID int64) (*TemporaryCreditGrant, error) {
	grant := &TemporaryCreditGrant{}
	err := tx.QueryRowContext(ctx, `SELECT id, expires_at FROM temporary_credit_grants WHERE checkin_id = $1`, checkinID).
		Scan(&grant.ID, &grant.expiresAt)
	if err != nil {
		return nil, fmt.Errorf("load existing daily checkin grant: %w", err)
	}
	return grant, nil
}

func loadCheckinCalendar(ctx context.Context, queryer sqlQueryer, userID int64, monthStart, monthEnd string) ([]DailyCheckinCalendarEntry, float64, error) {
	rows, err := queryer.QueryContext(ctx, `
SELECT checkin_date::text, streak_day, reward_day, reward_amount
FROM daily_checkins
WHERE user_id = $1 AND checkin_date >= $2 AND checkin_date < $3
ORDER BY checkin_date ASC`, userID, monthStart, monthEnd)
	if err != nil {
		return nil, 0, fmt.Errorf("load daily checkin calendar: %w", err)
	}
	defer rows.Close()

	calendar := make([]DailyCheckinCalendarEntry, 0)
	for rows.Next() {
		entry := DailyCheckinCalendarEntry{}
		var rewardAmount float64
		if err := rows.Scan(&entry.CheckinDate, &entry.StreakDay, &entry.RewardDay, &rewardAmount); err != nil {
			return nil, 0, fmt.Errorf("scan daily checkin calendar: %w", err)
		}
		entry.RewardAmount = formatLedgerAmount(rewardAmount)
		calendar = append(calendar, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate daily checkin calendar: %w", err)
	}

	var monthlyRewardTotal float64
	err = queryer.QueryRowContext(ctx, `SELECT COALESCE(SUM(reward_amount), 0) FROM daily_checkins WHERE user_id = $1 AND checkin_date >= $2 AND checkin_date < $3`, userID, monthStart, monthEnd).
		Scan(&monthlyRewardTotal)
	if err != nil {
		return nil, 0, fmt.Errorf("sum monthly daily checkin rewards: %w", err)
	}
	return calendar, monthlyRewardTotal, nil
}

func checkinMonthBounds(requestedMonth string, now time.Time) (string, string, error) {
	month := strings.TrimSpace(requestedMonth)
	if month == "" {
		businessNow := now.In(beijingLocation)
		month = businessNow.Format("2006-01")
	}
	parsed, err := time.ParseInLocation("2006-01", month, beijingLocation)
	if err != nil || parsed.Format("2006-01") != month {
		return "", "", ErrCheckinMonthInvalid
	}
	return parsed.Format("2006-01-02"), parsed.AddDate(0, 1, 0).Format("2006-01-02"), nil
}
