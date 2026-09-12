package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyDailyCheckinPolicyV2 = "daily_checkin_policy_v2"

	defaultCheckinAutoFeeBps = 500
	defaultCheckinRandomBps  = 10000
	checkinMaxMultiplierBps  = 1000000
)

var (
	ErrCheckinPreferenceInvalid  = infraerrors.BadRequest("INVALID_CHECKIN_PREFERENCE", "checkin preference is invalid")
	ErrCheckinConsentRequired    = infraerrors.Conflict("CHECKIN_CONSENT_REQUIRED", "current automatic-checkin consent is required")
	ErrCheckinAutoModeConflict   = infraerrors.Conflict("CHECKIN_AUTO_MODE_CONFLICT", "automatic check-in is enabled; normal and super modes are unavailable")
	ErrCheckinModeDisabled       = infraerrors.Conflict("CHECKIN_MODE_DISABLED", "check-in mode is disabled")
	ErrCheckinAlreadyCompleted   = infraerrors.Conflict("CHECKIN_ALREADY_COMPLETED", "check-in already completed for this period")
	ErrCheckinRewardZero         = infraerrors.Conflict("CHECKIN_REWARD_ZERO", "check-in reward is zero")
	ErrCheckinPermanentFunds     = infraerrors.Conflict("CHECKIN_SUPER_BALANCE_INSUFFICIENT", "spendable permanent balance is insufficient")
	ErrCheckinRandomUnavailable  = infraerrors.ServiceUnavailable("CHECKIN_RANDOM_UNAVAILABLE", "secure random source is unavailable")
	ErrCheckinPolicyVersionStale = infraerrors.Conflict("CHECKIN_POLICY_VERSION_STALE", "check-in policy version changed")
)

type CheckinMode string

const (
	CheckinModeDirect     CheckinMode = "direct"
	CheckinModeDirectAuto CheckinMode = "direct-auto"
	CheckinModeNormal     CheckinMode = "normal"
	CheckinModeSuper      CheckinMode = "super"
)

func normalizeCheckinMode(mode CheckinMode) (CheckinMode, error) {
	switch CheckinMode(strings.TrimSpace(string(mode))) {
	case CheckinModeDirect:
		return CheckinModeDirect, nil
	case CheckinModeDirectAuto:
		return CheckinModeDirectAuto, nil
	case CheckinModeNormal:
		return CheckinModeNormal, nil
	case CheckinModeSuper:
		return CheckinModeSuper, nil
	default:
		return "", infraerrors.BadRequest("INVALID_CHECKIN_MODE", "checkin mode is invalid")
	}
}

// DailyCheckinPolicy keeps the legacy reward-tier fields unchanged and carries
// the additive v2 policy only when it was explicitly loaded by a v2 service.
type DailyCheckinPolicyV2 struct {
	Version        string                 `json:"version"`
	RefreshTime    string                 `json:"refresh_time"`
	AutoFeeBps     int                    `json:"auto_fee_bps"`
	Normal         DailyCheckinRandomV2   `json:"normal"`
	Super          DailyCheckinSuperV2    `json:"super"`
	PendingRefresh *DailyCheckinPendingV2 `json:"pending_refresh,omitempty"`
	ReviewApproved bool                   `json:"-"`
	Configured     bool                   `json:"-"`
}

type DailyCheckinRandomV2 struct {
	Enabled bool `json:"enabled"`
	MinBps  int  `json:"min_bps"`
	MaxBps  int  `json:"max_bps"`
}

type DailyCheckinSuperV2 struct {
	Enabled bool    `json:"enabled"`
	MinBps  int     `json:"min_bps"`
	MaxBps  int     `json:"max_bps"`
	Cost    float64 `json:"cost"`
}

type DailyCheckinPendingV2 struct {
	RefreshTime string    `json:"refresh_time"`
	EffectiveAt time.Time `json:"effective_at"`
}

type checkinPolicyV2Wire struct {
	Version        string                 `json:"version"`
	RefreshTime    string                 `json:"refresh_time"`
	AutoFeeBps     int                    `json:"auto_fee_bps"`
	Normal         DailyCheckinRandomV2   `json:"normal"`
	Super          DailyCheckinSuperWire  `json:"super"`
	PendingRefresh *DailyCheckinPendingV2 `json:"pending_refresh,omitempty"`
	Reviewed       bool                   `json:"reviewed,omitempty"`
}

type DailyCheckinSuperWire struct {
	Enabled bool   `json:"enabled"`
	MinBps  int    `json:"min_bps"`
	MaxBps  int    `json:"max_bps"`
	Cost    string `json:"cost"`
}

func DefaultDailyCheckinPolicyV2() DailyCheckinPolicyV2 {
	return DailyCheckinPolicyV2{
		Version:     "checkin-v2-default",
		RefreshTime: "00:00",
		AutoFeeBps:  defaultCheckinAutoFeeBps,
		Normal:      DailyCheckinRandomV2{Enabled: false, MinBps: defaultCheckinRandomBps, MaxBps: defaultCheckinRandomBps},
		Super: DailyCheckinSuperV2{
			Enabled: false,
			MinBps:  defaultCheckinRandomBps,
			MaxBps:  defaultCheckinRandomBps,
			Cost:    0,
		},
	}
}

func (p DailyCheckinPolicyV2) Validate() error {
	if _, err := parseCheckinRefreshTime(p.RefreshTime); err != nil {
		return ErrDailyCheckinPolicyInvalid
	}
	if p.AutoFeeBps < 0 || p.AutoFeeBps > 10000 {
		return ErrDailyCheckinPolicyInvalid
	}
	if err := validateCheckinRandomRange(p.Normal.MinBps, p.Normal.MaxBps); err != nil {
		return err
	}
	if err := validateCheckinRandomRange(p.Super.MinBps, p.Super.MaxBps); err != nil {
		return err
	}
	if (p.Normal.Enabled || p.Super.Enabled) && !p.ReviewApproved {
		return ErrDailyCheckinPolicyInvalid
	}
	if err := validateCheckinV2LedgerAmount(p.Super.Cost); err != nil {
		return ErrDailyCheckinPolicyInvalid
	}
	if p.Super.Enabled && p.Super.Cost <= 0 {
		return ErrDailyCheckinPolicyInvalid
	}
	if p.PendingRefresh != nil {
		if _, err := parseCheckinRefreshTime(p.PendingRefresh.RefreshTime); err != nil {
			return ErrDailyCheckinPolicyInvalid
		}
		if p.PendingRefresh.EffectiveAt.IsZero() {
			return ErrDailyCheckinPolicyInvalid
		}
	}
	return nil
}

func validateCheckinV2LedgerAmount(amount float64) error {
	if amount < 0 || math.Abs(amount*1e8-math.Round(amount*1e8)) > 1e-6 {
		return ErrDailyCheckinPolicyInvalid
	}
	if _, err := normalizeLedgerAmount(amount); err != nil {
		return ErrDailyCheckinPolicyInvalid
	}
	return nil
}

func validateCheckinRandomRange(minBps, maxBps int) error {
	if minBps <= 0 || minBps > maxBps || maxBps > checkinMaxMultiplierBps {
		return ErrDailyCheckinPolicyInvalid
	}
	return nil
}

func parseCheckinRefreshTime(raw string) (int, error) {
	parts := strings.Split(strings.TrimSpace(raw), ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, ErrDailyCheckinPolicyInvalid
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, ErrDailyCheckinPolicyInvalid
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, ErrDailyCheckinPolicyInvalid
	}
	return hour*60 + minute, nil
}

func parseDailyCheckinPolicyV2(raw string) (DailyCheckinPolicyV2, error) {
	if strings.TrimSpace(raw) == "" {
		policy := DefaultDailyCheckinPolicyV2()
		policy.Configured = false
		return policy, nil
	}
	var wire checkinPolicyV2Wire
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return DailyCheckinPolicyV2{}, ErrDailyCheckinPolicyInvalid
	}
	cost, err := ParseStrictLedgerAmount(strings.TrimSpace(wire.Super.Cost))
	if err != nil || cost < 0 {
		return DailyCheckinPolicyV2{}, ErrDailyCheckinPolicyInvalid
	}
	policy := DailyCheckinPolicyV2{
		Version:        strings.TrimSpace(wire.Version),
		RefreshTime:    strings.TrimSpace(wire.RefreshTime),
		AutoFeeBps:     wire.AutoFeeBps,
		Normal:         wire.Normal,
		Super:          DailyCheckinSuperV2{Enabled: wire.Super.Enabled, MinBps: wire.Super.MinBps, MaxBps: wire.Super.MaxBps, Cost: cost},
		PendingRefresh: wire.PendingRefresh,
		ReviewApproved: wire.Reviewed,
		Configured:     true,
	}
	if policy.Version == "" {
		policy.Version = "checkin-v2"
	}
	if err := policy.Validate(); err != nil {
		return DailyCheckinPolicyV2{}, err
	}
	return policy, nil
}

func (p DailyCheckinPolicyV2) settingValue() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	wire := checkinPolicyV2Wire{
		Version:        p.Version,
		RefreshTime:    p.RefreshTime,
		AutoFeeBps:     p.AutoFeeBps,
		Normal:         p.Normal,
		Super:          DailyCheckinSuperWire{Enabled: p.Super.Enabled, MinBps: p.Super.MinBps, MaxBps: p.Super.MaxBps, Cost: formatLedgerAmount(p.Super.Cost)},
		PendingRefresh: p.PendingRefresh,
		Reviewed:       p.ReviewApproved,
	}
	raw, err := json.Marshal(wire)
	if err != nil {
		return "", fmt.Errorf("marshal daily checkin policy v2: %w", err)
	}
	return string(raw), nil
}

func (p DailyCheckinPolicyV2) EffectiveAt(now time.Time) DailyCheckinPolicyV2 {
	effective := p
	if p.PendingRefresh != nil && !now.Before(p.PendingRefresh.EffectiveAt) {
		effective.RefreshTime = p.PendingRefresh.RefreshTime
		effective.PendingRefresh = nil
	}
	return effective
}

type checkinPolicyRuleVersionWire struct {
	Enabled        bool                          `json:"enabled"`
	MaxRewardDay   int                           `json:"max_reward_day"`
	RewardTiers    []dailyCheckinRewardTierValue `json:"reward_tiers"`
	RefreshTime    string                        `json:"refresh_time"`
	AutoFeeBps     int                           `json:"auto_fee_bps"`
	Normal         DailyCheckinRandomV2          `json:"normal"`
	Super          DailyCheckinSuperV2           `json:"super"`
	PendingRefresh *DailyCheckinPendingV2        `json:"pending_refresh,omitempty"`
	Reviewed       bool                          `json:"reviewed"`
}

// EffectiveCheckinPolicyVersion is a deterministic fingerprint of every rule
// that can change rewards or consent. It intentionally includes the legacy base
// reward tiers so an old admin endpoint cannot advance the clock of the V2
// policy without invalidating consent.
func EffectiveCheckinPolicyVersion(base *DailyCheckinPolicy, extended DailyCheckinPolicyV2) string {
	tiers := make([]dailyCheckinRewardTierValue, 0)
	if base != nil {
		tiers = make([]dailyCheckinRewardTierValue, len(base.RewardTiers))
		for index, tier := range base.RewardTiers {
			tiers[index] = dailyCheckinRewardTierValue{
				Day:             tier.Day,
				Amount:          formatLedgerAmount(tier.Amount),
				PermanentAmount: formatLedgerAmount(tier.PermanentAmount),
			}
		}
	}
	wire := checkinPolicyRuleVersionWire{
		Enabled: base != nil && base.Enabled, MaxRewardDay: 0, RewardTiers: tiers,
		RefreshTime: extended.RefreshTime, AutoFeeBps: extended.AutoFeeBps,
		Normal: extended.Normal, Super: extended.Super,
		PendingRefresh: extended.PendingRefresh, Reviewed: extended.ReviewApproved,
	}
	if base != nil {
		wire.MaxRewardDay = base.MaxRewardDay
	}
	raw, _ := json.Marshal(wire)
	sum := sha256.Sum256(raw)
	return "checkin-v2-" + hex.EncodeToString(sum[:12])
}

func (p DailyCheckinPolicyV2) ConsentVersion() string {
	version := strings.TrimSpace(p.Version)
	if version == "" {
		version = "checkin-v2"
	}
	return fmt.Sprintf("%s:auto-fee:%d", version, p.AutoFeeBps)
}

type CheckinPeriod struct {
	ID        string    `json:"business_period_id"`
	StartAt   time.Time `json:"period_start_at"`
	NextReset time.Time `json:"next_reset_at"`
	Date      string    `json:"checkin_date"`
}

func checkinPeriodAt(policy DailyCheckinPolicyV2, now time.Time) (CheckinPeriod, error) {
	effective := policy.EffectiveAt(now)
	refreshMinute, err := parseCheckinRefreshTime(effective.RefreshTime)
	if err != nil {
		return CheckinPeriod{}, err
	}
	localNow := now.In(beijingLocation)
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), refreshMinute/60, refreshMinute%60, 0, 0, beijingLocation)
	if candidate.After(localNow) {
		candidate = candidate.AddDate(0, 0, -1)
	}
	if policy.PendingRefresh != nil && !now.Before(policy.PendingRefresh.EffectiveAt) {
		if policy.PendingRefresh.EffectiveAt.After(candidate) {
			candidate = policy.PendingRefresh.EffectiveAt.In(beijingLocation)
		}
	}
	next := time.Date(candidate.Year(), candidate.Month(), candidate.Day(), refreshMinute/60, refreshMinute%60, 0, 0, beijingLocation)
	if !next.After(candidate) {
		next = next.AddDate(0, 0, 1)
	}
	return CheckinPeriod{
		ID:        fmt.Sprintf("checkin:%d", candidate.UTC().Unix()),
		StartAt:   candidate,
		NextReset: next,
		Date:      candidate.Format("2006-01-02"),
	}, nil
}

// CurrentPeriod exposes the same non-overlapping period calculation used by
// check-in writes so the admin UI can preview the next refresh boundary.
func (p DailyCheckinPolicyV2) CurrentPeriod(now time.Time) (CheckinPeriod, error) {
	return checkinPeriodAt(p, now)
}

type AtomicSettingPolicyRepository interface {
	CompareAndSetMultiple(ctx context.Context, expected, updates map[string]string) (bool, error)
}

func checkinPolicySettingKeys() []string {
	return []string{SettingKeyDailyCheckinEnabled, SettingKeyDailyCheckinMaxRewardDay, SettingKeyDailyCheckinRewardTiers, SettingKeyDailyCheckinPolicyV2}
}

func parseCheckinPolicyBundle(values map[string]string) (*DailyCheckinPolicy, DailyCheckinPolicyV2, error) {
	base, err := parseDailyCheckinPolicySettings(values)
	if err != nil {
		return nil, DailyCheckinPolicyV2{}, err
	}
	extended, err := parseDailyCheckinPolicyV2(values[SettingKeyDailyCheckinPolicyV2])
	if err != nil {
		return nil, DailyCheckinPolicyV2{}, err
	}
	extended.Version = EffectiveCheckinPolicyVersion(base, extended)
	return base, extended, nil
}

func (s *SettingService) GetDailyCheckinPolicyV2(ctx context.Context) (*DailyCheckinPolicy, DailyCheckinPolicyV2, error) {
	if s == nil || s.settingRepo == nil {
		return nil, DailyCheckinPolicyV2{}, ErrDailyCheckinPolicyInvalid
	}
	values, err := s.settingRepo.GetMultiple(ctx, checkinPolicySettingKeys())
	if err != nil {
		return nil, DailyCheckinPolicyV2{}, err
	}
	return parseCheckinPolicyBundle(values)
}

// UpdateDailyCheckinPolicyV2 is the additive admin entry point. Refresh-time
// changes become pending until the current period ends; the existing admin
// update method keeps its legacy three-key contract untouched.
func (s *SettingService) UpdateDailyCheckinPolicyV2(ctx context.Context, policy *DailyCheckinPolicy, extended *DailyCheckinPolicyV2, expectedVersion ...string) error {
	if s == nil || s.settingRepo == nil || policy == nil || extended == nil {
		return ErrDailyCheckinPolicyInvalid
	}
	expected := ""
	if len(expectedVersion) > 0 {
		expected = strings.TrimSpace(expectedVersion[0])
	}
	if expected == "" {
		return ErrCheckinPolicyVersionStale
	}
	updates, err := policy.settingValues()
	if err != nil {
		return err
	}
	expectedValues, err := s.settingRepo.GetMultiple(ctx, checkinPolicySettingKeys())
	if err != nil {
		return err
	}
	for _, key := range checkinPolicySettingKeys() {
		if _, ok := expectedValues[key]; !ok {
			expectedValues[key] = ""
		}
	}
	_, current, err := parseCheckinPolicyBundle(expectedValues)
	if err != nil {
		return err
	}
	if expected != current.Version {
		return ErrCheckinPolicyVersionStale
	}
	now := time.Now()
	active := current.EffectiveAt(now)
	if extended.RefreshTime != active.RefreshTime {
		period, periodErr := checkinPeriodAt(current, now)
		if periodErr != nil {
			return periodErr
		}
		effectiveAt := period.NextReset
		extended.PendingRefresh = &DailyCheckinPendingV2{RefreshTime: extended.RefreshTime, EffectiveAt: effectiveAt}
		extended.RefreshTime = active.RefreshTime // retain the old clock until the agreed boundary
	} else {
		extended.PendingRefresh = active.PendingRefresh
	}
	extended.Version = EffectiveCheckinPolicyVersion(policy, *extended)
	raw, err := extended.settingValue()
	if err != nil {
		return err
	}
	updates[SettingKeyDailyCheckinPolicyV2] = raw
	atomicRepo, ok := s.settingRepo.(AtomicSettingPolicyRepository)
	if !ok {
		return fmt.Errorf("check-in policy requires an atomic settings repository")
	}
	applied, err := atomicRepo.CompareAndSetMultiple(ctx, expectedValues, updates)
	if err != nil {
		return fmt.Errorf("update daily checkin policy v2: %w", err)
	}
	if !applied {
		return ErrCheckinPolicyVersionStale
	}
	return nil
}
