//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// deepSeekV41Flash is the request-side model. The default billing alias maps it
// to deepseek-v4-flash only for pricing lookup; requests, overrides and logs
// keep this original name.
const deepSeekV41Flash = "deepseek-v4.1-flash"

// deepSeekV41FlashJSON mimics a remote catalog entry under the new 4.1 key with
// deliberately different numbers, so tests can prove it is never picked up by
// the default pricing path.
const deepSeekV41FlashJSON = `{
	"deepseek-v4.1-flash": {"litellm_provider": "deepseek", "mode": "chat",
		"input_cost_per_token": 9e-06, "output_cost_per_token": 18e-06,
		"cache_read_input_token_cost": 9e-07},
	"deepseek-v4-flash": {"litellm_provider": "deepseek", "mode": "chat",
		"input_cost_per_token": 1.4e-07, "output_cost_per_token": 2.8e-07,
		"cache_read_input_token_cost": 2.8e-09}
}`

func assertDeepSeekV41SameAsFlash(t *testing.T, got *ModelPricing, flash *ModelPricing) {
	t.Helper()
	require.Equal(t, flash, got, "V4.1 Flash must reuse the full V4 Flash pricing card")
}

func TestDeepSeekV41_NoDynamicServiceUsesFlashFallbackCard(t *testing.T) {
	svc := newTestBillingService()

	newPricing, err := svc.GetModelPricing(deepSeekV41Flash)
	require.NoError(t, err)
	basePricing, err := svc.GetModelPricing("deepseek-v4-flash")
	require.NoError(t, err)
	assertDeepSeekV41SameAsFlash(t, newPricing, basePricing)

	require.InDelta(t, 1.4e-7, newPricing.InputPricePerToken, 1e-18)
	require.InDelta(t, 2.8e-7, newPricing.OutputPricePerToken, 1e-18)
	require.InDelta(t, 2.8e-9, newPricing.CacheReadPricePerToken, 1e-21)
}

func TestDeepSeekV41_ProAndLegacyAliasesUnchanged(t *testing.T) {
	svc := newTestBillingService()

	pro, err := svc.GetModelPricing("deepseek-v4-pro")
	require.NoError(t, err)
	require.InDelta(t, 4.35e-7, pro.InputPricePerToken, 1e-18)
	require.InDelta(t, 8.7e-7, pro.OutputPricePerToken, 1e-18)

	// deepseek-chat / deepseek-reasoner keep mapping to V4 Flash rates.
	for _, model := range []string{"deepseek-chat", "deepseek-reasoner"} {
		pricing, err := svc.GetModelPricing(model)
		require.NoError(t, err)
		require.Equal(t, 1.4e-7, pricing.InputPricePerToken)
		require.Equal(t, 2.8e-7, pricing.OutputPricePerToken)
	}
}

func TestDeepSeekV41_DynamicOnlyFlashReusesFullCard(t *testing.T) {
	// Dynamic catalog has only the V4 Flash entry; the alias must land on it
	// instead of falling back to another hardcoded card.
	catalog := newStubPricingServiceFromJSON(t, `{
		"deepseek-v4-flash": {"litellm_provider": "deepseek", "mode": "chat",
			"input_cost_per_token": 1.4e-07, "output_cost_per_token": 2.8e-07,
			"cache_read_input_token_cost": 2.8e-09,
			"cache_creation_input_token_cost": 1.4e-06,
			"long_context_input_token_threshold": 200000,
			"long_context_input_cost_multiplier": 2,
			"long_context_output_cost_multiplier": 1.5}
	}`)
	svc := NewBillingService(&config.Config{}, catalog)

	newPricing, err := svc.GetModelPricing(deepSeekV41Flash)
	require.NoError(t, err)
	basePricing, err := svc.GetModelPricing("deepseek-v4-flash")
	require.NoError(t, err)
	assertDeepSeekV41SameAsFlash(t, newPricing, basePricing)
	require.Equal(t, 200000, newPricing.LongContextInputThreshold)
	require.Equal(t, 1.4e-6, newPricing.CacheCreationPricePerToken)
}

func TestDeepSeekV41_ConflictingDynamicEntriesUseFlashOnly(t *testing.T) {
	// Both keys exist and V4.1 differs from V4 Flash: the default path must
	// still resolve to the single V4 Flash source of truth.
	catalog := newStubPricingServiceFromJSON(t, deepSeekV41FlashJSON)
	svc := NewBillingService(&config.Config{}, catalog)

	newPricing, err := svc.GetModelPricing(deepSeekV41Flash)
	require.NoError(t, err)
	basePricing, err := svc.GetModelPricing("deepseek-v4-flash")
	require.NoError(t, err)
	assertDeepSeekV41SameAsFlash(t, newPricing, basePricing)
	require.InDelta(t, 1.4e-7, newPricing.InputPricePerToken, 1e-18)
	require.InDelta(t, 2.8e-7, newPricing.OutputPricePerToken, 1e-18)
}

func TestDeepSeekV41_FlashDynamicMissingFallsBack(t *testing.T) {
	// Only the unrelated 4.1 key exists: neither key can be identified, so the
	// new model continues with the existing V4 Flash fallback.
	catalog := newStubPricingServiceFromJSON(t, `{
		"deepseek-v4.1-flash": {"litellm_provider": "deepseek", "mode": "chat",
			"input_cost_per_token": 9e-06, "output_cost_per_token": 18e-06}
	}`)
	svc := NewBillingService(&config.Config{}, catalog)

	newPricing, err := svc.GetModelPricing(deepSeekV41Flash)
	require.NoError(t, err)
	basePricing, err := svc.GetModelPricing("deepseek-v4-flash")
	require.NoError(t, err)
	assertDeepSeekV41SameAsFlash(t, newPricing, basePricing)
	require.InDelta(t, 1.4e-7, newPricing.InputPricePerToken, 1e-18)
}

func TestDeepSeekV41_FlashTokenPricingAbsentFallsBack(t *testing.T) {
	// Flash entry has image-only prices (no token prices): token billing must
	// skip it and pair both models onto the V4 Flash fallback.
	catalog := newStubPricingServiceFromJSON(t, `{
		"deepseek-v4-flash": {"litellm_provider": "deepseek", "mode": "image",
			"output_cost_per_image": 0.04}
	}`)
	svc := NewBillingService(&config.Config{}, catalog)

	for _, model := range []string{deepSeekV41Flash, "deepseek-v4-flash"} {
		pricing, err := svc.GetModelPricing(model)
		require.NoError(t, err)
		require.InDelta(t, 1.4e-7, pricing.InputPricePerToken, 1e-18)
		require.InDelta(t, 2.8e-7, pricing.OutputPricePerToken, 1e-18)
	}
	require.True(t, svc.HasIdentifiedTokenPricing(deepSeekV41Flash))
	require.True(t, svc.HasIdentifiedTokenPricing("deepseek-v4-flash"))
}

func TestDeepSeekV41_HotReloadTracksNewSnapshot(t *testing.T) {
	oneSnapshot := `{
		"deepseek-v4-flash": {"litellm_provider": "deepseek", "mode": "chat",
			"input_cost_per_token": 1.4e-07, "output_cost_per_token": 2.8e-07,
			"cache_read_input_token_cost": 2.8e-09}
	}`
	nextSnapshot := `{
		"deepseek-v4-flash": {"litellm_provider": "deepseek", "mode": "chat",
			"input_cost_per_token": 2e-07, "output_cost_per_token": 4e-07,
			"cache_read_input_token_cost": 4e-09}
	}`
	catalog := newStubPricingServiceFromJSON(t, oneSnapshot)
	svc := NewBillingService(&config.Config{}, catalog)

	before, err := svc.GetModelPricing(deepSeekV41Flash)
	require.NoError(t, err)
	require.InDelta(t, 1.4e-7, before.InputPricePerToken, 1e-18)

	// Simulate a hot price-layer reload by swapping the in-memory snapshot.
	data, err := catalog.parsePricingData([]byte(nextSnapshot))
	require.NoError(t, err)
	catalog.pricingData = data

	afterNew, err := svc.GetModelPricing(deepSeekV41Flash)
	require.NoError(t, err)
	afterBase, err := svc.GetModelPricing("deepseek-v4-flash")
	require.NoError(t, err)
	assertDeepSeekV41SameAsFlash(t, afterNew, afterBase)
	require.InDelta(t, 2e-7, afterNew.InputPricePerToken, 1e-18)
	require.InDelta(t, 4e-7, afterNew.OutputPricePerToken, 1e-18)
}

func TestDeepSeekV41_HasIdentifiedTokenPricingExactOnly(t *testing.T) {
	svc := newTestBillingService()

	require.True(t, svc.HasIdentifiedTokenPricing(deepSeekV41Flash))
	require.True(t, svc.HasIdentifiedTokenPricing("  DeepSeek-V4.1-Flash  "))

	// Neighbouring / prefixed / suffixed unknowns stay fail-closed.
	for _, model := range []string{
		"deepseek-v4.1", "deepseek-v4.1-flash-20260901", "deepseek-v4.1-flash-pro",
		"deepseek-v4.1-flash-", "deepseek-v4.1-flash-something", "deepseek-v5-flash",
		"deepseek-reasoner-v4.1-flash", "deepseek-v4-flash-extra", "deepseek-v4.2-flash",
	} {
		require.False(t, svc.HasIdentifiedTokenPricing(model), "must stay fail-closed: %s", model)
	}
}

func TestDeepSeekV41_SameTokensSameCostAndOriginalRates(t *testing.T) {
	svc := newTestBillingService()
	tokens := UsageTokens{
		InputTokens:         1234,
		OutputTokens:        567,
		CacheCreationTokens: 89,
		CacheReadTokens:     2000,
	}

	newCost, err := svc.CalculateCost(deepSeekV41Flash, tokens, 1)
	require.NoError(t, err)
	baseCost, err := svc.CalculateCost("deepseek-v4-flash", tokens, 1)
	require.NoError(t, err)
	require.InDelta(t, baseCost.TotalCost, newCost.TotalCost, 1e-12)
	require.InDelta(t, baseCost.ActualCost, newCost.ActualCost, 1e-12)

	// Original Flash/Pro card values are untouched by the alias.
	require.InDelta(t, 1234*1.4e-7+567*2.8e-7+2000*2.8e-9, baseCost.TotalCost, 1e-15)
	pro, err := svc.CalculateCost("deepseek-v4-pro", tokens, 1)
	require.NoError(t, err)
	require.InDelta(t, 1234*4.35e-7+567*8.7e-7+2000*3.625e-9, pro.TotalCost, 1e-15)
}

func TestDeepSeekV41_GroupOverrideWinsByOriginalID(t *testing.T) {
	_, resolver := newTokenCostTestEnv(t, "openai", []ChannelModelPricing{{
		Platform:    PlatformOpenAI,
		Models:      []string{deepSeekV41Flash},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(5e-6),
		OutputPrice: testPtrFloat64(10e-6),
	}}, newStubPricingServiceFromJSON(t, deepSeekV41FlashJSON))

	group := &Group{ID: 100, Platform: "openai",
		ModelPricing: []ChannelModelPricing{{
			Models:      []string{deepSeekV41Flash},
			BillingMode: BillingModeToken,
			InputPrice:  testPtrFloat64(7e-6),
			OutputPrice: testPtrFloat64(14e-6),
		}},
	}
	gid := group.ID
	resolved := resolver.Resolve(context.Background(), PricingInput{
		Model: deepSeekV41Flash, GroupID: &gid, Group: group,
	})
	require.Equal(t, PricingSourceGroup, resolved.Source)
	require.NotNil(t, resolved.BasePricing)
	require.InDelta(t, 7e-6, resolved.BasePricing.InputPricePerToken, 1e-18)
	require.InDelta(t, 14e-6, resolved.BasePricing.OutputPricePerToken, 1e-18)
}

func TestDeepSeekV41_ChannelOverrideWinsByOriginalID(t *testing.T) {
	// Channel pricing matches the original V4.1 model only; the alias must not
	// redirect the override lookup to the V4 Flash key.
	_, resolver := newTokenCostTestEnv(t, "openai", []ChannelModelPricing{{
		Platform:    PlatformOpenAI,
		Models:      []string{deepSeekV41Flash},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(5e-6),
		OutputPrice: testPtrFloat64(10e-6),
	}}, newStubPricingServiceFromJSON(t, deepSeekV41FlashJSON))

	group := &Group{ID: 100, Platform: "openai"}
	gid := group.ID
	resolved := resolver.Resolve(context.Background(), PricingInput{
		Model: deepSeekV41Flash, GroupID: &gid, Group: group,
	})
	require.Equal(t, PricingSourceChannel, resolved.Source)
	require.InDelta(t, 5e-6, resolved.BasePricing.InputPricePerToken, 1e-18)
	require.InDelta(t, 10e-6, resolved.BasePricing.OutputPricePerToken, 1e-18)
}

func TestDeepSeekV41_ChannelOverrideOnlyForFlashDoesNotLeak(t *testing.T) {
	// A channel card configured for V4 Flash must NOT be picked up by a V4.1
	// request, because the override lookup uses the original request model.
	_, resolver := newTokenCostTestEnv(t, "openai", []ChannelModelPricing{{
		Platform:    PlatformOpenAI,
		Models:      []string{"deepseek-v4-flash"},
		BillingMode: BillingModeToken,
		InputPrice:  testPtrFloat64(5e-6),
		OutputPrice: testPtrFloat64(10e-6),
	}}, newStubPricingServiceFromJSON(t, deepSeekV41FlashJSON))

	group := &Group{ID: 100, Platform: "openai"}
	gid := group.ID
	resolved := resolver.Resolve(context.Background(), PricingInput{
		Model: deepSeekV41Flash, GroupID: &gid, Group: group,
	})
	require.Equal(t, PricingSourceLiteLLM, resolved.Source)
	require.InDelta(t, 1.4e-7, resolved.BasePricing.InputPricePerToken, 1e-18)
	require.InDelta(t, 2.8e-7, resolved.BasePricing.OutputPricePerToken, 1e-18)
}

func TestDeepSeekV41_AccountStatsOverrideByOriginalID(t *testing.T) {
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 50}
	want := 100*3e-6 + 50*6e-6

	channel := &Channel{
		ID:     1,
		Status: StatusActive,
		AccountStatsPricingRules: []AccountStatsPricingRule{{
			GroupIDs: []int64{10},
			Pricing: []ChannelModelPricing{{
				Models:      []string{deepSeekV41Flash},
				BillingMode: BillingModeToken,
				InputPrice:  testPtrFloat64(3e-6),
				OutputPrice: testPtrFloat64(6e-6),
			}},
		}},
	}
	cs := newTestChannelServiceForStats(t, channel, 10, "openai")

	got := resolveAccountStatsCost(
		context.Background(), cs, newTestBillingService(),
		1, 10, deepSeekV41Flash, tokens, 1, 0, "",
	)
	require.NotNil(t, got)
	require.InDelta(t, want, *got, 1e-12)

	// The V4.1 rule must not leak to V4 Flash; without an own rule, priority 3
	// uses the default model pricing file instead.
	base := resolveAccountStatsCost(
		context.Background(), cs, newTestBillingService(),
		1, 10, "deepseek-v4-flash", tokens, 1, 0, "",
	)
	require.NotNil(t, base)
	require.InDelta(t, 100*1.4e-7+50*2.8e-7, *base, 1e-12)
}
