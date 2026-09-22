package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SettingKeyDailyCheckinPolicyV2 = "daily_checkin_policy_v2"

	defaultCheckinAutoFeeBps               = 500
	defaultCheckinRandomBps                = 10000
	checkinMaxMultiplierBps                = 1000000
	checkinPolicyCanonicalizationVersionV1 = "checkin-effective-v1"
)

var (
	ErrCheckinPreferenceInvalid  = infraerrors.BadRequest("INVALID_CHECKIN_PREFERENCE", "checkin preference is invalid")
	ErrCheckinConsentRequired    = infraerrors.Conflict("CHECKIN_CONSENT_REQUIRED", "current automatic-checkin consent is required")
	ErrCheckinModeRemoved        = infraerrors.Conflict("CHECKIN_MODE_REMOVED", "check-in game modes have been retired; use direct check-in")
	ErrCheckinAutoModeConflict   = infraerrors.Conflict("CHECKIN_AUTO_MODE_CONFLICT", "automatic check-in is enabled; manual direct check-in is unavailable")
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
	case CheckinModeNormal, CheckinModeSuper:
		return "", ErrCheckinModeRemoved
	default:
		return "", infraerrors.BadRequest("INVALID_CHECKIN_MODE", "checkin mode is invalid")
	}
}

// DailyCheckinPolicyV2 keeps the legacy normal/super/reviewed fields readable
// for old settings and history, but those fields are no longer active policy.
type DailyCheckinPolicyV2 struct {
	Version              string                        `json:"version"`
	RefreshTime          string                        `json:"refresh_time"`
	AutoFeeBps           int                           `json:"auto_fee_bps"`
	Normal               DailyCheckinRandomV2          `json:"normal"`
	Super                DailyCheckinSuperV2           `json:"super"`
	PendingRefresh       *DailyCheckinPendingV2        `json:"pending_refresh,omitempty"`
	RefreshEffectiveAt   *time.Time                    `json:"-"`
	ReviewApproved       bool                          `json:"-"`
	Configured           bool                          `json:"-"`
	ConsentCompatibility []checkinConsentCompatibility `json:"-"`
	legacyGameFields     bool                          `json:"-"`
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
	Version              string                        `json:"version"`
	RefreshTime          string                        `json:"refresh_time"`
	AutoFeeBps           *int                          `json:"auto_fee_bps"`
	Normal               json.RawMessage               `json:"normal"`
	Super                json.RawMessage               `json:"super"`
	PendingRefresh       *DailyCheckinPendingV2        `json:"pending_refresh,omitempty"`
	RefreshEffectiveAt   *time.Time                    `json:"refresh_effective_at,omitempty"`
	Reviewed             json.RawMessage               `json:"reviewed"`
	ConsentCompatibility []checkinConsentCompatibility `json:"consent_compatibility,omitempty"`
}

type checkinPolicyV2StoredWire struct {
	Version              string                        `json:"version"`
	RefreshTime          string                        `json:"refresh_time"`
	AutoFeeBps           int                           `json:"auto_fee_bps"`
	PendingRefresh       *DailyCheckinPendingV2        `json:"pending_refresh,omitempty"`
	RefreshEffectiveAt   *time.Time                    `json:"refresh_effective_at,omitempty"`
	ConsentCompatibility []checkinConsentCompatibility `json:"consent_compatibility,omitempty"`
}

type checkinConsentCompatibility struct {
	ConsentVersion string `json:"consent_version"`
	PolicyVersion  string `json:"policy_version"`
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
	if wire.AutoFeeBps == nil {
		return DailyCheckinPolicyV2{}, ErrDailyCheckinPolicyInvalid
	}

	// These fields are intentionally best-effort compatibility reads. Their
	// values no longer affect runtime rewards, consent, or validation, and they
	// are omitted by settingValue when the policy is saved again.
	var normal DailyCheckinRandomV2
	if len(wire.Normal) > 0 {
		_ = json.Unmarshal(wire.Normal, &normal)
	}
	var superWire DailyCheckinSuperWire
	if len(wire.Super) > 0 {
		_ = json.Unmarshal(wire.Super, &superWire)
	}
	cost := 0.0
	if parsed, parseErr := ParseStrictLedgerAmount(strings.TrimSpace(superWire.Cost)); parseErr == nil && parsed >= 0 {
		cost = parsed
	}
	reviewed := false
	if len(wire.Reviewed) > 0 {
		_ = json.Unmarshal(wire.Reviewed, &reviewed)
	}
	policy := DailyCheckinPolicyV2{
		Version:              strings.TrimSpace(wire.Version),
		RefreshTime:          strings.TrimSpace(wire.RefreshTime),
		AutoFeeBps:           *wire.AutoFeeBps,
		Normal:               normal,
		Super:                DailyCheckinSuperV2{Enabled: superWire.Enabled, MinBps: superWire.MinBps, MaxBps: superWire.MaxBps, Cost: cost},
		PendingRefresh:       wire.PendingRefresh,
		RefreshEffectiveAt:   wire.RefreshEffectiveAt,
		ReviewApproved:       reviewed,
		Configured:           true,
		ConsentCompatibility: normalizeCheckinConsentCompatibility(wire.ConsentCompatibility),
		legacyGameFields:     len(wire.Normal) > 0 || len(wire.Super) > 0 || len(wire.Reviewed) > 0,
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
	wire := checkinPolicyV2StoredWire{
		Version:              p.Version,
		RefreshTime:          p.RefreshTime,
		AutoFeeBps:           p.AutoFeeBps,
		PendingRefresh:       p.PendingRefresh,
		RefreshEffectiveAt:   p.RefreshEffectiveAt,
		ConsentCompatibility: normalizeCheckinConsentCompatibility(p.ConsentCompatibility),
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
		anchor := p.PendingRefresh.EffectiveAt
		effective.RefreshEffectiveAt = &anchor
		effective.PendingRefresh = nil
	}
	return effective
}

type legacyCheckinPolicyRuleVersionWire struct {
	Enabled            bool                          `json:"enabled"`
	MaxRewardDay       int                           `json:"max_reward_day"`
	RewardTiers        []dailyCheckinRewardTierValue `json:"reward_tiers"`
	RefreshTime        string                        `json:"refresh_time"`
	AutoFeeBps         int                           `json:"auto_fee_bps"`
	Normal             DailyCheckinRandomV2          `json:"normal"`
	Super              DailyCheckinSuperV2           `json:"super"`
	PendingRefresh     *DailyCheckinPendingV2        `json:"pending_refresh,omitempty"`
	RefreshEffectiveAt *time.Time                    `json:"refresh_effective_at,omitempty"`
	Reviewed           bool                          `json:"reviewed"`
}

type checkinPolicyRuleVersionWire struct {
	Canonicalization   string                        `json:"canonicalization"`
	Enabled            bool                          `json:"enabled"`
	MaxRewardDay       int                           `json:"max_reward_day"`
	RewardTiers        []dailyCheckinRewardTierValue `json:"reward_tiers"`
	RefreshTime        string                        `json:"refresh_time"`
	AutoFeeBps         int                           `json:"auto_fee_bps"`
	PendingRefresh     *checkinPendingRefreshVersion `json:"pending_refresh,omitempty"`
	RefreshEffectiveAt string                        `json:"refresh_effective_at,omitempty"`
}

type checkinPendingRefreshVersion struct {
	RefreshTime string `json:"refresh_time"`
	EffectiveAt string `json:"effective_at"`
}

// EffectiveCheckinPolicyVersion is a deterministic fingerprint of the active
// direct/direct-auto policy only. Deprecated normal/super/reviewed settings are
// intentionally excluded so they cannot silently invalidate automatic consent.
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
	var pending *checkinPendingRefreshVersion
	if extended.PendingRefresh != nil {
		pending = &checkinPendingRefreshVersion{
			RefreshTime: strings.TrimSpace(extended.PendingRefresh.RefreshTime),
			EffectiveAt: canonicalCheckinTime(extended.PendingRefresh.EffectiveAt),
		}
	}
	wire := checkinPolicyRuleVersionWire{
		Canonicalization:   checkinPolicyCanonicalizationVersionV1,
		Enabled:            base != nil && base.Enabled,
		MaxRewardDay:       0,
		RewardTiers:        tiers,
		RefreshTime:        strings.TrimSpace(extended.RefreshTime),
		AutoFeeBps:         extended.AutoFeeBps,
		PendingRefresh:     pending,
		RefreshEffectiveAt: canonicalCheckinTimePtr(extended.RefreshEffectiveAt),
	}
	if base != nil {
		wire.MaxRewardDay = base.MaxRewardDay
	}
	raw, _ := json.Marshal(wire)
	sum := sha256.Sum256(raw)
	return "checkin-v2-" + hex.EncodeToString(sum[:12])
}

func legacyCheckinConsentVersion(base *DailyCheckinPolicy, extended DailyCheckinPolicyV2) string {
	tiers := make([]dailyCheckinRewardTierValue, 0)
	maxRewardDay := 0
	enabled := false
	if base != nil {
		enabled = base.Enabled
		maxRewardDay = base.MaxRewardDay
		tiers = make([]dailyCheckinRewardTierValue, len(base.RewardTiers))
		for index, tier := range base.RewardTiers {
			tiers[index] = dailyCheckinRewardTierValue{
				Day:             tier.Day,
				Amount:          formatLedgerAmount(tier.Amount),
				PermanentAmount: formatLedgerAmount(tier.PermanentAmount),
			}
		}
	}
	wire := legacyCheckinPolicyRuleVersionWire{
		Enabled:            enabled,
		MaxRewardDay:       maxRewardDay,
		RewardTiers:        tiers,
		RefreshTime:        extended.RefreshTime,
		AutoFeeBps:         extended.AutoFeeBps,
		Normal:             extended.Normal,
		Super:              extended.Super,
		PendingRefresh:     extended.PendingRefresh,
		RefreshEffectiveAt: extended.RefreshEffectiveAt,
		Reviewed:           extended.ReviewApproved,
	}
	raw, _ := json.Marshal(wire)
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("checkin-v2-%s:auto-fee:%d", hex.EncodeToString(sum[:12]), extended.AutoFeeBps)
}

func normalizeCheckinConsentCompatibility(values []checkinConsentCompatibility) []checkinConsentCompatibility {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]checkinConsentCompatibility, 0, len(values))
	for _, value := range values {
		value.ConsentVersion = strings.TrimSpace(value.ConsentVersion)
		value.PolicyVersion = strings.TrimSpace(value.PolicyVersion)
		if value.ConsentVersion == "" || value.PolicyVersion == "" {
			continue
		}
		key := value.ConsentVersion + "\x00" + value.PolicyVersion
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func addCheckinConsentCompatibility(values []checkinConsentCompatibility, consentVersion, policyVersion string) []checkinConsentCompatibility {
	values = append(values, checkinConsentCompatibility{
		ConsentVersion: strings.TrimSpace(consentVersion),
		PolicyVersion:  strings.TrimSpace(policyVersion),
	})
	return normalizeCheckinConsentCompatibility(values)
}

// carryCheckinConsentCompatibility re-points every consent version the old
// policy version accepted onto the new policy version, preserving existing
// consents across a fee reduction (see UpdateDailyCheckinPolicyV2).
func carryCheckinConsentCompatibility(previous []checkinConsentCompatibility, oldVersion, newVersion string) []checkinConsentCompatibility {
	carried := addCheckinConsentCompatibility(nil, oldVersion, newVersion)
	for _, entry := range previous {
		if strings.TrimSpace(entry.PolicyVersion) == strings.TrimSpace(oldVersion) {
			carried = addCheckinConsentCompatibility(carried, entry.ConsentVersion, newVersion)
		}
	}
	return carried
}

// checkinPolicyVersionIgnoringFee computes the effective policy version with the
// auto-checkin fee normalized out, so two policies that differ only by fee hash
// to the same value.
func checkinPolicyVersionIgnoringFee(base *DailyCheckinPolicy, extended DailyCheckinPolicyV2) string {
	extended.AutoFeeBps = 0
	return EffectiveCheckinPolicyVersion(base, extended)
}

// checkinPolicyChangeIsFeeReduction reports whether the only effective change
// between the current and updated policy is a reduction of the auto-checkin fee.
// A fee reduction stays within the customer's existing consent ("at most N bps")
// and therefore must not invalidate it.
func checkinPolicyChangeIsFeeReduction(currentBase *DailyCheckinPolicy, current DailyCheckinPolicyV2, nextBase *DailyCheckinPolicy, next DailyCheckinPolicyV2, now time.Time) bool {
	active := current.EffectiveAt(now)
	if next.AutoFeeBps >= active.AutoFeeBps {
		return false
	}
	return checkinPolicyVersionIgnoringFee(nextBase, next) == checkinPolicyVersionIgnoringFee(currentBase, active)
}

func canonicalCheckinTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func canonicalCheckinTimePtr(value *time.Time) string {
	if value == nil {
		return ""
	}
	return canonicalCheckinTime(*value)
}

func (p DailyCheckinPolicyV2) ConsentVersion() string {
	version := strings.TrimSpace(p.Version)
	if version == "" {
		version = "checkin-v2-" + checkinPolicyCanonicalizationVersionV1
	}
	return version
}

func (p DailyCheckinPolicyV2) AcceptsConsentVersion(version string) bool {
	version = strings.TrimSpace(version)
	current := p.ConsentVersion()
	if version == current {
		return true
	}
	for _, compatible := range p.ConsentCompatibility {
		if compatible.ConsentVersion == version && compatible.PolicyVersion == current {
			return true
		}
	}
	return false
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
	if effective.RefreshEffectiveAt != nil && !now.Before(*effective.RefreshEffectiveAt) && effective.RefreshEffectiveAt.After(candidate) {
		candidate = effective.RefreshEffectiveAt.In(beijingLocation)
	}
	next := time.Date(candidate.Year(), candidate.Month(), candidate.Day(), refreshMinute/60, refreshMinute%60, 0, 0, beijingLocation)
	if !next.After(candidate) {
		next = next.AddDate(0, 0, 1)
	}
	if policy.PendingRefresh != nil && now.Before(policy.PendingRefresh.EffectiveAt) && policy.PendingRefresh.EffectiveAt.Before(next) {
		next = policy.PendingRefresh.EffectiveAt.In(beijingLocation)
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
	if extended.legacyGameFields {
		extended.ConsentCompatibility = addCheckinConsentCompatibility(
			extended.ConsentCompatibility,
			legacyCheckinConsentVersion(base, extended),
			extended.Version,
		)
	}
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
	base, extended, err := parseCheckinPolicyBundle(values)
	if err != nil {
		return nil, DailyCheckinPolicyV2{}, err
	}
	return base, checkinAdminPolicyView(base, extended, time.Now()), nil
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
	currentBase, current, err := parseCheckinPolicyBundle(expectedValues)
	if err != nil {
		return err
	}
	now := time.Now()
	if err := prepareCheckinPolicyV2Change(currentBase, current, extended, expected, now); err != nil {
		return err
	}

	nextVersion := EffectiveCheckinPolicyVersion(policy, *extended)
	oldVersion := activeCheckinPolicyVersion(currentBase, current, now)
	switch {
	case nextVersion == oldVersion:
		extended.ConsentCompatibility = current.ConsentCompatibility
	case checkinPolicyChangeIsFeeReduction(currentBase, current, policy, *extended, now):
		// A fee reduction never widens what the user agreed to (they consented to
		// "at most" the prior fee), so prior consents must stay valid instead of
		// forcing a silent re-consent that would break auto check-in streaks.
		// Re-point every consent version the old policy accepted onto the new one.
		extended.ConsentCompatibility = carryCheckinConsentCompatibility(current.ConsentCompatibility, oldVersion, nextVersion)
	default:
		extended.ConsentCompatibility = nil
	}
	extended.Version = nextVersion
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

// Admin CAS follows the effective representation, not just the stored JSON.
// Crossing a pending boundary invalidates an old form without guessing whether
// the old clock was an intentional edit. Customer consent still uses the raw
// policy version and is not changed merely by this read-only projection.
func activeCheckinPolicyVersion(base *DailyCheckinPolicy, policy DailyCheckinPolicyV2, now time.Time) string {
	return EffectiveCheckinPolicyVersion(base, policy.EffectiveAt(now))
}

func checkinAdminPolicyView(base *DailyCheckinPolicy, policy DailyCheckinPolicyV2, now time.Time) DailyCheckinPolicyV2 {
	view := policy.EffectiveAt(now)
	view.Version = activeCheckinPolicyVersion(base, policy, now)
	return view
}

func prepareCheckinPolicyV2Change(base *DailyCheckinPolicy, current DailyCheckinPolicyV2, updated *DailyCheckinPolicyV2, expected string, now time.Time) error {
	active := checkinAdminPolicyView(base, current, now)
	if expected != active.Version {
		return ErrCheckinPolicyVersionStale
	}
	updated.RefreshTime = strings.TrimSpace(updated.RefreshTime)
	// This anchor is server-owned. Retaining it keeps a short transition period
	// unchanged when an unrelated setting is saved before its next reset.
	updated.RefreshEffectiveAt = active.RefreshEffectiveAt
	if updated.RefreshTime != active.RefreshTime {
		if _, err := parseCheckinRefreshTime(updated.RefreshTime); err != nil {
			return err
		}
		period, err := checkinPeriodAt(current, now)
		if err != nil {
			return err
		}
		updated.PendingRefresh = &DailyCheckinPendingV2{RefreshTime: updated.RefreshTime, EffectiveAt: period.NextReset}
		updated.RefreshTime = active.RefreshTime
	} else {
		updated.PendingRefresh = active.PendingRefresh
	}
	return nil
}
