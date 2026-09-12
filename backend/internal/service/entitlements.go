package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

const (
	EntitlementTierStandard = domain.EntitlementTierStandard
	EntitlementTierPremium  = domain.EntitlementTierPremium
)

var (
	ErrEntitlementTierUnknown         = errors.New("unknown entitlement tier")
	ErrEntitlementDisabled            = errors.New("entitlement tier is disabled")
	ErrEntitlementReasonRequired      = errors.New("entitlement change reason is required")
	ErrEntitlementVersionConflict     = errors.New("entitlement configuration changed")
	ErrEntitlementIdempotencyConflict = errors.New("idempotency key was already used for a different entitlement change")
)

type EntitlementSource struct {
	Source    string     `json:"source"`
	Tier      string     `json:"tier"`
	GroupID   *int64     `json:"group_id,omitempty"`
	Rate      *float64   `json:"rate,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	Version   int64      `json:"version"`
	Explain   string     `json:"explain"`
}

type EntitlementTierGroupPolicy struct {
	GroupID        int64    `json:"group_id"`
	RateMultiplier *float64 `json:"rate_multiplier,omitempty"`
	Source         string   `json:"source"`
}

type EntitlementTierPolicy struct {
	Tier        string                       `json:"tier"`
	DisplayName string                       `json:"display_name"`
	Enabled     bool                         `json:"enabled"`
	Version     int64                        `json:"version"`
	Groups      []EntitlementTierGroupPolicy `json:"groups"`
}

type UpdateEntitlementTierPolicyInput struct {
	Tier            string                       `json:"tier"`
	DisplayName     string                       `json:"display_name"`
	Enabled         bool                         `json:"enabled"`
	ExpectedVersion *int64                       `json:"expected_version,omitempty"`
	Groups          []EntitlementTierGroupPolicy `json:"groups"`
	Reason          string                       `json:"reason"`
	RequestID       string                       `json:"request_id"`
}

type EntitlementSnapshot struct {
	UserID             int64               `json:"user_id"`
	Tier               string              `json:"tier"`
	TierEnabled        bool                `json:"tier_enabled"`
	Version            int64               `json:"version"`
	AllowedGroups      []int64             `json:"allowed_groups"`
	TierGroups         []int64             `json:"tier_groups"`
	DefaultRates       map[int64]float64   `json:"default_rates"`
	Sources            []EntitlementSource `json:"sources"`
	ManualGroups       []int64             `json:"manual_groups"`
	SubscriptionGroups []int64             `json:"subscription_groups"`
	Capabilities       *AdminCapabilities  `json:"capabilities,omitempty"`
}

type EffectiveGroupRate struct {
	GroupID    int64   `json:"group_id"`
	Multiplier float64 `json:"multiplier"`
	Source     string  `json:"source"`
	Version    int64   `json:"version"`
}

type EntitlementChangePreview struct {
	Tier                   string  `json:"tier"`
	UserIDs                []int64 `json:"user_ids"`
	AffectedUserIDs        []int64 `json:"affected_user_ids"`
	AlreadyAtTier          []int64 `json:"already_at_tier"`
	RevokedGroupIDs        []int64 `json:"revoked_group_ids,omitempty"`
	GrantedGroupIDs        []int64 `json:"granted_group_ids,omitempty"`
	PreservedManual        bool    `json:"preserved_manual_groups"`
	PreservedSubscriptions bool    `json:"preserved_subscriptions"`
}

type EntitlementChangeResult struct {
	Tier       string `json:"tier"`
	Requested  int    `json:"requested"`
	Changed    int    `json:"changed"`
	Unchanged  int    `json:"unchanged"`
	Version    int64  `json:"version"`
	Idempotent bool   `json:"idempotent"`
}

type EntitlementRepository interface {
	ResolveEntitlement(ctx context.Context, userID int64) (*EntitlementSnapshot, error)
	PreviewEntitlementChange(ctx context.Context, userIDs []int64, tier string) (*EntitlementChangePreview, error)
	ApplyEntitlementChange(ctx context.Context, userIDs []int64, tier string, actorUserID int64, reason, requestID string) (*EntitlementChangeResult, error)
	EntitlementTierDefinition(ctx context.Context, tier string) (displayName string, enabled bool, version int64, err error)
	ListEntitlementTierPolicies(ctx context.Context) ([]EntitlementTierPolicy, error)
	UpdateEntitlementTierPolicy(ctx context.Context, input UpdateEntitlementTierPolicyInput, actorUserID int64) (*EntitlementTierPolicy, error)
}

type EntitlementService struct {
	repo EntitlementRepository
}

func NewEntitlementService(repo EntitlementRepository) *EntitlementService {
	return &EntitlementService{repo: repo}
}

func NormalizeEntitlementTier(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case EntitlementTierPremium:
		return EntitlementTierPremium
	case EntitlementTierStandard, "":
		return EntitlementTierStandard
	default:
		return ""
	}
}

func (s *EntitlementService) Resolve(ctx context.Context, userID int64) (*EntitlementSnapshot, error) {
	if s == nil || s.repo == nil || userID <= 0 {
		return nil, ErrEntitlementTierUnknown
	}
	snapshot, err := s.repo.ResolveEntitlement(ctx, userID)
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		snapshot = &EntitlementSnapshot{UserID: userID, Tier: EntitlementTierStandard, TierEnabled: true, Version: 1}
	}
	if NormalizeEntitlementTier(snapshot.Tier) == "" {
		snapshot.Tier = EntitlementTierStandard
	}
	if snapshot.DefaultRates == nil {
		snapshot.DefaultRates = map[int64]float64{}
	}
	return snapshot, nil
}

// ResolveGroupRate deliberately does not mutate user_group_rates or subscriptions.
// Priority: explicit user override > entitlement tier default > group default.
func (s *EntitlementService) ResolveGroupRate(ctx context.Context, userID, groupID int64, userRate *float64, groupRate float64) (EffectiveGroupRate, error) {
	if userRate != nil {
		return EffectiveGroupRate{GroupID: groupID, Multiplier: *userRate, Source: "user_override"}, nil
	}
	snapshot, err := s.Resolve(ctx, userID)
	if err != nil {
		return EffectiveGroupRate{}, err
	}
	if rate, ok := snapshot.DefaultRates[groupID]; ok {
		return EffectiveGroupRate{GroupID: groupID, Multiplier: rate, Source: "entitlement_tier", Version: snapshot.Version}, nil
	}
	return EffectiveGroupRate{GroupID: groupID, Multiplier: groupRate, Source: "group_default", Version: snapshot.Version}, nil
}

func (s *EntitlementService) PreviewTierChange(ctx context.Context, userIDs []int64, tier string) (*EntitlementChangePreview, error) {
	tier = NormalizeEntitlementTier(tier)
	if tier == "" {
		return nil, ErrEntitlementTierUnknown
	}
	if s == nil || s.repo == nil {
		return nil, errors.New("entitlement repository is nil")
	}
	return s.repo.PreviewEntitlementChange(ctx, normalizeEntitlementUserIDs(userIDs), tier)
}

func (s *EntitlementService) ApplyTierChange(ctx context.Context, userIDs []int64, tier string, actorUserID int64, reason, requestID string) (*EntitlementChangeResult, error) {
	tier = NormalizeEntitlementTier(tier)
	reason = strings.TrimSpace(reason)
	requestID = strings.TrimSpace(requestID)
	if tier == "" {
		return nil, ErrEntitlementTierUnknown
	}
	if reason == "" {
		return nil, ErrEntitlementReasonRequired
	}
	if s == nil || s.repo == nil {
		return nil, errors.New("entitlement repository is nil")
	}
	return s.repo.ApplyEntitlementChange(ctx, normalizeEntitlementUserIDs(userIDs), tier, actorUserID, reason, requestID)
}

func (s *EntitlementService) ListTierPolicies(ctx context.Context) ([]EntitlementTierPolicy, error) {
	if s == nil || s.repo == nil {
		return nil, errors.New("entitlement repository is nil")
	}
	return s.repo.ListEntitlementTierPolicies(ctx)
}

func (s *EntitlementService) UpdateTierPolicy(ctx context.Context, input UpdateEntitlementTierPolicyInput, actorUserID int64) (*EntitlementTierPolicy, error) {
	input.Tier = NormalizeEntitlementTier(input.Tier)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Reason = strings.TrimSpace(input.Reason)
	input.RequestID = strings.TrimSpace(input.RequestID)
	if input.Tier == "" {
		return nil, ErrEntitlementTierUnknown
	}
	if input.Reason == "" {
		return nil, ErrEntitlementReasonRequired
	}
	if input.DisplayName == "" {
		input.DisplayName = input.Tier
	}
	if s == nil || s.repo == nil {
		return nil, errors.New("entitlement repository is nil")
	}
	return s.repo.UpdateEntitlementTierPolicy(ctx, input, actorUserID)
}

func normalizeEntitlementUserIDs(userIDs []int64) []int64 {
	seen := make(map[int64]struct{}, len(userIDs))
	out := make([]int64, 0, len(userIDs))
	for _, id := range userIDs {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func (s *EntitlementService) ResolveTierNames(ctx context.Context, userIDs []int64) (map[int64]string, error) {
	reader, ok := s.repo.(interface {
		ResolveEntitlementTierNames(context.Context, []int64) (map[int64]string, error)
	})
	if !ok {
		return nil, errors.New("entitlement tier summary unavailable")
	}
	return reader.ResolveEntitlementTierNames(ctx, normalizeEntitlementUserIDs(userIDs))
}
