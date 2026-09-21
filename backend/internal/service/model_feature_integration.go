package service

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// ModelFeatureAccess resolves the same persistent user, subscription and tier
// sources used for admission. A public display row never grants membership.
type ModelFeatureAccess struct {
	Users        *UserService
	Groups       *GroupService
	Entitlements *EntitlementService
}

func (s *ModelFeatureAccess) ResolveModelAccess(ctx context.Context, userID int64, anonymous bool) (ModelAccessInput, error) {
	input := ModelAccessInput{Anonymous: anonymous, AllowedGroupIDs: map[int64]struct{}{}, GroupEntitlement: map[int64]bool{}}
	if anonymous {
		return input, nil
	}
	user, err := s.Users.GetByID(ctx, userID)
	if err != nil {
		return input, err
	}
	if user == nil || !user.IsActive() {
		return input, ErrUserNotActive
	}
	snapshot, err := s.Entitlements.Resolve(ctx, userID)
	if err != nil {
		return input, err
	}
	for _, id := range snapshot.AllowedGroups {
		input.AllowedGroupIDs[id] = struct{}{}
	}
	subscriptions := map[int64]bool{}
	for _, id := range snapshot.SubscriptionGroups {
		subscriptions[id] = true
	}
	groups, err := s.Groups.ListActive(ctx)
	if err != nil {
		return input, err
	}
	for _, group := range groups {
		_, entitled := input.AllowedGroupIDs[group.ID]
		if group.IsSubscriptionType() {
			input.GroupEntitlement[group.ID] = subscriptions[group.ID]
		} else {
			input.GroupEntitlement[group.ID] = entitled || (!group.IsExclusive && !user.RestrictPublicGroups)
		}
	}
	return input, nil
}

// ResolveDynamicPriceFactor is read-only: directory previews never reserve or
// increment usage. Anonymous quotes are explicitly public reference factors.
func (s *GatewayService) ResolveDynamicPriceFactor(ctx context.Context, input DynamicPriceFactorInput) (*DynamicPriceFactor, error) {
	if input.UserID <= 0 {
		return &DynamicPriceFactor{Factor: 1, Source: "anonymous_reference"}, nil
	}
	at := input.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	status, policy, err := s.DynamicRateStatus(ctx, input.UserID, input.GroupID, at)
	if err != nil {
		return nil, err
	}
	if policy == nil || !policy.Enabled {
		return &DynamicPriceFactor{Factor: 1, Source: "dynamic_disabled"}, nil
	}
	if input.Mode != "" && !containsDynamicRateMode(policy.IncludedModes, input.Mode) {
		return nil, ErrDynamicRateModeNotCovered
	}
	if status == nil {
		return nil, fmt.Errorf("dynamic price snapshot unavailable")
	}
	details := map[string]string{"window_id": status.WindowID, "tier_id": status.TierID, "current": status.Current.String(), "metric": status.Metric, "reset_at": status.ResetAt.UTC().Format(time.RFC3339)}
	if status.NextThreshold != nil {
		details["next_threshold"] = status.NextThreshold.String()
	}
	return &DynamicPriceFactor{Factor: status.Factor, Source: "committed_usage", Version: strconv.FormatInt(status.PolicyVersion, 10), Details: details}, nil
}

// IsModelPlazaV2Enabled reports whether the V2 catalog engine is selected.
// This is an engine-selection switch (not the plaza's master gate), so it
// defaults ON: absent / empty / unparseable → true. Only an explicit "false"
// disables V2 and falls the frontend back to the legacy plaza. The plaza's
// master visibility remains governed by model_plaza_enabled (fail-closed).
func (s *SettingService) IsModelPlazaV2Enabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return true
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{"model_plaza_v2_enabled"})
	if err != nil {
		return true
	}
	raw, ok := values["model_plaza_v2_enabled"]
	if !ok || raw == "" {
		return true
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return true
	}
	return enabled
}
