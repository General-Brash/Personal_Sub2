//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type catalogSourceStub struct{ items []ModelCatalogSourceItem }

func (s catalogSourceStub) ListModelCatalogSource(context.Context) ([]ModelCatalogSourceItem, error) {
	return s.items, nil
}

func TestModelCatalogService_MergesRoutesWithoutChannelDisplayRows(t *testing.T) {
	service := NewModelCatalogService(catalogSourceStub{items: []ModelCatalogSourceItem{
		{ModelID: "claude-sonnet-4", Platform: "anthropic", Routes: []ModelCatalogRoute{{GroupID: 1, RequestModelID: "claude-sonnet-4", Schedulable: true, AvailabilityKnown: true}}},
		{ModelID: "claude-sonnet-4", Platform: "anthropic", Routes: []ModelCatalogRoute{{GroupID: 2, RequestModelID: "claude-sonnet-4", RouteKind: "alias", Alias: true, UpstreamModelID: "upstream-sonnet", Schedulable: false, AvailabilityKnown: true}}},
	}})
	items, err := service.List(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Routes, 2)
	require.Equal(t, "claude-sonnet-4", items[0].DisplayName)
}

func TestRuntimeModelCatalogSource_UsesGroupsAndAccountsNotModelsListConfig(t *testing.T) {
	groups := runtimeCatalogGroupSourceStub{groups: []Group{{
		ID: 10, Name: "public", Platform: "anthropic", Status: StatusActive,
		ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}}},
	}}}
	accounts := runtimeCatalogAccountSourceStub{accounts: map[int64][]Account{10: {{
		ID: 20, Platform: "anthropic", Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-4": "claude-sonnet-4-upstream"}},
	}}}}
	source := NewRuntimeModelCatalogSource(groups, accounts)
	items, err := source.ListModelCatalogSource(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Routes, 1)
	require.True(t, items[0].Routes[0].Schedulable)
	require.True(t, items[0].Routes[0].Alias)
	require.Equal(t, "claude-sonnet-4-upstream", items[0].Routes[0].UpstreamModelID)
}

type runtimeCatalogGroupSourceStub struct{ groups []Group }

func (s runtimeCatalogGroupSourceStub) ListActive(context.Context) ([]Group, error) {
	return s.groups, nil
}

type runtimeCatalogAccountSourceStub struct{ accounts map[int64][]Account }

func (s runtimeCatalogAccountSourceStub) ListByGroup(_ context.Context, groupID int64) ([]Account, error) {
	return s.accounts[groupID], nil
}

func TestRuntimeModelCatalogSource_UsesExplicitCompositeRoutes(t *testing.T) {
	groups := runtimeCatalogGroupSourceStub{groups: []Group{{
		ID: 11, Name: "composite", Platform: PlatformComposite, Status: StatusActive,
	}}}
	accounts := runtimeCatalogAccountSourceStub{accounts: map[int64][]Account{11: {{
		ID: 21, Platform: "anthropic", Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"model_mapping": map[string]any{"router-model": "actual-model"}},
	}}}}
	source := NewRuntimeModelCatalogSource(groups, accounts)
	source.SetCompositeRouteSource(runtimeCatalogCompositeSourceStub{routes: map[int64][]CompositeModelRoute{11: {{
		ID: 1, GroupID: 11, PublicModel: "router-model", MatchType: CompositeRouteMatchExact,
		TargetPlatform: "anthropic", UpstreamModel: "actual-model", Endpoint: CompositeRouteEndpointAny, Enabled: true,
	}}}})
	items, err := source.ListModelCatalogSource(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Routes, 1)
	require.Equal(t, "composite", items[0].Routes[0].RouteKind)
	require.True(t, items[0].Routes[0].Alias)
	require.Equal(t, "actual-model", items[0].Routes[0].UpstreamModelID)
}

type runtimeCatalogCompositeSourceStub struct {
	routes map[int64][]CompositeModelRoute
}

func (s runtimeCatalogCompositeSourceStub) ListByGroup(_ context.Context, groupID int64, _ bool) ([]CompositeModelRoute, error) {
	return s.routes[groupID], nil
}

func TestRuntimeModelCatalogSource_PricingOnlyEntryIsNotCallableCapability(t *testing.T) {
	groups := runtimeCatalogGroupSourceStub{groups: []Group{{
		ID: 12, Name: "pricing-only", Platform: PlatformAnthropic, Status: StatusActive,
		ModelPricing: []ChannelModelPricing{{Models: []string{"priced-only-model"}}},
	}}}
	source := NewRuntimeModelCatalogSource(groups, runtimeCatalogAccountSourceStub{accounts: map[int64][]Account{12: {}}})
	items, err := source.ListModelCatalogSource(context.Background())
	require.NoError(t, err)
	for _, item := range items {
		require.NotEqual(t, "priced-only-model", item.ModelID)
	}
	require.NotEmpty(t, items, "platform defaults remain a truthful catalog even without a channel row")
}

func TestRuntimeModelCatalogSource_CompositeUsesRouteTargetPlatformAccounts(t *testing.T) {
	groups := runtimeCatalogGroupSourceStub{groups: []Group{{
		ID: 13, Name: "composite", Platform: PlatformComposite, Status: StatusActive,
	}}}
	accounts := runtimeCatalogAccountSourceStub{accounts: map[int64][]Account{13: {
		{ID: 31, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"router-model": "router-model"}}},
		{ID: 32, Platform: PlatformGemini, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"model_mapping": map[string]any{"router-model": "router-model"}}},
	}}}
	source := NewRuntimeModelCatalogSource(groups, accounts)
	source.SetCompositeRouteSource(runtimeCatalogCompositeSourceStub{routes: map[int64][]CompositeModelRoute{13: {{
		ID: 2, GroupID: 13, PublicModel: "router-model", MatchType: CompositeRouteMatchExact,
		TargetPlatform: PlatformOpenAI, UpstreamModel: "router-model", Endpoint: CompositeRouteEndpointAny, Enabled: true,
	}}}})
	items, err := source.ListModelCatalogSource(context.Background())
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Len(t, items[0].Routes, 1)
	require.True(t, items[0].Routes[0].Schedulable)
	require.Equal(t, PlatformOpenAI, items[0].Platform)
}
