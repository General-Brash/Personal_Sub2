package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// RuntimeModelCatalogGroupSource is implemented by GroupService.
type RuntimeModelCatalogGroupSource interface {
	ListActive(ctx context.Context) ([]Group, error)
}

// RuntimeModelCatalogAccountSource is implemented by AccountService.
type RuntimeModelCatalogAccountSource interface {
	ListByGroup(ctx context.Context, groupID int64) ([]Account, error)
}

// RuntimeModelCatalogCompositeSource is implemented by the composite route
// repository. It supplies explicit public-model -> provider/upstream routes.
type RuntimeModelCatalogCompositeSource interface {
	ListByGroup(ctx context.Context, groupID int64, includeDisabled bool) ([]CompositeModelRoute, error)
}

// RuntimeModelCatalogSource builds the independent catalog from active groups
// and the accounts actually bound to those groups. It intentionally does not
// read channel display rows or models_list_config.
type RuntimeModelCatalogSource struct {
	groups    RuntimeModelCatalogGroupSource
	accounts  RuntimeModelCatalogAccountSource
	composite RuntimeModelCatalogCompositeSource
	now       func() time.Time
}

func NewRuntimeModelCatalogSource(groups RuntimeModelCatalogGroupSource, accounts RuntimeModelCatalogAccountSource) *RuntimeModelCatalogSource {
	return &RuntimeModelCatalogSource{groups: groups, accounts: accounts, now: time.Now}
}

func (s *RuntimeModelCatalogSource) SetCompositeRouteSource(source RuntimeModelCatalogCompositeSource) {
	if s != nil {
		s.composite = source
	}
}

func (s *RuntimeModelCatalogSource) SetClock(now func() time.Time) {
	if s != nil && now != nil {
		s.now = now
	}
}

func (s *RuntimeModelCatalogSource) ListModelCatalogSource(ctx context.Context) ([]ModelCatalogSourceItem, error) {
	if s == nil || s.groups == nil || s.accounts == nil {
		return nil, fmt.Errorf("runtime model catalog source is not configured")
	}
	groups, err := s.groups.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("list catalog groups: %w", err)
	}
	items := make(map[string]*ModelCatalogSourceItem)
	for i := range groups {
		group := &groups[i]
		if group == nil || group.ID <= 0 || !group.IsActive() {
			continue
		}
		accounts, err := s.accounts.ListByGroup(ctx, group.ID)
		if err != nil {
			return nil, fmt.Errorf("list catalog accounts for group %d: %w", group.ID, err)
		}
		var compositeRoutes []CompositeModelRoute
		if s.composite != nil && strings.EqualFold(strings.TrimSpace(group.Platform), PlatformComposite) {
			compositeRoutes, err = s.composite.ListByGroup(ctx, group.ID, false)
			if err != nil {
				return nil, fmt.Errorf("list composite catalog routes for group %d: %w", group.ID, err)
			}
		}
		seeds := runtimeCatalogModelSeeds(group, accounts)
		if strings.EqualFold(group.Platform, PlatformComposite) {
			seeds = map[string]runtimeCatalogSeed{} // public capabilities come from routes, not every bound account alias
		}
		for i := range compositeRoutes {
			route := compositeRoutes[i]
			if !route.Enabled || strings.TrimSpace(route.PublicModel) == "" || normalizeCompositeRouteMatchType(route.MatchType) == CompositeRouteMatchPrefix {
				continue
			}
			modelID := strings.TrimSpace(route.PublicModel)
			platform := strings.TrimSpace(route.TargetPlatform)
			upstream := strings.TrimSpace(route.UpstreamModel)
			if upstream == "" {
				upstream = modelID
			}
			seeds[catalogSourceItemKey(modelID, platform)] = runtimeCatalogSeed{
				modelID: modelID, platform: platform, upstream: upstream, aliased: !strings.EqualFold(upstream, modelID),
			}
		}
		for _, seed := range seeds {
			key := catalogSourceItemKey(seed.modelID, seed.platform)
			item := items[key]
			if item == nil {
				item = &ModelCatalogSourceItem{
					ModelID:        seed.modelID,
					DisplayName:    seed.modelID,
					Platform:       seed.platform,
					SourceVersions: map[string]string{},
				}
				items[key] = item
			}
			route := s.runtimeCatalogRoute(group, accounts, seed.modelID, seed.upstream, seed.platform, s.now())
			if compositeRoute, ok := findCompositeCatalogRoute(compositeRoutes, seed.modelID); ok && strings.EqualFold(strings.TrimSpace(compositeRoute.TargetPlatform), seed.platform) {
				upstream := strings.TrimSpace(compositeRoute.UpstreamModel)
				if upstream == "" {
					upstream = seed.modelID
				}
				route.RouteKind = "composite"
				route.UpstreamModelID = upstream
				route.Alias = !strings.EqualFold(upstream, seed.modelID)
				route.SourceVersion = compositeRoute.UpdatedAt.UTC().Format(time.RFC3339Nano)
			}
			item.Routes = mergeCatalogRoutes(item.Routes, []ModelCatalogRoute{route})
			item.SourceVersions[fmt.Sprintf("group:%d", group.ID)] = groupVersion(group)
			for j := range accounts {
				item.SourceVersions[fmt.Sprintf("account:%d", accounts[j].ID)] = accountVersion(&accounts[j])
			}
		}
	}
	out := make([]ModelCatalogSourceItem, 0, len(items))
	for _, item := range items {
		sort.SliceStable(item.Routes, func(i, j int) bool { return item.Routes[i].GroupID < item.Routes[j].GroupID })
		out = append(out, *item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ModelID != out[j].ModelID {
			return out[i].ModelID < out[j].ModelID
		}
		return out[i].Platform < out[j].Platform
	})
	return out, nil
}

type runtimeCatalogSeed struct {
	modelID  string
	platform string
	upstream string
	aliased  bool
}

func runtimeCatalogModelSeeds(group *Group, accounts []Account) map[string]runtimeCatalogSeed {
	seeds := make(map[string]runtimeCatalogSeed)
	if group == nil {
		return seeds
	}
	add := func(modelID, platform, upstream string) {
		modelID = strings.TrimSpace(modelID)
		platform = strings.ToLower(strings.TrimSpace(platform))
		upstream = strings.TrimSpace(upstream)
		if modelID == "" || strings.Contains(modelID, "*") || platform == "" {
			return
		}
		if strings.EqualFold(upstream, modelID) {
			upstream = ""
		}
		key := catalogSourceItemKey(modelID, platform)
		if _, exists := seeds[key]; exists {
			return
		}
		seeds[key] = runtimeCatalogSeed{modelID: modelID, platform: platform, upstream: upstream, aliased: upstream != ""}
	}
	for i := range accounts {
		account := &accounts[i]
		platform := strings.TrimSpace(group.Platform)
		if platform == "" || strings.EqualFold(platform, PlatformComposite) {
			platform = strings.TrimSpace(account.Platform)
		}
		if platform == "" || strings.EqualFold(platform, PlatformComposite) {
			continue
		}
		mapping := account.GetModelMapping()
		if len(mapping) == 0 {
			for _, modelID := range defaultModelsListCandidateIDs(platform) {
				add(modelID, platform, "")
			}
			continue
		}
		before := len(seeds)
		for requested, upstream := range mapping {
			add(requested, platform, upstream)
		}
		if len(seeds) == before {
			// A wildcard-only mapping cannot be expanded into a truthful finite
			// catalog, so use the platform's explicit default capability list.
			for _, modelID := range defaultModelsListCandidateIDs(platform) {
				add(modelID, platform, "")
			}
		}
	}
	if len(seeds) == 0 && !strings.EqualFold(strings.TrimSpace(group.Platform), PlatformComposite) {
		platform := strings.TrimSpace(group.Platform)
		for _, modelID := range defaultModelsListCandidateIDs(platform) {
			add(modelID, platform, "")
		}
	}
	return seeds
}

func (s *RuntimeModelCatalogSource) runtimeCatalogRoute(group *Group, accounts []Account, modelID, upstream, targetPlatform string, now time.Time) ModelCatalogRoute {
	route := ModelCatalogRoute{
		GroupID:             group.ID,
		GroupName:           group.Name,
		GroupPlatform:       group.Platform,
		RequestModelID:      modelID,
		UpstreamModelID:     upstream,
		RouteKind:           "direct",
		Exclusive:           group.IsExclusive,
		Subscription:        group.IsSubscriptionType(),
		AvailabilityKnown:   true,
		AvailabilityReason:  "no_account",
		SourceVersion:       groupVersion(group),
		ChannelPricingModel: modelID,
		Group:               group,
	}
	if route.UpstreamModelID != "" && !strings.EqualFold(route.UpstreamModelID, modelID) {
		route.RouteKind = "alias"
		route.Alias = true
	}
	if strings.EqualFold(strings.TrimSpace(group.Platform), PlatformComposite) {
		if strings.TrimSpace(targetPlatform) == "" {
			route.AvailabilityKnown = false
			route.AvailabilityReason = "composite_target_unknown"
			return route
		}
		route.RouteKind = "composite"
	}
	supporting := 0
	schedulable := 0
	rateLimited := false
	for i := range accounts {
		account := &accounts[i]
		if !runtimeAccountMatchesTargetPlatform(account, targetPlatform) || !runtimeAccountSupportsModel(account, modelID) {
			continue
		}
		supporting++
		if runtimeAccountSchedulable(account, now) {
			schedulable++
		} else if account.RateLimitedAt != nil && account.RateLimitResetAt != nil && now.Before(*account.RateLimitResetAt) {
			rateLimited = true
		}
	}
	if supporting == 0 {
		return route
	}
	route.AvailabilityReason = "no_schedulable_account"
	if schedulable > 0 {
		route.Schedulable = true
		route.AvailabilityReason = ""
	} else if rateLimited {
		route.AvailabilityReason = "rate_limited"
	}
	return route
}

func runtimeAccountMatchesTargetPlatform(account *Account, targetPlatform string) bool {
	if account == nil {
		return false
	}
	targetPlatform = strings.TrimSpace(targetPlatform)
	if targetPlatform == "" {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(account.Platform), targetPlatform)
}

func findCompositeCatalogRoute(routes []CompositeModelRoute, modelID string) (CompositeModelRoute, bool) {
	var picked CompositeModelRoute
	found := false
	for i := range routes {
		route := routes[i]
		if !route.Enabled || strings.TrimSpace(route.PublicModel) != modelID || normalizeCompositeRouteMatchType(route.MatchType) != CompositeRouteMatchExact {
			continue
		}
		if !found || route.Priority < picked.Priority || (route.Priority == picked.Priority && route.ID < picked.ID) {
			picked = route
			found = true
		}
	}
	return picked, found
}

func runtimeAccountSupportsModel(account *Account, modelID string) bool {
	if account == nil || account.Status != StatusActive || !account.Schedulable {
		return false
	}
	return account.IsModelSupported(modelID)
}

func runtimeAccountSchedulable(account *Account, now time.Time) bool {
	if account == nil || account.Status != StatusActive || !account.Schedulable {
		return false
	}
	if account.RateLimitedAt != nil && account.RateLimitResetAt != nil && now.Before(*account.RateLimitResetAt) {
		return false
	}
	if account.OverloadUntil != nil && now.Before(*account.OverloadUntil) {
		return false
	}
	if account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) {
		return false
	}
	return true
}

func catalogSourceItemKey(modelID, platform string) string {
	return strings.ToLower(strings.TrimSpace(modelID)) + "\x00" + strings.ToLower(strings.TrimSpace(platform))
}

func groupVersion(group *Group) string {
	if group == nil {
		return ""
	}
	return group.UpdatedAt.UTC().Format(time.RFC3339Nano)
}

func accountVersion(account *Account) string {
	if account == nil {
		return ""
	}
	return account.UpdatedAt.UTC().Format(time.RFC3339Nano)
}
