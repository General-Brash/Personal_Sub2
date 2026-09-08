package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// gpt55OverrideCatalogJSON 镜像真实目录形态：长上下文以 above_272k 绝对价字段表达。
const gpt55OverrideCatalogJSON = `{
	"gpt-5.5": {"litellm_provider": "openai", "mode": "chat",
		"input_cost_per_token": 5e-06, "input_cost_per_token_priority": 1.25e-05,
		"output_cost_per_token": 3e-05, "output_cost_per_token_priority": 7.5e-05,
		"cache_read_input_token_cost": 5e-07,
		"input_cost_per_token_above_272k_tokens": 1e-05,
		"output_cost_per_token_above_272k_tokens": 4.5e-05,
		"cache_read_input_token_cost_above_272k_tokens": 1e-06},
	"gpt-5.4": {"litellm_provider": "openai", "mode": "chat",
		"input_cost_per_token": 2.5e-06, "output_cost_per_token": 1.5e-05,
		"cache_read_input_token_cost": 2.5e-07,
		"input_cost_per_token_above_272k_tokens": 5e-06,
		"output_cost_per_token_above_272k_tokens": 2.25e-05}
}`

func newPricingServiceWithOverride(t *testing.T, overrideJSON string) *PricingService {
	t.Helper()
	path := filepath.Join(t.TempDir(), "overrides.json")
	require.NoError(t, os.WriteFile(path, []byte(overrideJSON), 0644))
	svc := &PricingService{cfg: &config.Config{}}
	svc.cfg.Pricing.OverrideFile = path
	return svc
}

// override 的旗舰用例：显式 threshold=0 压住 above 折算，把目录条目的阶梯关成标准价。
func TestPricingOverride_ExplicitZeroThresholdDisablesCatalogLadder(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"gpt-5.5": {"long_context_input_token_threshold": 0}}`)
	data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
	require.NoError(t, err)

	patched := data["gpt-5.5"]
	require.NotNil(t, patched)
	require.Zero(t, patched.LongContextInputTokenThreshold)
	require.Zero(t, patched.LongContextInputCostMultiplier)
	require.InDelta(t, 5e-6, patched.InputCostPerToken, 1e-12, "补丁不得影响基础价")
	require.InDelta(t, 3e-5, patched.OutputCostPerToken, 1e-12)
	require.Equal(t, 272000, data["gpt-5.4"].LongContextInputTokenThreshold, "未覆盖的模型保持目录阶梯")

	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)
	tokens := UsageTokens{InputTokens: 300000, OutputTokens: 1000, CacheReadTokens: 10000}
	cost, err := billing.CalculateCost("gpt-5.5", tokens, 1)
	require.NoError(t, err)
	require.False(t, cost.LongContextBillingApplied)
	require.InDelta(t, 300000*5e-6, cost.InputCost, 1e-10)
	require.InDelta(t, 1000*3e-5, cost.OutputCost, 1e-10)
	require.InDelta(t, 10000*5e-7, cost.CacheReadCost, 1e-10)
}

func TestPricingOverride_LongContextThresholdPresenceDrivesBillingPolicy(t *testing.T) {
	tests := []struct {
		name          string
		overrideJSON  string
		wantThreshold int
		wantPresent   bool
		wantApplied   bool
	}{
		{name: "missing keeps catalog ladder", overrideJSON: `{}`, wantThreshold: 272000, wantApplied: true},
		{name: "explicit zero disables ladder", overrideJSON: `{"gpt-5.5": {"long_context_input_token_threshold": 0}}`, wantApplied: false, wantPresent: true},
		{name: "positive threshold enables ladder", overrideJSON: `{"gpt-5.5": {"long_context_input_token_threshold": 300000}}`, wantThreshold: 300000, wantApplied: true, wantPresent: true},
		{name: "null removes override and exposes catalog ladder", overrideJSON: `{"gpt-5.5": {"long_context_input_token_threshold": null}}`, wantThreshold: 272000, wantApplied: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newPricingServiceWithOverride(t, tt.overrideJSON)
			data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
			require.NoError(t, err)

			parsed := data["gpt-5.5"]
			require.NotNil(t, parsed)
			require.Equal(t, tt.wantThreshold, parsed.LongContextInputTokenThreshold)
			require.Equal(t, tt.wantPresent, parsed.LongContextInputTokenThresholdPresent)

			svc.pricingData = data
			billing := NewBillingService(&config.Config{}, svc)
			pricing, err := billing.GetModelPricing("gpt-5.5")
			require.NoError(t, err)
			require.Equal(t, tt.wantThreshold, pricing.LongContextInputThreshold)
			require.Equal(t, tt.wantPresent, pricing.LongContextInputThresholdPresent)
			if tt.name == "positive threshold enables ladder" {
				require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
				require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)
			}

			cost, err := billing.CalculateCost("gpt-5.5", UsageTokens{InputTokens: 300001, OutputTokens: 1}, 1)
			require.NoError(t, err)
			require.Equal(t, tt.wantApplied, cost.LongContextBillingApplied)

			// The resolver/unified path must carry the same source-presence bit;
			// otherwise it could re-enable the ladder after legacy billing disables it.
			resolver := NewModelPricingResolver(nil, billing)
			unifiedCost, err := billing.CalculateCostUnified(CostInput{
				Ctx: context.Background(), Model: "gpt-5.5",
				Tokens:         UsageTokens{InputTokens: 300001, OutputTokens: 1},
				RateMultiplier: 1, Resolver: resolver,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantApplied, unifiedCost.LongContextBillingApplied)
		})
	}
}

func TestPricingOverride_ExplicitZeroSurvivesChannelResolver(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"gpt-5.5": {"long_context_input_token_threshold": 0}}`)
	data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
	require.NoError(t, err)
	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)

	groupID := int64(77)
	gpt55Input, gpt55Output := 10e-6, 40e-6
	gpt54Input, gpt54Output := 2e-6, 8e-6
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.channelByGroupID[groupID] = &Channel{ID: 1, Status: StatusActive}
	cache.groupPlatform[groupID] = PlatformOpenAI
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformOpenAI, model: "gpt-5.5"}] = &ChannelModelPricing{
		Platform: PlatformOpenAI, Models: []string{"gpt-5.5"},
		InputPrice: &gpt55Input, OutputPrice: &gpt55Output,
	}
	cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformOpenAI, model: "gpt-5.4"}] = &ChannelModelPricing{
		Platform: PlatformOpenAI, Models: []string{"gpt-5.4"},
		InputPrice: &gpt54Input, OutputPrice: &gpt54Output,
	}
	channels := &ChannelService{}
	channels.cache.Store(cache)
	resolver := NewModelPricingResolver(channels, billing)
	group := &Group{ID: groupID, Platform: PlatformOpenAI, LongContextPricingEnabled: true}
	tokens := UsageTokens{InputTokens: 300000, OutputTokens: 1000}

	for _, tt := range []struct {
		model         string
		wantThreshold int
		wantPresent   bool
		wantApplied   bool
		wantInput     float64
		wantOutput    float64
	}{
		{
			model: "gpt-5.5", wantThreshold: 0, wantPresent: true, wantApplied: false,
			wantInput: 300000 * gpt55Input, wantOutput: 1000 * gpt55Output,
		},
		{
			model: "gpt-5.4", wantThreshold: 272000, wantPresent: false, wantApplied: true,
			wantInput: 300000 * gpt54Input * 2, wantOutput: 1000 * gpt54Output * 1.5,
		},
	} {
		t.Run(tt.model, func(t *testing.T) {
			resolved := resolver.Resolve(context.Background(), PricingInput{
				Model: tt.model, GroupID: &groupID, Group: group,
			})
			require.NotNil(t, resolved)
			require.Equal(t, PricingSourceChannel, resolved.Source)
			require.NotNil(t, resolved.BasePricing)
			require.Equal(t, tt.wantThreshold, resolved.BasePricing.LongContextInputThreshold)
			require.Equal(t, tt.wantPresent, resolved.BasePricing.LongContextInputThresholdPresent)

			cost, err := billing.CalculateCostUnified(CostInput{
				Ctx: context.Background(), Model: tt.model, GroupID: &groupID, Group: group,
				Tokens: tokens, RateMultiplier: 1, Resolver: resolver, Resolved: resolved,
			})
			require.NoError(t, err)
			require.Equal(t, tt.wantApplied, cost.LongContextBillingApplied)
			require.InDelta(t, tt.wantInput, cost.InputCost, 1e-12)
			require.InDelta(t, tt.wantOutput, cost.OutputCost, 1e-12)
		})
	}
}

func TestPricingOverride_DeletedFileRevealsCatalogLadder(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "model_pricing.json")
	writeOverride := func(body string) string {
		path := filepath.Join(dir, "overrides.json")
		require.NoError(t, os.WriteFile(path, []byte(body), 0644))
		return path
	}
	require.NoError(t, os.WriteFile(catalogPath, []byte(gpt55OverrideCatalogJSON), 0644))
	overridePath := writeOverride(`{"gpt-5.5": {"long_context_input_token_threshold": 0}}`)

	svc := &PricingService{cfg: &config.Config{}}
	svc.cfg.Pricing.DataDir = dir
	svc.cfg.Pricing.OverrideFile = overridePath
	require.NoError(t, svc.loadPricingData(catalogPath))
	billing := NewBillingService(&config.Config{}, svc)

	pricing, err := billing.GetModelPricing("gpt-5.5")
	require.NoError(t, err)
	require.Zero(t, pricing.LongContextInputThreshold)
	require.True(t, pricing.LongContextInputThresholdPresent)

	require.NoError(t, os.Remove(overridePath))
	svc.reloadIfCustomFilesChanged()

	pricing, err = billing.GetModelPricing("gpt-5.5")
	require.NoError(t, err)
	require.Equal(t, 272000, pricing.LongContextInputThreshold)
	require.False(t, pricing.LongContextInputThresholdPresent)
	cost, err := billing.CalculateCost("gpt-5.5", UsageTokens{InputTokens: 300000}, 1)
	require.NoError(t, err)
	require.True(t, cost.LongContextBillingApplied)
}

func TestPricingOverride_FieldLevelMergeKeepsOtherFields(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"gpt-5.4": {"input_cost_per_token": 3e-06}}`)
	data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
	require.NoError(t, err)

	patched := data["gpt-5.4"]
	require.InDelta(t, 3e-6, patched.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.5e-5, patched.OutputCostPerToken, 1e-12, "未覆盖字段保持目录值")
	require.Equal(t, "openai", patched.LiteLLMProvider)
	require.Equal(t, 272000, patched.LongContextInputTokenThreshold, "above 折算仍生效")
	// 折算发生在合并之后：above 价不变、基础价被补丁改小，倍率随之变化。
	require.InDelta(t, 5.0/3.0, patched.LongContextInputCostMultiplier, 1e-9)
}

func TestPricingOverride_NullFieldValueRemovesField(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"gpt-5.5": {
		"input_cost_per_token_above_272k_tokens": null,
		"output_cost_per_token_above_272k_tokens": null,
		"cache_read_input_token_cost_above_272k_tokens": null}}`)
	data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
	require.NoError(t, err)
	require.Zero(t, data["gpt-5.5"].LongContextInputTokenThreshold, "above 字段删除后不再折算阶梯")
	require.InDelta(t, 5e-6, data["gpt-5.5"].InputCostPerToken, 1e-12)
}

// 完整加载管线：纯补丁不得抢在回退合并前建条目（否则回退完整条目被跳过、
// 其余分项价变 0 少收）；目录/回退都没有的模型作为独立条目并入。
func TestPricingOverride_LoadPipelineAddsNewModelAndPatchesFallbackOnly(t *testing.T) {
	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	require.NoError(t, os.WriteFile(catalogPath, []byte(`{
		"remote-model": {"litellm_provider": "test", "mode": "chat",
			"input_cost_per_token": 1e-06, "output_cost_per_token": 2e-06}
	}`), 0644))
	fallbackPath := filepath.Join(dir, "fallback.json")
	require.NoError(t, os.WriteFile(fallbackPath, []byte(`{
		"fallback-only-model": {"litellm_provider": "test", "mode": "chat",
			"input_cost_per_token": 4e-06, "output_cost_per_token": 8e-06,
			"cache_read_input_token_cost": 4e-07}
	}`), 0644))
	overridePath := filepath.Join(dir, "overrides.json")
	require.NoError(t, os.WriteFile(overridePath, []byte(`{
		"fallback-only-model": {"input_cost_per_token": 9e-06},
		"override-new-model": {"litellm_provider": "test", "mode": "chat",
			"input_cost_per_token": 5e-06, "output_cost_per_token": 1e-05}
	}`), 0644))

	svc := &PricingService{cfg: &config.Config{}}
	svc.cfg.Pricing.FallbackFile = fallbackPath
	svc.cfg.Pricing.OverrideFile = overridePath
	require.NoError(t, svc.loadPricingData(catalogPath))

	patched := svc.pricingData["fallback-only-model"]
	require.NotNil(t, patched)
	require.InDelta(t, 9e-6, patched.InputCostPerToken, 1e-12)
	require.InDelta(t, 8e-6, patched.OutputCostPerToken, 1e-12, "回退条目的其余字段必须保留")
	require.InDelta(t, 4e-7, patched.CacheReadInputTokenCost, 1e-12)

	added := svc.pricingData["override-new-model"]
	require.NotNil(t, added)
	require.InDelta(t, 5e-6, added.InputCostPerToken, 1e-12)
	require.InDelta(t, 1e-5, added.OutputCostPerToken, 1e-12)

	require.InDelta(t, 1e-6, svc.pricingData["remote-model"].InputCostPerToken, 1e-12)
}

// 拼错模型名（或纯补丁落在不存在的模型上）会被有效性过滤丢弃，必须有哨兵 WARN。
func TestPricingOverride_IneffectiveEntryWarns(t *testing.T) {
	logSink, restore := captureStructuredLog(t)
	defer restore()

	dir := t.TempDir()
	catalogPath := filepath.Join(dir, "catalog.json")
	require.NoError(t, os.WriteFile(catalogPath, []byte(`{
		"remote-model": {"litellm_provider": "test", "mode": "chat", "input_cost_per_token": 1e-06}
	}`), 0644))
	overridePath := filepath.Join(dir, "overrides.json")
	require.NoError(t, os.WriteFile(overridePath, []byte(`{
		"typo-model": {"long_context_input_token_threshold": 0}
	}`), 0644))

	svc := &PricingService{cfg: &config.Config{}}
	svc.cfg.Pricing.OverrideFile = overridePath
	require.NoError(t, svc.loadPricingData(catalogPath))

	require.NotContains(t, svc.pricingData, "typo-model")
	require.True(t, logSink.ContainsMessageAtLevel("override had no effect for 1 model(s): typo-model", "warn"))
}

func TestPricingOverride_NonObjectEntryKeepsCatalogEntry(t *testing.T) {
	svc := newPricingServiceWithOverride(t, `{"gpt-5.5": "oops"}`)
	data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
	require.NoError(t, err)
	require.Equal(t, 272000, data["gpt-5.5"].LongContextInputTokenThreshold, "非法补丁忽略，目录条目原样保留")
	require.InDelta(t, 5e-6, data["gpt-5.5"].InputCostPerToken, 1e-12)
}

func TestPricingOverride_MissingOrInvalidFileIsIgnored(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		svc := &PricingService{cfg: &config.Config{}}
		svc.cfg.Pricing.OverrideFile = filepath.Join(t.TempDir(), "absent.json")
		data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
		require.NoError(t, err)
		require.Equal(t, 272000, data["gpt-5.5"].LongContextInputTokenThreshold)
	})

	t.Run("invalid json", func(t *testing.T) {
		svc := newPricingServiceWithOverride(t, `{invalid`)
		data, err := svc.parsePricingData([]byte(gpt55OverrideCatalogJSON))
		require.NoError(t, err)
		require.Equal(t, 272000, data["gpt-5.5"].LongContextInputTokenThreshold)
	})
}

// 对真实出厂目录快照关闭 gpt-5.5 阶梯：计费视角阈值归零、基础价不变，
// 其他模型（gpt-5.4）的目录阶梯不受影响。
func TestPricingOverride_DisablesGPT55LadderOnDefaultCatalog(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	svc := newPricingServiceWithOverride(t, `{
		"gpt-5.5": {"long_context_input_token_threshold": 0},
		"gpt-5.5-2026-04-23": {"long_context_input_token_threshold": 0}
	}`)
	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	svc.pricingData = data
	billing := NewBillingService(&config.Config{}, svc)

	for _, model := range []string{"gpt-5.5", "gpt-5.5-2026-04-23"} {
		pricing, err := billing.GetModelPricing(model)
		require.NoError(t, err)
		require.Zero(t, pricing.LongContextInputThreshold, model)
		require.InDelta(t, 5e-6, pricing.InputPricePerToken, 1e-12, model)
	}

	pricing, err := billing.GetModelPricing("gpt-5.4")
	require.NoError(t, err)
	require.Equal(t, 272000, pricing.LongContextInputThreshold, "其他模型的目录阶梯不受影响")
}
