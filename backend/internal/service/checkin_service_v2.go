package service

import (
	"context"
	cryptorand "crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type CheckinPreference struct {
	AutoEnabled          bool       `json:"auto_enabled"`
	ConsentPolicyVersion string     `json:"consent_policy_version,omitempty"`
	ConsentFeeBps        int        `json:"consent_fee_bps"`
	ConsentedAt          *time.Time `json:"consented_at,omitempty"`
	ConsentValid         bool       `json:"consent_valid"`
	CurrentPolicyVersion string     `json:"current_policy_version"`
	CurrentFeeBps        int        `json:"current_fee_bps"`
}

func NewCheckinServiceV2(db *sql.DB, policyProvider DailyCheckinPolicyProvider, temporaryCreditService *TemporaryCreditService) *CheckinService {
	svc := NewCheckinService(db, policyProvider, temporaryCreditService)
	svc.policyV2 = true
	svc.randRead = cryptorand.Read
	return svc
}

func NewCheckinServiceV2WithClock(db *sql.DB, policyProvider DailyCheckinPolicyProvider, temporaryCreditService *TemporaryCreditService, now func() time.Time) *CheckinService {
	svc := NewCheckinServiceWithClock(db, policyProvider, temporaryCreditService, now)
	svc.policyV2 = true
	svc.randRead = cryptorand.Read
	return svc
}

// CheckInModeAtomic is the additive mode-aware entry point. CheckInAtomic keeps
// its historical direct-mode signature for callers that cannot be rewired yet.
func (s *CheckinService) CheckInModeAtomic(ctx context.Context, userID int64, mode CheckinMode, claim *IdempotencyAtomicClaim, expectedPolicy ...string) (*CheckinResult, error) {
	normalized, err := normalizeCheckinMode(mode)
	if err != nil {
		return nil, err
	}
	if claim == nil {
		return nil, ErrIdempotencyStoreUnavail
	}
	if !s.policyV2 {
		if normalized != CheckinModeDirect {
			return nil, ErrCheckinModeDisabled
		}
		return s.checkIn(ctx, userID, claim)
	}
	return s.checkInV2(ctx, userID, normalized, claim, expectedPolicy...)
}

func (s *CheckinService) AutoCheckInAtomic(ctx context.Context, userID int64, claim *IdempotencyAtomicClaim) (*CheckinResult, error) {
	return s.CheckInModeAtomic(ctx, userID, CheckinModeDirectAuto, claim)
}

func (s *CheckinService) GetPreference(ctx context.Context, userID int64) (*CheckinPreference, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, ErrCheckinPreferenceInvalid
	}
	_, extended, err := s.loadCheckinPolicyBundle(ctx, s.db)
	if err != nil {
		return nil, err
	}
	preference, err := loadCheckinPreference(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	return decorateCheckinPreference(preference, extended), nil
}

func (s *CheckinService) UpdatePreference(ctx context.Context, userID int64, autoEnabled, accepted bool, expectedPolicy string, expectedFeeBps int) (*CheckinPreference, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, ErrCheckinPreferenceInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin daily checkin preference transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	// Use the same user row lock as check-in. The policy is read only after the
	// lock is held, so a manual preference toggle cannot race an automatic claim.
	if err := lockCheckinUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	_, extended, err := s.loadCheckinPolicyBundle(ctx, tx)
	if err != nil {
		return nil, err
	}
	if autoEnabled && !accepted {
		return nil, ErrCheckinConsentRequired
	}
	if autoEnabled && (expectedPolicy != extended.ConsentVersion() || expectedFeeBps != extended.AutoFeeBps) {
		return nil, ErrCheckinPolicyVersionStale
	}
	var consentVersion string
	var consentFee int
	var consentedAt any
	if autoEnabled {
		consentVersion = extended.ConsentVersion()
		consentFee = extended.AutoFeeBps
		consentedAt = s.now().UTC()
	}
	_, err = tx.ExecContext(ctx, `
INSERT INTO daily_checkin_preferences
    (user_id, auto_enabled, consent_policy_version, consent_fee_bps, consented_at, updated_at)
VALUES ($1, $2, $3, $4, $5, NOW())
ON CONFLICT (user_id) DO UPDATE SET
    auto_enabled = EXCLUDED.auto_enabled,
    consent_policy_version = CASE WHEN EXCLUDED.auto_enabled THEN EXCLUDED.consent_policy_version ELSE daily_checkin_preferences.consent_policy_version END,
    consent_fee_bps = CASE WHEN EXCLUDED.auto_enabled THEN EXCLUDED.consent_fee_bps ELSE daily_checkin_preferences.consent_fee_bps END,
    consented_at = CASE WHEN EXCLUDED.auto_enabled THEN EXCLUDED.consented_at ELSE daily_checkin_preferences.consented_at END,
    updated_at = NOW()`, userID, autoEnabled, consentVersion, consentFee, consentedAt)
	if err != nil {
		return nil, fmt.Errorf("update daily checkin preference: %w", err)
	}
	preference, err := loadCheckinPreference(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit daily checkin preference: %w", err)
	}
	return decorateCheckinPreference(preference, extended), nil
}

func loadCheckinPreference(ctx context.Context, queryer sqlQueryer, userID int64) (CheckinPreference, error) {
	var preference CheckinPreference
	var consentedAt sql.NullTime
	err := queryer.QueryRowContext(ctx, `
SELECT auto_enabled, consent_policy_version, consent_fee_bps, consented_at
FROM daily_checkin_preferences WHERE user_id = $1`, userID).Scan(
		&preference.AutoEnabled, &preference.ConsentPolicyVersion, &preference.ConsentFeeBps, &consentedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return preference, nil
	}
	if err != nil {
		return CheckinPreference{}, fmt.Errorf("load daily checkin preference: %w", err)
	}
	if consentedAt.Valid {
		utc := consentedAt.Time.UTC()
		preference.ConsentedAt = &utc
	}
	return preference, nil
}

func decorateCheckinPreference(preference CheckinPreference, policy DailyCheckinPolicyV2) *CheckinPreference {
	preference.CurrentPolicyVersion = policy.ConsentVersion()
	preference.CurrentFeeBps = policy.AutoFeeBps
	// Consent records the fee ceiling the user agreed to ("at most N bps"), so a
	// lower actual fee is still within consent; only a fee above the agreed
	// ceiling requires re-consent.
	preference.ConsentValid = preference.AutoEnabled &&
		policy.AcceptsConsentVersion(preference.ConsentPolicyVersion) &&
		preference.ConsentFeeBps >= policy.AutoFeeBps
	return &preference
}

func (s *CheckinService) checkInV2(ctx context.Context, userID int64, mode CheckinMode, claim *IdempotencyAtomicClaim, expectedPolicy ...string) (*CheckinResult, error) {
	if mode != CheckinModeDirect && mode != CheckinModeDirectAuto {
		return nil, ErrCheckinModeRemoved
	}
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, ErrCheckinUserNotFound
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin daily checkin v2 transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if err := lockCheckinUser(ctx, tx, userID); err != nil {
		return nil, err
	}
	businessNow, err := s.sampleTransactionNow(ctx, tx)
	if err != nil {
		return nil, err
	}
	basePolicy, extended, err := s.loadCheckinPolicyBundle(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !basePolicy.Enabled {
		return nil, ErrDailyCheckinDisabled
	}
	period, err := checkinPeriodAt(extended, businessNow)
	if err != nil {
		return nil, err
	}

	existing, err := loadCheckinByPeriodOrDate(ctx, tx, userID, period.ID, period.Date)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if existing != nil {
		existingMode := existing.mode
		if existingMode == "" || existingMode == "legacy" {
			existingMode = CheckinModeDirect
		}
		if existingMode != mode {
			return nil, ErrCheckinAlreadyCompleted
		}
		grant, loadErr := loadCheckinV2Grant(ctx, tx, existing, period)
		if loadErr != nil {
			return nil, loadErr
		}
		result := newCheckinV2Result(existing, grant, period, true)
		if err := persistAtomicCheckinSuccess(ctx, tx, claim, result); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit replayed daily checkin v2: %w", err)
		}
		return result, nil
	}

	preference, err := loadCheckinPreference(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	// Fee ceiling semantics: a lower actual fee stays within the user's consent
	// (see decorateCheckinPreference); only a fee above the agreed ceiling forces
	// re-consent.
	consentValid := preference.AutoEnabled &&
		extended.AcceptsConsentVersion(preference.ConsentPolicyVersion) &&
		preference.ConsentFeeBps >= extended.AutoFeeBps
	if mode == CheckinModeDirectAuto && (!preference.AutoEnabled || !consentValid) {
		return nil, ErrCheckinConsentRequired
	}
	if mode == CheckinModeDirect && preference.AutoEnabled {
		return nil, ErrCheckinAutoModeConflict
	}

	providedVersion := ""
	if len(expectedPolicy) > 0 {
		providedVersion = strings.TrimSpace(expectedPolicy[0])
	}
	if providedVersion != "" && providedVersion != extended.ConsentVersion() {
		return nil, ErrCheckinPolicyVersionStale
	}

	lastPeriodEnd, lastStreak, err := loadLatestCheckinPeriod(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	streakDay := NextCheckinStreakFromPeriod(lastPeriodEnd, lastStreak, period.StartAt)
	rewardDay, baseReward, permanentReward, err := basePolicy.RewardForStreakAmounts(streakDay)
	if err != nil {
		return nil, err
	}
	if baseReward <= 0 {
		return nil, ErrCheckinRewardZero
	}

	multiplierBps := defaultCheckinRandomBps
	randomRuleVersion := ""
	superCost := 0.0
	rewardAmount := baseReward
	switch mode {
	case CheckinModeDirectAuto:
		fee := checkinBasisPointAmount(baseReward, extended.AutoFeeBps)
		rewardAmount = floor8(baseReward - fee)
	}
	if rewardAmount < 0 {
		return nil, ErrCheckinRewardZero
	}

	checkin, err := insertCheckinV2(ctx, tx, userID, period, streakDay, rewardDay, baseReward, permanentReward, rewardAmount, mode, multiplierBps, superCost, extended.AutoFeeBps, extended.ConsentVersion(), randomRuleVersion)
	if errors.Is(err, sql.ErrNoRows) {
		existing, loadErr := loadCheckinByPeriodOrDate(ctx, tx, userID, period.ID, period.Date)
		if loadErr != nil {
			return nil, loadErr
		}
		existingMode := existing.mode
		if existingMode == "" || existingMode == "legacy" {
			existingMode = CheckinModeDirect
		}
		if existingMode != mode {
			return nil, ErrCheckinAlreadyCompleted
		}
		grant, loadErr := loadCheckinV2Grant(ctx, tx, existing, period)
		if loadErr != nil {
			return nil, loadErr
		}
		result := newCheckinV2Result(existing, grant, period, true)
		if err := persistAtomicCheckinSuccess(ctx, tx, claim, result); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit raced daily checkin v2: %w", err)
		}
		return result, nil
	}
	if err != nil {
		return nil, err
	}
	if permanentReward > 0 {
		if err := addCheckinPermanentBalance(ctx, tx, userID, permanentReward); err != nil {
			return nil, err
		}
	}
	grant := &TemporaryCreditGrant{expiresAt: period.NextReset}
	if rewardAmount > 0 {
		checkinID := checkin.id
		grant, err = s.temporaryCreditService.CreateGrantTx(ctx, tx, CreateTemporaryCreditGrantInput{
			UserID:      userID,
			Source:      TemporaryCreditSourceCheckin,
			CheckinID:   &checkinID,
			Amount:      rewardAmount,
			businessNow: &businessNow,
		})
		if err != nil {
			return nil, fmt.Errorf("create daily checkin v2 temporary credit grant: %w", err)
		}
	}
	result := newCheckinV2Result(checkin, grant, period, false)
	if err := persistAtomicCheckinSuccess(ctx, tx, claim, result); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit daily checkin v2 transaction: %w", err)
	}
	s.invalidateCommittedCheckinCredit(ctx, userID, permanentReward > 0 || superCost > 0)
	return result, nil
}

func loadCheckinByPeriodOrDate(ctx context.Context, queryer sqlQueryer, userID int64, periodID, checkinDate string) (*persistedCheckin, error) {
	checkin := &persistedCheckin{}
	err := queryer.QueryRowContext(ctx, `
SELECT id, checkin_date::text, streak_day, reward_day, reward_amount, permanent_reward_amount,
       mode, business_period_key, period_start_at, period_end_at, base_reward_amount,
       multiplier_bps, super_cost, auto_fee_bps, policy_version, random_rule_version
FROM daily_checkins
WHERE user_id = $1 AND business_period_key = $2
ORDER BY id DESC
LIMIT 1`, userID, periodID).Scan(
		&checkin.id, &checkin.date, &checkin.streakDay, &checkin.rewardDay, &checkin.rewardAmount, &checkin.permanentRewardAmount,
		&checkin.mode, &checkin.periodID, &checkin.periodStartAt, &checkin.periodEndAt, &checkin.baseRewardAmount,
		&checkin.multiplierBps, &checkin.superCost, &checkin.autoFeeBps, &checkin.policyVersion, &checkin.randomRuleVersion,
	)
	if err != nil {
		return nil, err
	}
	return checkin, nil
}

func insertCheckinV2(ctx context.Context, tx *sql.Tx, userID int64, period CheckinPeriod, streakDay, rewardDay int, baseReward, permanentReward, rewardAmount float64, mode CheckinMode, multiplierBps int, superCost float64, autoFeeBps int, policyVersion, randomRuleVersion string) (*persistedCheckin, error) {
	checkin := &persistedCheckin{}
	err := tx.QueryRowContext(ctx, `
INSERT INTO daily_checkins
    (user_id, checkin_date, streak_day, reward_day, reward_amount, permanent_reward_amount,
     business_period_key, period_start_at, period_end_at, mode, base_reward_amount,
     multiplier_bps, super_cost, auto_fee_bps, policy_version, random_rule_version)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
ON CONFLICT DO NOTHING
RETURNING id, checkin_date::text, streak_day, reward_day, reward_amount, permanent_reward_amount,
          mode, business_period_key, period_start_at, period_end_at, base_reward_amount,
          multiplier_bps, super_cost, auto_fee_bps, policy_version, random_rule_version`,
		userID, period.Date, streakDay, rewardDay, formatLedgerAmount(rewardAmount), formatLedgerAmount(permanentReward),
		period.ID, period.StartAt, period.NextReset, mode, formatLedgerAmount(baseReward),
		multiplierBps, formatLedgerAmount(superCost), autoFeeBps, policyVersion, randomRuleVersion,
	).Scan(
		&checkin.id, &checkin.date, &checkin.streakDay, &checkin.rewardDay, &checkin.rewardAmount, &checkin.permanentRewardAmount,
		&checkin.mode, &checkin.periodID, &checkin.periodStartAt, &checkin.periodEndAt, &checkin.baseRewardAmount,
		&checkin.multiplierBps, &checkin.superCost, &checkin.autoFeeBps, &checkin.policyVersion, &checkin.randomRuleVersion,
	)
	if err != nil {
		return nil, err
	}
	return checkin, nil
}

func newCheckinV2Result(checkin *persistedCheckin, grant *TemporaryCreditGrant, period CheckinPeriod, already bool) *CheckinResult {
	return &CheckinResult{
		AlreadyCheckedIn:       already,
		CheckinDate:            checkin.date,
		StreakDay:              checkin.streakDay,
		RewardDay:              checkin.rewardDay,
		RewardAmount:           formatLedgerAmount(checkin.rewardAmount),
		PermanentRewardAmount:  formatLedgerAmount(checkin.permanentRewardAmount),
		TemporaryCreditGrantID: grant.ID,
		ExpiresAt:              grant.ExpiresAt().UTC(),
		Mode:                   checkin.mode,
		BusinessPeriodID:       checkin.periodID,
		PeriodStartAt:          checkin.periodStartAt.UTC(),
		NextResetAt:            checkin.periodEndAt.UTC(),
		BaseRewardAmount:       formatLedgerAmount(checkin.baseRewardAmount),
		MultiplierBps:          checkin.multiplierBps,
		SuperCost:              formatLedgerAmount(checkin.superCost),
		AutoFeeBps:             checkin.autoFeeBps,
		PolicyVersion:          checkin.policyVersion,
		RandomRuleVersion:      checkin.randomRuleVersion,
	}
}

func floor8(value float64) float64 {
	result, _ := decimal.NewFromFloat(value).Truncate(8).Float64()
	return result
}

// Multiply the decimal reward and integer basis points before quantizing;
// binary float multiplication can incorrectly discard the last ledger unit.
func checkinBasisPointAmount(value float64, bps int) float64 {
	result, _ := decimal.NewFromFloat(value).Mul(decimal.NewFromInt(int64(bps))).Div(decimal.NewFromInt(10000)).Truncate(8).Float64()
	return result
}

func (s *CheckinService) getStatusV2(ctx context.Context, userID int64, requestedMonth string) (*CheckinStatus, error) {
	if err := s.validateDependencies(); err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, ErrCheckinUserNotFound
	}
	now := s.now()
	monthStart, monthEnd, err := checkinMonthBounds(requestedMonth, now)
	if err != nil {
		return nil, err
	}
	policy, extended, err := s.loadCheckinPolicyBundle(ctx, s.db)
	if err != nil {
		return nil, err
	}
	period, err := checkinPeriodAt(extended, now)
	if err != nil {
		return nil, err
	}
	preference, err := loadCheckinPreference(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	decoratedPreference := decorateCheckinPreference(preference, extended)
	lastPeriodEnd, lastStreak, err := loadLatestCheckinPeriod(ctx, s.db, userID)
	if err != nil {
		return nil, err
	}
	calendar, monthlyRewardTotal, monthlyPermanentRewardTotal, err := loadCheckinCalendar(ctx, s.db, userID, monthStart, monthEnd)
	if err != nil {
		return nil, err
	}
	available, earliestExpiry, err := s.availableTemporaryCreditSummary(ctx, userID)
	if err != nil {
		return nil, err
	}
	if earliestExpiry != nil {
		utc := earliestExpiry.UTC()
		earliestExpiry = &utc
	}
	existing, err := loadCheckinByPeriodOrDate(ctx, s.db, userID, period.ID, period.Date)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	todayCheckedIn := existing != nil
	currentStreakDay := 0
	nextStreakDay := 1
	if todayCheckedIn {
		currentStreakDay = existing.streakDay
		nextStreakDay = existing.streakDay + 1
	} else if lastPeriodEnd != nil && lastPeriodEnd.UTC().Equal(period.StartAt.UTC()) {
		currentStreakDay = lastStreak
		nextStreakDay = lastStreak + 1
	}
	nextRewardDay, nextRewardAmount, nextPermanentRewardAmount, err := policy.RewardForStreakAmounts(nextStreakDay)
	if err != nil {
		return nil, err
	}
	periodStartUTC := period.StartAt.UTC()
	nextResetUTC := period.NextReset.UTC()
	status := &CheckinStatus{
		Enabled:                          policy.Enabled,
		TodayCheckedIn:                   todayCheckedIn,
		CurrentStreakDay:                 currentStreakDay,
		NextRewardDay:                    nextRewardDay,
		NextRewardAmount:                 formatLedgerAmount(nextRewardAmount),
		NextPermanentRewardAmount:        formatLedgerAmount(nextPermanentRewardAmount),
		TemporaryCreditAvailable:         formatLedgerAmount(available),
		TemporaryCreditEarliestExpiresAt: earliestExpiry,
		MonthlyRewardTotal:               formatLedgerAmount(monthlyRewardTotal),
		MonthlyPermanentRewardTotal:      formatLedgerAmount(monthlyPermanentRewardTotal),
		RewardTiers:                      dailyCheckinRewardTierStatuses(policy),
		Calendar:                         calendar,
		BusinessPeriodID:                 period.ID,
		PeriodStartAt:                    &periodStartUTC,
		NextResetAt:                      &nextResetUTC,
		Mode:                             CheckinModeDirect,
		AutoFeeBps:                       extended.AutoFeeBps,
		AutoEnabled:                      decoratedPreference.AutoEnabled,
		ConsentValid:                     decoratedPreference.ConsentValid,
		PolicyVersion:                    extended.ConsentVersion(),
	}
	if existing != nil {
		status.Mode = existing.mode
		if status.Mode == "" || status.Mode == "legacy" {
			status.Mode = CheckinModeDirect
		}
	} else if decoratedPreference.ConsentValid {
		status.Mode = CheckinModeDirectAuto
	}
	return status, nil
}

func loadLatestCheckinPeriod(ctx context.Context, queryer sqlQueryer, userID int64) (*time.Time, int, error) {
	var periodEnd time.Time
	var streakDay int
	err := queryer.QueryRowContext(ctx, `SELECT period_end_at, streak_day FROM daily_checkins WHERE user_id = $1 ORDER BY period_end_at DESC, id DESC LIMIT 1`, userID).Scan(&periodEnd, &streakDay)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("load latest daily checkin period: %w", err)
	}
	utc := periodEnd.UTC()
	return &utc, streakDay, nil
}

func loadCheckinV2Grant(ctx context.Context, tx *sql.Tx, checkin *persistedCheckin, period CheckinPeriod) (*TemporaryCreditGrant, error) {
	if checkin.rewardAmount == 0 {
		return &TemporaryCreditGrant{expiresAt: period.NextReset}, nil
	}
	return loadCheckinGrant(ctx, tx, checkin.id)
}

func (s *CheckinService) loadCheckinPolicyBundle(ctx context.Context, q sqlQueryer) (*DailyCheckinPolicy, DailyCheckinPolicyV2, error) {
	var enabled, maxDay, rewards, extended string
	err := q.QueryRowContext(ctx, `SELECT
 COALESCE((SELECT value FROM settings WHERE key=$1),''),
 COALESCE((SELECT value FROM settings WHERE key=$2),''),
 COALESCE((SELECT value FROM settings WHERE key=$3),''),
 COALESCE((SELECT value FROM settings WHERE key=$4),'')`, SettingKeyDailyCheckinEnabled, SettingKeyDailyCheckinMaxRewardDay, SettingKeyDailyCheckinRewardTiers, SettingKeyDailyCheckinPolicyV2).Scan(&enabled, &maxDay, &rewards, &extended)
	if err != nil {
		return nil, DailyCheckinPolicyV2{}, err
	}
	return parseCheckinPolicyBundle(map[string]string{SettingKeyDailyCheckinEnabled: enabled, SettingKeyDailyCheckinMaxRewardDay: maxDay, SettingKeyDailyCheckinRewardTiers: rewards, SettingKeyDailyCheckinPolicyV2: extended})
}
