package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/shopspring/decimal"
)

var ErrDynamicRateRepositoryUnavailable = errors.New("dynamic rate repository is unavailable")

// DynamicRatePreview is a read-only estimate for the admin UI. It never writes
// a counter or a pricing snapshot.
type DynamicRatePreview struct {
	Policy        *DynamicRatePolicy      `json:"policy"`
	Status        *DynamicRateUsageStatus `json:"status"`
	WindowStart   time.Time               `json:"window_start"`
	WindowEnd     time.Time               `json:"window_end"`
	DynamicFactor float64                 `json:"dynamic_factor"`
	FinalFactor   float64                 `json:"final_factor"`
}

func DefaultDynamicRatePolicy(groupID int64) *DynamicRatePolicy {
	return &DynamicRatePolicy{
		GroupID:          groupID,
		Enabled:          false,
		Metric:           DynamicRateMetricTokensM,
		Timezone:         "Asia/Shanghai",
		ResetTime:        "00:00",
		Tiers:            []DynamicRateTier{{ID: "tier-0", Threshold: decimal.Zero, Factor: 1}},
		IncludedModes:    []string{DynamicRateModeText},
		CacheTokenPolicy: DynamicRateCachePolicyNormalized,
	}
}

func (r *dynamicRateResolver) Policy(ctx context.Context, groupID int64) (*DynamicRatePolicy, error) {
	if r == nil || r.repo == nil {
		return nil, ErrDynamicRateRepositoryUnavailable
	}
	if drafts, ok := r.repo.(interface {
		GetDynamicRatePolicyDraft(context.Context, int64) (*DynamicRatePolicy, error)
	}); ok {
		return drafts.GetDynamicRatePolicyDraft(ctx, groupID)
	}
	return r.repo.GetDynamicRatePolicy(ctx, groupID)
}

func (r *dynamicRateResolver) UpsertPolicy(ctx context.Context, policy *DynamicRatePolicy) (*DynamicRatePolicy, error) {
	if r == nil || r.repo == nil {
		return nil, ErrDynamicRateRepositoryUnavailable
	}
	return r.repo.UpsertDynamicRatePolicy(ctx, policy)
}

func (r *dynamicRateResolver) Preview(ctx context.Context, policy *DynamicRatePolicy, userID int64, at time.Time, staticFactor, peakFactor float64, subscriptionGroup bool) (*DynamicRatePreview, error) {
	if r == nil || r.repo == nil {
		return nil, ErrDynamicRateRepositoryUnavailable
	}
	if policy == nil {
		return nil, ErrDynamicRatePolicyInvalid
	}
	if err := ValidateDynamicRatePolicy(policy); err != nil {
		return nil, err
	}
	if !policy.Enabled {
		return &DynamicRatePreview{Policy: policy, DynamicFactor: 1, FinalFactor: staticFactor * peakFactor}, nil
	}
	if userID <= 0 {
		return nil, fmt.Errorf("%w: user_id is required", ErrDynamicRatePolicyInvalid)
	}
	if at.IsZero() {
		at = time.Now()
	}
	windowID, start, end, err := DynamicRateWindow(policy, at)
	if err != nil {
		return nil, err
	}
	usage, err := r.repo.GetDynamicRateUsage(ctx, userID, policy.GroupID, windowID)
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
	status := &DynamicRateUsageStatus{
		GroupID: policy.GroupID, UserID: userID, Metric: policy.Metric, WindowID: windowID,
		Current: counter, TierID: tier.ID, Factor: tier.Factor, ResetAt: end, PolicyVersion: policy.PolicyVersion,
	}
	for _, candidate := range policy.Tiers {
		if candidate.Threshold.GreaterThan(counter) {
			threshold, factor := candidate.Threshold, candidate.Factor
			status.NextThreshold, status.NextFactor = &threshold, &factor
			break
		}
	}
	return &DynamicRatePreview{Policy: policy, Status: status, WindowStart: start, WindowEnd: end, DynamicFactor: tier.Factor, FinalFactor: finalFactor}, nil
}

func (s *GatewayService) DynamicRatePolicy(ctx context.Context, groupID int64) (*DynamicRatePolicy, error) {
	if s == nil || s.dynamicRateResolver == nil {
		return DefaultDynamicRatePolicy(groupID), nil
	}
	policy, err := s.dynamicRateResolver.Policy(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if policy == nil {
		return DefaultDynamicRatePolicy(groupID), nil
	}
	return policy, nil
}

func (s *GatewayService) UpsertDynamicRatePolicy(ctx context.Context, policy *DynamicRatePolicy) (*DynamicRatePolicy, error) {
	if s == nil || s.dynamicRateResolver == nil {
		return nil, ErrDynamicRateRepositoryUnavailable
	}
	if policy != nil {
		if policy.Enabled && s.cfg != nil && s.cfg.RunMode == config.RunModeSimple {
			return nil, ErrDynamicRateSimpleRunModeDisabled
		}
		if strings.TrimSpace(policy.Timezone) == "" {
			policy.Timezone = "Asia/Shanghai"
		}
	}
	return s.dynamicRateResolver.UpsertPolicy(ctx, policy)
}

func (s *GatewayService) PreviewDynamicRate(ctx context.Context, policy *DynamicRatePolicy, userID int64, at time.Time, staticFactor, peakFactor float64, subscriptionGroup bool) (*DynamicRatePreview, error) {
	if s == nil || s.dynamicRateResolver == nil {
		return nil, ErrDynamicRateRepositoryUnavailable
	}
	return s.dynamicRateResolver.Preview(ctx, policy, userID, at, staticFactor, peakFactor, subscriptionGroup)
}

func (s *GatewayService) DynamicRateStatus(ctx context.Context, userID, groupID int64, at time.Time) (*DynamicRateUsageStatus, *DynamicRatePolicy, error) {
	if s == nil || s.dynamicRateResolver == nil {
		return nil, DefaultDynamicRatePolicy(groupID), nil
	}
	return s.dynamicRateResolver.Status(ctx, userID, groupID, at)
}
