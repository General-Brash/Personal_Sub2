package service

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type DynamicRateUsageStatus struct {
	GroupID       int64            `json:"group_id"`
	UserID        int64            `json:"user_id"`
	Metric        string           `json:"metric"`
	WindowID      string           `json:"window_id"`
	Current       decimal.Decimal  `json:"current"`
	TierID        string           `json:"tier_id"`
	Factor        float64          `json:"factor"`
	NextThreshold *decimal.Decimal `json:"next_threshold,omitempty"`
	NextFactor    *float64         `json:"next_factor,omitempty"`
	ResetAt       time.Time        `json:"reset_at"`
	PolicyVersion int64            `json:"policy_version"`
}

type DynamicRateRepository interface {
	GetDynamicRatePolicy(ctx context.Context, groupID int64) (*DynamicRatePolicy, error)
	GetDynamicRateUsage(ctx context.Context, userID, groupID int64, windowID string) (DynamicRateUsageCounters, error)
	UpsertDynamicRatePolicy(ctx context.Context, policy *DynamicRatePolicy) (*DynamicRatePolicy, error)
}

type dynamicRateResolver struct{ repo DynamicRateRepository }

func newDynamicRateResolver(repo DynamicRateRepository) *dynamicRateResolver {
	return &dynamicRateResolver{repo: repo}
}

func (r *dynamicRateResolver) Freeze(ctx context.Context, userID, groupID int64, mode string, staticFactor, peakFactor float64, at time.Time, subscriptionGroup bool) (*DynamicRatePricingSnapshot, error) {
	if r == nil || r.repo == nil || userID <= 0 || groupID <= 0 {
		return nil, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	policy, err := effectiveDynamicRatePolicyAt(ctx, r.repo, groupID, at)
	if err != nil {
		return nil, err
	}
	if policy == nil || !policy.Enabled {
		return nil, nil
	}
	if err := ValidateDynamicRatePolicy(policy); err != nil {
		return nil, err
	}
	if at.IsZero() {
		at = time.Now()
	}
	if !policy.EffectiveAt.IsZero() && at.Before(policy.EffectiveAt) {
		return nil, nil
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	if !containsDynamicRateMode(policy.IncludedModes, mode) {
		return nil, ErrDynamicRateModeNotCovered
	}
	if policy.Metric == DynamicRateMetricWalletSpend && subscriptionGroup {
		return nil, ErrDynamicRateSubscriptionWalletMode
	}
	windowID, _, _, err := DynamicRateWindow(policy, at)
	if err != nil {
		return nil, err
	}
	usage, err := r.repo.GetDynamicRateUsage(ctx, userID, groupID, windowID)
	if err != nil {
		return nil, err
	}
	counter := DynamicRateCounter(policy.Metric, usage)
	tier, ok := SelectDynamicRateTier(policy, counter)
	if !ok {
		return nil, ErrDynamicRatePolicyInvalid
	}
	finalFactor, err := ComposeDynamicRateFactor(staticFactor, peakFactor, tier.Factor)
	if err != nil {
		return nil, err
	}
	snapshot := &DynamicRatePricingSnapshot{UserID: userID, GroupID: groupID, Metric: policy.Metric, Mode: mode, WindowID: windowID, PolicyVersion: policy.PolicyVersion, TierID: tier.ID, CounterBefore: counter, StaticFactor: staticFactor, PeakFactor: peakFactor, DynamicFactor: tier.Factor, FinalFactor: finalFactor, PricedAt: at.UTC()}
	snapshot.PricingSnapshotID = BuildDynamicRateSnapshotID(snapshot)
	return snapshot, nil
}

func (r *dynamicRateResolver) ResolveForBilling(ctx context.Context, snapshot *DynamicRatePricingSnapshot, userID, groupID int64, mode, metric string) (*DynamicRatePricingSnapshot, error) {
	mode, metric = strings.ToLower(strings.TrimSpace(mode)), strings.ToLower(strings.TrimSpace(metric))
	if snapshot != nil && metric == "" {
		metric = strings.ToLower(strings.TrimSpace(snapshot.Metric))
	}
	if snapshot != nil && snapshot.Mode == DynamicRateModeImage && mode == DynamicRateModeText && snapshot.Metric == DynamicRateMetricWalletSpend {
		mode = DynamicRateModeImage
	}
	if snapshot != nil {
		if err := ValidateDynamicRateSnapshotForRequest(snapshot, userID, groupID, mode, metric); err != nil {
			return nil, err
		}
		return snapshot, nil
	}
	if r == nil || r.repo == nil || userID <= 0 || groupID <= 0 {
		return nil, nil
	}
	var policy *DynamicRatePolicy
	var err error
	if at, ok := ctx.Value(dynamicRateAdmissionTimeKey{}).(time.Time); ok && !at.IsZero() {
		policy, err = effectiveDynamicRatePolicyAt(ctx, r.repo, groupID, at)
	} else {
		policy, err = r.repo.GetDynamicRatePolicy(ctx, groupID)
	}
	if err != nil {
		return nil, err
	}
	if policy == nil || !policy.Enabled {
		return nil, nil
	}
	if err := ValidateDynamicRatePolicy(policy); err != nil {
		return nil, err
	}
	now := time.Now()
	if admission, ok := ctx.Value(dynamicRateAdmissionTimeKey{}).(time.Time); ok && !admission.IsZero() {
		now = admission
	}
	if !policy.EffectiveAt.IsZero() && now.Before(policy.EffectiveAt) {
		return nil, nil
	}
	return nil, ErrDynamicRateSnapshotRequired
}

func (r *dynamicRateResolver) Status(ctx context.Context, userID, groupID int64, at time.Time) (*DynamicRateUsageStatus, *DynamicRatePolicy, error) {
	if r == nil || r.repo == nil || userID <= 0 || groupID <= 0 {
		return nil, nil, nil
	}
	policy, err := r.repo.GetDynamicRatePolicy(ctx, groupID)
	if err != nil {
		return nil, nil, err
	}
	if policy == nil || !policy.Enabled {
		return nil, policy, nil
	}
	if err := ValidateDynamicRatePolicy(policy); err != nil {
		return nil, nil, err
	}
	windowID, _, end, err := DynamicRateWindow(policy, at)
	if err != nil {
		return nil, nil, err
	}
	usage, err := r.repo.GetDynamicRateUsage(ctx, userID, groupID, windowID)
	if err != nil {
		return nil, nil, err
	}
	counter := DynamicRateCounter(policy.Metric, usage)
	tier, ok := SelectDynamicRateTier(policy, counter)
	if !ok {
		return nil, nil, ErrDynamicRatePolicyInvalid
	}
	status := &DynamicRateUsageStatus{GroupID: groupID, UserID: userID, Metric: policy.Metric, WindowID: windowID, Current: counter, TierID: tier.ID, Factor: tier.Factor, ResetAt: end, PolicyVersion: policy.PolicyVersion}
	for _, candidate := range policy.Tiers {
		if candidate.Threshold.GreaterThan(counter) {
			threshold, factor := candidate.Threshold, candidate.Factor
			status.NextThreshold, status.NextFactor = &threshold, &factor
			break
		}
	}
	return status, policy, nil
}

func containsDynamicRateMode(modes []string, mode string) bool {
	for _, candidate := range modes {
		if strings.EqualFold(strings.TrimSpace(candidate), mode) {
			return true
		}
	}
	return false
}

type dynamicRatePricingSnapshotCtxKey struct{}

func WithDynamicRatePricingSnapshot(ctx context.Context, snapshot *DynamicRatePricingSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snapshot == nil {
		return ctx
	}
	return context.WithValue(ctx, dynamicRatePricingSnapshotCtxKey{}, snapshot)
}

func DynamicRatePricingSnapshotFromContext(ctx context.Context) *DynamicRatePricingSnapshot {
	if ctx == nil {
		return nil
	}
	snapshot, _ := ctx.Value(dynamicRatePricingSnapshotCtxKey{}).(*DynamicRatePricingSnapshot)
	return snapshot
}

func (s *GatewayService) SetDynamicRateRepository(repo DynamicRateRepository) {
	if s == nil || repo == nil {
		return
	}
	s.dynamicRateResolver = newDynamicRateResolver(repo)
}

func (s *OpenAIGatewayService) SetDynamicRateRepository(repo DynamicRateRepository) {
	if s == nil || repo == nil {
		return
	}
	s.dynamicRateResolver = newDynamicRateResolver(repo)
}

func (s *GatewayService) FreezeDynamicRatePricing(ctx context.Context, apiKey *APIKey, userID int64, mode string, at time.Time) (context.Context, error) {
	if s == nil || s.dynamicRateResolver == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil {
		return ctx, nil
	}
	staticFactor := s.getUserGroupRateMultiplier(ctx, userID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	peakFactor := apiKey.Group.PeakMultiplierAt(at)
	if math.IsNaN(staticFactor) || math.IsInf(staticFactor, 0) {
		return ctx, ErrDynamicRatePolicyInvalid
	}
	snapshot, err := s.dynamicRateResolver.Freeze(ctx, userID, *apiKey.GroupID, mode, staticFactor, peakFactor, at, apiKey.Group.IsSubscriptionType())
	if err != nil {
		return ctx, err
	}
	if snapshot != nil {
		base := resolveImageRateMultiplier(apiKey, staticFactor)
		snapshot.ImageBaseFactor = &base
		snapshot.PricingSnapshotID = BuildDynamicRateSnapshotID(snapshot)
	}
	ctx = WithDynamicRateAdmissionTime(ctx, at)
	return WithDynamicRatePricingSnapshot(ctx, snapshot), nil
}

func (s *OpenAIGatewayService) FreezeDynamicRatePricing(ctx context.Context, apiKey *APIKey, userID int64, mode string, at time.Time) (context.Context, error) {
	if s == nil || s.dynamicRateResolver == nil || apiKey == nil || apiKey.GroupID == nil || apiKey.Group == nil {
		return ctx, nil
	}
	staticFactor := s.ResolveUserGroupRateMultiplier(ctx, userID, *apiKey.GroupID, apiKey.Group.RateMultiplier)
	peakFactor := apiKey.Group.PeakMultiplierAt(at)
	if math.IsNaN(staticFactor) || math.IsInf(staticFactor, 0) {
		return ctx, ErrDynamicRatePolicyInvalid
	}
	snapshot, err := s.dynamicRateResolver.Freeze(ctx, userID, *apiKey.GroupID, mode, staticFactor, peakFactor, at, apiKey.Group.IsSubscriptionType())
	if err != nil {
		return ctx, err
	}
	if snapshot != nil {
		base := resolveImageRateMultiplier(apiKey, staticFactor)
		snapshot.ImageBaseFactor = &base
		snapshot.PricingSnapshotID = BuildDynamicRateSnapshotID(snapshot)
	}
	ctx = WithDynamicRateAdmissionTime(ctx, at)
	return WithDynamicRatePricingSnapshot(ctx, snapshot), nil
}

// RequireDynamicRateAdmission is the fail-closed pre-upstream gate for modes
// that either are not billed by the unified usage path (Live/count_tokens) or
// have no mode-specific snapshot implementation yet. A disabled policy passes;
// an enabled policy requires either a matching frozen snapshot for text-funded
// paths or rejects the request before any upstream call.
func (s *GatewayService) RequireDynamicRateAdmission(ctx context.Context, apiKey *APIKey, mode string) error {
	if s == nil || s.dynamicRateResolver == nil || apiKey == nil || apiKey.GroupID == nil || *apiKey.GroupID <= 0 {
		return nil
	}
	userID := apiKey.UserID
	if userID <= 0 && apiKey.User != nil {
		userID = apiKey.User.ID
	}
	if userID <= 0 {
		return nil
	}
	_, err := dynamicRateSnapshotForBilling(ctx, s.dynamicRateResolver, userID, *apiKey.GroupID, mode, "")
	return err
}

func (s *OpenAIGatewayService) RequireDynamicRateAdmission(ctx context.Context, apiKey *APIKey, mode string) error {
	if s == nil || s.dynamicRateResolver == nil || apiKey == nil || apiKey.GroupID == nil || *apiKey.GroupID <= 0 {
		return nil
	}
	userID := apiKey.UserID
	if userID <= 0 && apiKey.User != nil {
		userID = apiKey.User.ID
	}
	if userID <= 0 {
		return nil
	}
	_, err := dynamicRateSnapshotForBilling(ctx, s.dynamicRateResolver, userID, *apiKey.GroupID, mode, "")
	return err
}

func dynamicRateSnapshotForBilling(ctx context.Context, resolver *dynamicRateResolver, userID, groupID int64, mode, metric string) (*DynamicRatePricingSnapshot, error) {
	if resolver == nil {
		return DynamicRatePricingSnapshotFromContext(ctx), nil
	}
	return resolver.ResolveForBilling(ctx, DynamicRatePricingSnapshotFromContext(ctx), userID, groupID, mode, metric)
}

type DynamicRateUsageCounters struct {
	NormalizedTokens int64           `json:"normalized_tokens"`
	WalletSpent      decimal.Decimal `json:"wallet_spent"`
}

func DynamicRateCounter(metric string, usage DynamicRateUsageCounters) decimal.Decimal {
	if metric == DynamicRateMetricTokensM {
		return decimal.NewFromInt(usage.NormalizedTokens)
	}
	return usage.WalletSpent
}

type dynamicRateAdmissionTimeKey struct{}

func WithDynamicRateAdmissionTime(ctx context.Context, at time.Time) context.Context {
	if at.IsZero() {
		at = time.Now()
	}
	return context.WithValue(ctx, dynamicRateAdmissionTimeKey{}, at)
}
func effectiveDynamicRatePolicyAt(ctx context.Context, repo DynamicRateRepository, groupID int64, at time.Time) (*DynamicRatePolicy, error) {
	if historical, ok := repo.(interface {
		GetDynamicRatePolicyAt(context.Context, int64, time.Time) (*DynamicRatePolicy, error)
	}); ok {
		return historical.GetDynamicRatePolicyAt(ctx, groupID, at)
	}
	return repo.GetDynamicRatePolicy(ctx, groupID)
}
