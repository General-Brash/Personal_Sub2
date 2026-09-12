package service

import (
	"context"
	"sort"
)

// ModelAccessProvider is the explicit integration point for W04/W09-backed
// entitlements and subscriptions. Implementations may include premium tier,
// subscription validity, and dynamic policy versions without changing catalog
// or quote semantics.
type ModelAccessProvider interface {
	ResolveModelAccess(ctx context.Context, userID int64, anonymous bool) (ModelAccessInput, error)
}

// ModelAvailabilityState is the authoritative per-user state. It must not be
// collapsed with catalog existence or with price availability.
type ModelAvailabilityState string

const (
	ModelAvailabilityCatalogOnly            ModelAvailabilityState = ModelCatalogStateCatalogOnly
	ModelAvailabilityEligible               ModelAvailabilityState = ModelCatalogStateEligible
	ModelAvailabilityTemporarilyUnavailable ModelAvailabilityState = ModelCatalogStateTemporarilyUnavailable
	ModelAvailabilityNotEntitled            ModelAvailabilityState = ModelCatalogStateNotEntitled
	ModelAvailabilityUnknown                ModelAvailabilityState = ModelCatalogStateUnknown
)

type ModelAccessInput struct {
	Anonymous       bool
	AllowedGroupIDs map[int64]struct{}
	// GroupEntitlement is optional and lets a future entitlement resolver
	// explicitly deny a group even when it is otherwise public.
	GroupEntitlement map[int64]bool
}

type ModelAvailabilityResult struct {
	State             ModelAvailabilityState `json:"availability_state"`
	Eligibility       ModelAvailabilityState `json:"eligibility"`
	ReasonCode        string                 `json:"reason_code,omitempty"`
	EligibleGroupIDs  []int64                `json:"eligible_group_ids,omitempty"`
	UnavailableGroups []int64                `json:"unavailable_group_ids,omitempty"`
	UnknownGroupIDs   []int64                `json:"unknown_group_ids,omitempty"`
}

type ModelAvailabilityResolver struct{}

func NewModelAvailabilityResolver() *ModelAvailabilityResolver {
	return &ModelAvailabilityResolver{}
}

// Resolve is intentionally conservative: a route without verified scheduling
// information is unknown, never eligible.
func (r *ModelAvailabilityResolver) Resolve(item ModelCatalogItem, access ModelAccessInput) ModelAvailabilityResult {
	result := ModelAvailabilityResult{State: ModelAvailabilityUnknown, Eligibility: ModelAvailabilityUnknown, ReasonCode: "no_route"}
	if len(item.Routes) == 0 {
		return result
	}
	if access.Anonymous {
		visible := false
		for _, route := range item.Routes {
			if !route.Exclusive {
				visible = true
				break
			}
		}
		if !visible {
			result.State = ModelAvailabilityNotEntitled
			result.Eligibility = ModelAvailabilityNotEntitled
			result.ReasonCode = "private_group"
			return result
		}
		result.State = ModelAvailabilityCatalogOnly
		result.Eligibility = ModelAvailabilityCatalogOnly
		result.ReasonCode = "authentication_required"
		return result
	}

	allowed := make([]ModelCatalogRoute, 0, len(item.Routes))
	for _, route := range item.Routes {
		if modelRouteAllowed(route, access) {
			allowed = append(allowed, route)
		}
	}
	if len(allowed) == 0 {
		result.State = ModelAvailabilityNotEntitled
		result.Eligibility = ModelAvailabilityNotEntitled
		result.ReasonCode = "group_not_entitled"
		return result
	}

	unknownCount := 0
	unavailableCount := 0
	for _, route := range allowed {
		if !route.AvailabilityKnown {
			unknownCount++
			result.UnknownGroupIDs = append(result.UnknownGroupIDs, route.GroupID)
			continue
		}
		if route.Schedulable {
			result.EligibleGroupIDs = append(result.EligibleGroupIDs, route.GroupID)
			continue
		}
		unavailableCount++
		result.UnavailableGroups = append(result.UnavailableGroups, route.GroupID)
	}
	result.EligibleGroupIDs = uniqueSortedIDs(result.EligibleGroupIDs)
	result.UnavailableGroups = uniqueSortedIDs(result.UnavailableGroups)
	result.UnknownGroupIDs = uniqueSortedIDs(result.UnknownGroupIDs)
	if len(result.EligibleGroupIDs) > 0 {
		result.State = ModelAvailabilityEligible
		result.Eligibility = ModelAvailabilityEligible
		result.ReasonCode = "schedulable_route"
		return result
	}
	if unknownCount > 0 && unavailableCount == 0 {
		result.State = ModelAvailabilityUnknown
		result.Eligibility = ModelAvailabilityUnknown
		result.ReasonCode = "route_state_unknown"
		return result
	}
	if unknownCount > 0 {
		result.State = ModelAvailabilityUnknown
		result.Eligibility = ModelAvailabilityUnknown
		result.ReasonCode = "mixed_route_state"
		return result
	}
	result.State = ModelAvailabilityTemporarilyUnavailable
	result.Eligibility = ModelAvailabilityTemporarilyUnavailable
	result.ReasonCode = "no_schedulable_route"
	return result
}

func modelRouteAllowed(route ModelCatalogRoute, access ModelAccessInput) bool {
	if allowed, ok := access.GroupEntitlement[route.GroupID]; ok {
		return allowed
	}
	if _, ok := access.AllowedGroupIDs[route.GroupID]; ok {
		return true
	}
	if route.Exclusive || route.Subscription {
		return false
	}
	return true
}

func uniqueSortedIDs(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	out := values[:0]
	var previous int64
	for i, value := range values {
		if i > 0 && value == previous {
			continue
		}
		previous = value
		out = append(out, value)
	}
	return out
}
