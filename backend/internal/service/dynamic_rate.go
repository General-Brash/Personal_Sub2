package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	DynamicRateMetricTokensM     = "tokens_m"
	DynamicRateMetricWalletSpend = "wallet_spend"

	DynamicRateModeText       = "text"
	DynamicRateModeImage      = "image"
	DynamicRateModeAudio      = "audio"
	DynamicRateModeVideo      = "video"
	DynamicRateModeBatchImage = "batch_image"

	DynamicRateCachePolicyNormalized = "normalized"
	DynamicRateCachePolicyProvider   = "provider"

	dynamicRateMaxFactor = 1000.0
)

var (
	ErrDynamicRatePolicyInvalid          = errors.New("dynamic rate policy is invalid")
	ErrDynamicRateModeNotCovered         = errors.New("dynamic rate policy does not cover billing mode")
	ErrDynamicRateSnapshotRequired       = errors.New("dynamic rate policy requires an admission snapshot")
	ErrDynamicRateSnapshotMismatch       = errors.New("dynamic rate snapshot does not match request")
	ErrDynamicRateSubscriptionWalletMode = errors.New("wallet_spend dynamic rate is not allowed for subscription groups")
	ErrDynamicRatePolicyVersionConflict  = errors.New("dynamic rate policy version conflict")
	ErrDynamicRateSimpleRunModeDisabled  = errors.New("dynamic rate cannot be enabled in simple run mode")
	ErrDynamicRateTokenUsageUnavailable  = errors.New("dynamic rate token metric has no non-overlapping usage")
)

type DynamicRateTier struct {
	ID        string          `json:"id"`
	Threshold decimal.Decimal `json:"threshold"`
	Factor    float64         `json:"factor"`
}

type DynamicRatePolicy struct {
	GroupID          int64             `json:"group_id"`
	Enabled          bool              `json:"enabled"`
	Metric           string            `json:"metric"`
	Timezone         string            `json:"timezone"`
	ResetTime        string            `json:"reset_time"`
	Tiers            []DynamicRateTier `json:"tiers"`
	IncludedModes    []string          `json:"included_modes"`
	CacheTokenPolicy string            `json:"cache_token_policy"`
	PolicyVersion    int64             `json:"policy_version"`
	EffectiveAt      time.Time         `json:"effective_at"`

	// CurrentPolicyVersion is the persisted head version. It can differ from
	// PolicyVersion when a future policy is already written and GET returns the
	// currently effective historical snapshot.
	CurrentPolicyVersion int64 `json:"current_policy_version,omitempty"`
	// ExpectedPolicyVersion is a compare-and-swap guard supplied by the admin
	// API. It is never persisted in the policy history snapshot.
	ExpectedPolicyVersion *int64 `json:"-"`
}

type DynamicRatePricingSnapshot struct {
	ImageBaseFactor   *float64        `json:"image_base_factor,omitempty"`
	UserID            int64           `json:"user_id"`
	GroupID           int64           `json:"group_id"`
	Metric            string          `json:"metric"`
	Mode              string          `json:"mode"`
	WindowID          string          `json:"window_id"`
	PolicyVersion     int64           `json:"policy_version"`
	TierID            string          `json:"tier_id"`
	CounterBefore     decimal.Decimal `json:"counter_before"`
	StaticFactor      float64         `json:"static_factor"`
	PeakFactor        float64         `json:"peak_factor"`
	DynamicFactor     float64         `json:"dynamic_factor"`
	FinalFactor       float64         `json:"final_factor"`
	PricingSnapshotID string          `json:"pricing_snapshot_id"`
	PricedAt          time.Time       `json:"priced_at"`
}

func NormalizeDynamicRatePolicy(p *DynamicRatePolicy) error {
	if p == nil {
		return fmt.Errorf("%w: nil policy", ErrDynamicRatePolicyInvalid)
	}
	p.Metric = strings.ToLower(strings.TrimSpace(p.Metric))
	p.Timezone = strings.TrimSpace(p.Timezone)
	if p.Timezone == "" {
		p.Timezone = "Asia/Shanghai"
	}
	p.ResetTime = strings.TrimSpace(p.ResetTime)
	if p.ResetTime == "" {
		p.ResetTime = "00:00"
	}
	p.CacheTokenPolicy = strings.ToLower(strings.TrimSpace(p.CacheTokenPolicy))
	if p.CacheTokenPolicy == "" {
		p.CacheTokenPolicy = DynamicRateCachePolicyNormalized
	}
	for i := range p.Tiers {
		p.Tiers[i].ID = strings.TrimSpace(p.Tiers[i].ID)
	}
	for i := range p.IncludedModes {
		p.IncludedModes[i] = strings.ToLower(strings.TrimSpace(p.IncludedModes[i]))
	}
	return nil
}

func ValidateDynamicRatePolicy(p *DynamicRatePolicy) error {
	if err := NormalizeDynamicRatePolicy(p); err != nil {
		return err
	}
	if !p.Enabled {
		return nil
	}
	if p.GroupID <= 0 {
		return fmt.Errorf("%w: group_id is required", ErrDynamicRatePolicyInvalid)
	}
	if p.Metric != DynamicRateMetricTokensM && p.Metric != DynamicRateMetricWalletSpend {
		return fmt.Errorf("%w: unsupported metric %q", ErrDynamicRatePolicyInvalid, p.Metric)
	}
	if p.Timezone != "Asia/Shanghai" && p.Timezone != "UTC" {
		return fmt.Errorf("%w: only Asia/Shanghai and UTC reset clocks are supported", ErrDynamicRatePolicyInvalid)
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("%w: invalid timezone %q", ErrDynamicRatePolicyInvalid, p.Timezone)
	}
	if _, _, err := parseDynamicRateResetTime(p.ResetTime); err != nil {
		return fmt.Errorf("%w: invalid reset_time %q", ErrDynamicRatePolicyInvalid, p.ResetTime)
	}
	if p.CacheTokenPolicy != DynamicRateCachePolicyNormalized {
		return fmt.Errorf("%w: unsupported cache_token_policy %q", ErrDynamicRatePolicyInvalid, p.CacheTokenPolicy)
	}
	if len(p.Tiers) == 0 {
		return fmt.Errorf("%w: at least one tier is required", ErrDynamicRatePolicyInvalid)
	}
	seenTierID := make(map[string]struct{}, len(p.Tiers))
	previous := decimal.Zero
	for i, tier := range p.Tiers {
		if tier.ID == "" {
			return fmt.Errorf("%w: tier[%d] id is required", ErrDynamicRatePolicyInvalid, i)
		}
		if _, ok := seenTierID[tier.ID]; ok {
			return fmt.Errorf("%w: duplicate tier id %q", ErrDynamicRatePolicyInvalid, tier.ID)
		}
		seenTierID[tier.ID] = struct{}{}
		if tier.Threshold.IsNegative() {
			return fmt.Errorf("%w: tier[%d] threshold must not be negative", ErrDynamicRatePolicyInvalid, i)
		}
		if p.Metric == DynamicRateMetricTokensM && !tier.Threshold.Equal(tier.Threshold.Truncate(0)) {
			return fmt.Errorf("%w: token thresholds must be whole tokens", ErrDynamicRatePolicyInvalid)
		}
		if p.Metric == DynamicRateMetricWalletSpend && !tier.Threshold.Equal(tier.Threshold.Truncate(8)) {
			return fmt.Errorf("%w: wallet thresholds support at most eight decimals", ErrDynamicRatePolicyInvalid)
		}
		if i == 0 && !tier.Threshold.IsZero() {
			return fmt.Errorf("%w: first tier threshold must be 0", ErrDynamicRatePolicyInvalid)
		}
		if i > 0 && tier.Threshold.LessThanOrEqual(previous) {
			return fmt.Errorf("%w: tier thresholds must be strictly increasing", ErrDynamicRatePolicyInvalid)
		}
		if math.IsNaN(tier.Factor) || math.IsInf(tier.Factor, 0) || tier.Factor <= 0 || tier.Factor > dynamicRateMaxFactor {
			return fmt.Errorf("%w: tier[%d] factor must be in (0,%g]", ErrDynamicRatePolicyInvalid, i, dynamicRateMaxFactor)
		}
		previous = tier.Threshold
	}
	if len(p.IncludedModes) == 0 {
		return fmt.Errorf("%w: included_modes is required", ErrDynamicRatePolicyInvalid)
	}
	seenMode := make(map[string]struct{}, len(p.IncludedModes))
	for _, mode := range p.IncludedModes {
		if !IsDynamicRateModeSupported(mode) {
			return fmt.Errorf("%w: mode %q is not adapted and cannot be enabled", ErrDynamicRatePolicyInvalid, mode)
		}
		if _, ok := seenMode[mode]; ok {
			return fmt.Errorf("%w: duplicate mode %q", ErrDynamicRatePolicyInvalid, mode)
		}
		if (mode == DynamicRateModeBatchImage || mode == DynamicRateModeImage) && p.Metric != DynamicRateMetricWalletSpend {
			return fmt.Errorf("%w: batch images require wallet_spend", ErrDynamicRatePolicyInvalid)
		}
		seenMode[mode] = struct{}{}
	}
	return nil
}

func IsDynamicRateModeSupported(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case DynamicRateModeText, DynamicRateModeImage, DynamicRateModeBatchImage:
		return true
	default:
		return false
	}
}

func SelectDynamicRateTier(policy *DynamicRatePolicy, counter decimal.Decimal) (DynamicRateTier, bool) {
	if policy == nil || len(policy.Tiers) == 0 {
		return DynamicRateTier{}, false
	}
	if counter.IsNegative() {
		counter = decimal.Zero
	}
	selected := policy.Tiers[0]
	for _, tier := range policy.Tiers {
		if tier.Threshold.GreaterThan(counter) {
			break
		}
		selected = tier
	}
	return selected, true
}

func DynamicRateWindow(policy *DynamicRatePolicy, at time.Time) (windowID string, start, end time.Time, err error) {
	if policy == nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("%w: nil policy", ErrDynamicRatePolicyInvalid)
	}
	loc, err := time.LoadLocation(strings.TrimSpace(policy.Timezone))
	if err != nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("%w: invalid timezone", ErrDynamicRatePolicyInvalid)
	}
	hour, minute, err := parseDynamicRateResetTime(policy.ResetTime)
	if err != nil {
		return "", time.Time{}, time.Time{}, fmt.Errorf("%w: invalid reset_time", ErrDynamicRatePolicyInvalid)
	}
	if at.IsZero() {
		at = time.Now()
	}
	localAt := at.In(loc)
	startLocal := time.Date(localAt.Year(), localAt.Month(), localAt.Day(), hour, minute, 0, 0, loc)
	if localAt.Before(startLocal) {
		startLocal = startLocal.AddDate(0, 0, -1)
	}
	endLocal := startLocal.AddDate(0, 0, 1)
	if !policy.EffectiveAt.IsZero() && !at.Before(policy.EffectiveAt) && policy.EffectiveAt.After(startLocal) {
		startLocal = policy.EffectiveAt.In(loc)
	}
	start = startLocal.UTC()
	end = endLocal.UTC()
	return start.Format(time.RFC3339), start, end, nil
}

func parseDynamicRateResetTime(value string) (int, int, error) {
	parsed, err := time.Parse("15:04", strings.TrimSpace(value))
	if err != nil {
		return 0, 0, err
	}
	return parsed.Hour(), parsed.Minute(), nil
}

func ComposeDynamicRateFactor(staticFactor, peakFactor, dynamicFactor float64) (float64, error) {
	for name, value := range map[string]float64{"static": staticFactor, "peak": peakFactor, "dynamic": dynamicFactor} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, fmt.Errorf("%w: %s factor is invalid", ErrDynamicRatePolicyInvalid, name)
		}
	}
	if dynamicFactor <= 0 {
		return 0, fmt.Errorf("%w: dynamic factor must be positive", ErrDynamicRatePolicyInvalid)
	}
	value := staticFactor * peakFactor * dynamicFactor
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, ErrDynamicRatePolicyInvalid
	}
	return value, nil
}

func BuildDynamicRateSnapshotID(snapshot *DynamicRatePricingSnapshot) string {
	if snapshot == nil {
		return ""
	}
	raw, _ := json.Marshal(struct {
		ImageBaseFactor *float64        `json:"image_base_factor,omitempty"`
		UserID          int64           `json:"user_id"`
		GroupID         int64           `json:"group_id"`
		Metric          string          `json:"metric"`
		Mode            string          `json:"mode"`
		WindowID        string          `json:"window_id"`
		PolicyVersion   int64           `json:"policy_version"`
		TierID          string          `json:"tier_id"`
		CounterBefore   decimal.Decimal `json:"counter_before"`
		StaticFactor    float64         `json:"static_factor"`
		PeakFactor      float64         `json:"peak_factor"`
		DynamicFactor   float64         `json:"dynamic_factor"`
		FinalFactor     float64         `json:"final_factor"`
		PricedAt        time.Time       `json:"priced_at"`
	}{ImageBaseFactor: snapshot.ImageBaseFactor, UserID: snapshot.UserID, GroupID: snapshot.GroupID, Metric: snapshot.Metric, Mode: snapshot.Mode, WindowID: snapshot.WindowID, PolicyVersion: snapshot.PolicyVersion, TierID: snapshot.TierID, CounterBefore: snapshot.CounterBefore, StaticFactor: snapshot.StaticFactor, PeakFactor: snapshot.PeakFactor, DynamicFactor: snapshot.DynamicFactor, FinalFactor: snapshot.FinalFactor, PricedAt: snapshot.PricedAt.UTC()})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func ValidateDynamicRateSnapshotForRequest(snapshot *DynamicRatePricingSnapshot, userID, groupID int64, mode, metric string) error {
	if snapshot == nil {
		return ErrDynamicRateSnapshotRequired
	}
	if snapshot.UserID != userID || snapshot.GroupID != groupID || strings.TrimSpace(snapshot.Mode) != strings.TrimSpace(mode) || strings.TrimSpace(snapshot.Metric) != strings.TrimSpace(metric) {
		return ErrDynamicRateSnapshotMismatch
	}
	if snapshot.PricingSnapshotID == "" || snapshot.PricingSnapshotID != BuildDynamicRateSnapshotID(snapshot) {
		return ErrDynamicRateSnapshotMismatch
	}
	if snapshot.WindowID == "" || snapshot.TierID == "" || snapshot.DynamicFactor <= 0 || math.IsNaN(snapshot.DynamicFactor) || math.IsInf(snapshot.DynamicFactor, 0) {
		return ErrDynamicRateSnapshotMismatch
	}
	return nil
}
