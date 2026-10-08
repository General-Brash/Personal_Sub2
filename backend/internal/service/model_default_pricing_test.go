//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type defaultPricingRepoStub struct {
	SettingRepository
	mu      sync.Mutex
	values  map[string]string
	fail    bool
	writes  int
	failCAS bool
}

func (r *defaultPricingRepoStub) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return nil, errors.New("database offline")
	}
	out := map[string]string{}
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			out[key] = value
		}
	}
	return out, nil
}
func (r *defaultPricingRepoStub) CompareAndSetMultiple(ctx context.Context, expected, updates map[string]string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail || r.failCAS {
		return false, errors.New("database offline")
	}
	for key, value := range expected {
		if r.values[key] != value {
			return false, nil
		}
	}
	for key, value := range updates {
		r.values[key] = value
	}
	r.writes++
	return true, nil
}
func newDefaultPricingFixture(t *testing.T) (*BillingService, *DefaultModelPricingService, *defaultPricingRepoStub) {
	t.Helper()
	repo := &defaultPricingRepoStub{values: map[string]string{}}
	defaults := NewDefaultModelPricingService(repo)
	require.NoError(t, defaults.Refresh(context.Background()))
	billing := NewBillingService(&config.Config{}, nil)
	billing.defaultPricing = defaults
	require.NoError(t, defaults.setBaselineValidator(billing.GetSystemDefaultPricing))
	return billing, defaults, repo
}
func defaultPatch(t *testing.T, raw string) DefaultPricingPatch {
	t.Helper()
	var patch DefaultPricingPatch
	require.NoError(t, json.Unmarshal([]byte(raw), &patch))
	return patch
}
func saveDefault(t *testing.T, billing *BillingService, model, patch string) *ModelDefaultPricingDetail {
	t.Helper()
	detail, _, err := billing.SaveDefaultPricing(context.Background(), model, billing.defaultPricing.LoadedVersion(), defaultPatch(t, patch), false)
	require.NoError(t, err)
	return detail
}

func TestDefaultModelPricingUnknownTokenAndRealUsageBillingCommand(t *testing.T) {
	ctx := context.Background()
	billing, defaults, _ := newDefaultPricingFixture(t)
	require.Error(t, billing.PreflightTokenPricing(ctx, "vendor/custom-v1", nil, nil))
	detail := saveDefault(t, billing, " Vendor/Custom-v1 ", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008}`)
	require.Equal(t, "vendor/custom-v1", detail.PricingKey)
	require.False(t, detail.HasExactSystemStandard)
	require.True(t, detail.EffectivePricingAvailable)
	resolver := NewModelPricingResolver(nil, billing)
	require.NoError(t, billing.PreflightTokenPricing(ctx, "VENDOR/CUSTOM-V1", nil, resolver))
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	gateway.billingService = billing
	gateway.resolver = resolver
	gateway.userGroupRateResolver = nil
	groupID := int64(13)
	err := gateway.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "default-pricing-token", Model: "vendor/custom-v1", Usage: OpenAIUsage{InputTokens: 1000, OutputTokens: 500}, Duration: time.Second},
		APIKey: &APIKey{ID: 1, GroupID: &groupID, Group: &Group{ID: groupID, RateMultiplier: 1.5}}, User: &User{ID: 2}, Account: &Account{ID: 3},
	})
	require.NoError(t, err)
	require.Equal(t, 1, billingRepo.calls)
	usage := requireUnifiedUsageLog(t, billingRepo, usageRepo)
	require.InDelta(t, 0.002, usage.InputCost, 1e-12)
	require.InDelta(t, 0.004, usage.OutputCost, 1e-12)
	require.InDelta(t, 0.006, usage.TotalCost, 1e-12)
	require.InDelta(t, 0.009, usage.ActualCost, 1e-12)
	require.InDelta(t, 0.009, billingRepo.lastCmd.BalanceCost, 1e-12)
	quote := NewPriceQuoteService(resolver, nil).Quote(ctx, PriceQuoteInput{Model: "vendor/custom-v1"})
	require.Equal(t, PricingSourceAdmin, quote.Source)
	require.Equal(t, defaults.LoadedVersion(), quote.SourceVersions["admin_default_pricing"])
	require.Equal(t, 2.0, *quote.InputPerMillion)
	require.Equal(t, 8.0, *quote.OutputPerMillion)
}

func TestDefaultModelPricingSparseFallbackZeroAndReset(t *testing.T) {
	billing, defaults, _ := newDefaultPricingFixture(t)
	ctx := context.Background()
	original, err := billing.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)
	detail := saveDefault(t, billing, "claude-sonnet-4", `{"input_price":0.000007}`)
	got, err := billing.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)
	require.Equal(t, 7e-6, got.InputPricePerToken)
	require.Equal(t, original.OutputPricePerToken, got.OutputPricePerToken)
	require.Equal(t, original.CacheReadPricePerToken, got.CacheReadPricePerToken)
	require.Equal(t, PricingSourceFallback, detail.SystemBaseline.Source)
	saveDefault(t, billing, "claude-sonnet-4", `{"cache_read_price":0}`)
	got, err = billing.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)
	require.Equal(t, 7e-6, got.InputPricePerToken)
	saveDefault(t, billing, "claude-sonnet-4", `{"input_price":null}`)
	got, err = billing.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)
	require.Equal(t, original.InputPricePerToken, got.InputPricePerToken)
	reset := DefaultPricingPatch{}
	for _, field := range DefaultPricingEditableFields() {
		reset[field] = json.RawMessage("null")
	}
	_, _, err = billing.SaveDefaultPricing(ctx, "claude-sonnet-4", defaults.LoadedVersion(), reset, true)
	require.NoError(t, err)
	got, err = billing.GetModelPricing("claude-sonnet-4")
	require.NoError(t, err)
	require.Equal(t, original.CacheReadPricePerToken, got.CacheReadPricePerToken)
	saveDefault(t, billing, "custom-free-v1", `{"billing_mode":"token","input_price":0,"output_price":0}`)
	require.NoError(t, billing.PreflightTokenPricing(ctx, "custom-free-v1", nil, NewModelPricingResolver(nil, billing)))
	_, _, err = billing.SaveDefaultPricing(ctx, "custom-free-v1", defaults.LoadedVersion(), reset, true)
	require.ErrorIs(t, err, ErrDefaultPricingResetUnavailable)
	quote := NewPriceQuoteService(NewModelPricingResolver(nil, billing), nil).Quote(ctx, PriceQuoteInput{Model: "custom-free-v1"})
	require.NotNil(t, quote.InputPerMillion)
	require.Zero(t, *quote.InputPerMillion)
	require.NotContains(t, quote.UnknownFields, "input_per_million")
}

func TestDefaultModelPricingExplicitZeroSurvivesPoliciesAndConsumption(t *testing.T) {
	billing, _, _ := newDefaultPricingFixture(t)
	billing.pricingService = NewPricingService(&config.Config{}, nil)
	saveDefault(t, billing, "gpt-5.6-sol", `{"cache_write_price":0,"cache_write_1h_price":0,"image_input_price":0,"image_output_price":0,"cache_read_price":0}`)
	resolver := NewModelPricingResolver(nil, billing)
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "gpt-5.6-sol", Resolver: resolver, RateMultiplier: 1, Tokens: UsageTokens{InputTokens: 100, ImageInputTokens: 100, OutputTokens: 100, ImageOutputTokens: 100, CacheCreationTokens: 100, CacheCreation5mTokens: 50, CacheCreation1hTokens: 50, CacheReadTokens: 100}})
	require.NoError(t, err)
	require.Zero(t, cost.TotalCost)
	p, err := billing.GetModelPricing("gpt-5.6-sol")
	require.NoError(t, err)
	require.Equal(t, openAIGPT54LongContextInputThreshold, p.LongContextInputThreshold)
	require.Greater(t, p.InputPricePerTokenPriority, 0.0)
}

func TestDefaultModelPricingValidationAndVersionProtection(t *testing.T) {
	billing, defaults, repo := newDefaultPricingFixture(t)
	ctx := context.Background()
	for _, patch := range []string{
		`{"billing_mode":"token","input_price":0}`, `{"billing_mode":"token","input_price":-1,"output_price":1}`,
		`{"billing_mode":"token","input_price":1000001,"output_price":1}`, `{"billing_mode":"token","input_price":"NaN","output_price":1}`,
		`{"billing_mode":"token","input_price":1e309,"output_price":1}`, `{"billing_mode":"token","input_price":0,"output_price":0,"unknown":1}`,
		`{"billing_mode":"image","input_price":1,"image_price_1k":1,"image_price_2k":1,"image_price_4k":1}`,
	} {
		_, _, err := billing.SaveDefaultPricing(ctx, "new-model", defaults.LoadedVersion(), defaultPatch(t, patch), false)
		require.Error(t, err, patch)
		require.Equal(t, "0", defaults.LoadedVersion())
		require.Zero(t, repo.writes)
	}
	for _, model := range []string{" ", "a b", "a\nb", "wild*", "wild?"} {
		_, err := NormalizeDefaultPricingModel(model)
		require.Error(t, err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, model := range []string{"new-model-a", "new-model-b"} {
		wg.Add(1)
		go func(model string) {
			defer wg.Done()
			_, _, err := billing.SaveDefaultPricing(ctx, model, "0", DefaultPricingPatch{"billing_mode": json.RawMessage(`"token"`), "input_price": json.RawMessage("0"), "output_price": json.RawMessage("0")}, false)
			errs <- err
		}(model)
	}
	wg.Wait()
	close(errs)
	saved, conflict := 0, 0
	for err := range errs {
		if err == nil {
			saved++
		} else {
			require.ErrorIs(t, err, ErrDefaultPricingVersionConflict)
			conflict++
		}
	}
	require.Equal(t, 1, saved)
	require.Equal(t, 1, conflict)
	require.Equal(t, 1, repo.writes)
	saveDefault(t, billing, "another-model", `{"billing_mode":"per_request","per_request_price":0.5}`)
	list, err := defaults.List(ctx, "", 1, 20)
	require.NoError(t, err)
	require.Equal(t, 2, list.Total)
	repo.mu.Lock()
	repo.fail = true
	repo.mu.Unlock()
	require.Error(t, defaults.Refresh(ctx))
	require.Equal(t, "2", defaults.LoadedVersion())
	p, _, _, ok := defaults.Lookup("another-model")
	require.True(t, ok)
	require.Equal(t, 0.5, *p.PerRequestPrice)
}

func TestDefaultModelPricingExactAliasesAndReload(t *testing.T) {
	billing, defaults, repo := newDefaultPricingFixture(t)
	saveDefault(t, billing, "deepseek-v4-flash", `{"input_price":0.000011}`)
	saveDefault(t, billing, "deepseek-v4.1-flash", `{"input_price":0.000022}`)
	p, err := billing.GetModelPricing("DeepSeek-V4.1-Flash")
	require.NoError(t, err)
	require.Equal(t, 22e-6, p.InputPricePerToken)
	p, err = billing.GetModelPricing("deepseek-v4-flash")
	require.NoError(t, err)
	require.Equal(t, 11e-6, p.InputPricePerToken)
	other := NewDefaultModelPricingService(repo)
	require.NoError(t, other.Refresh(context.Background()))
	require.Equal(t, defaults.LoadedVersion(), other.LoadedVersion())
	defaults.publish(&defaultPricingSnapshot{SchemaVersion: 1, Revision: 1, Models: map[string]DefaultPricingFields{}})
	require.Equal(t, "2", defaults.LoadedVersion())
	fields, _, _, _ := defaults.Lookup("deepseek-v4.1-flash")
	*fields.InputPrice = 99
	p, err = billing.GetModelPricing("deepseek-v4.1-flash")
	require.NoError(t, err)
	require.Equal(t, 22e-6, p.InputPricePerToken)
	catalog := NewPricingService(&config.Config{}, nil)
	catalog.pricingData = map[string]*LiteLLMModelPricing{"catalog-x": {InputCostPerToken: 1e-6, OutputCostPerToken: 2e-6}}
	billing.pricingService = catalog
	saveDefault(t, billing, "catalog-x", `{"input_price":0.000003}`)
	catalog.mu.Lock()
	catalog.pricingData = map[string]*LiteLLMModelPricing{"catalog-x": {InputCostPerToken: 5e-6, OutputCostPerToken: 7e-6}}
	catalog.mu.Unlock()
	p, err = billing.GetModelPricing("catalog-x")
	require.NoError(t, err)
	require.Equal(t, 3e-6, p.InputPricePerToken)
	require.Equal(t, 7e-6, p.OutputPricePerToken)
}

func TestDefaultModelPricingMediaUnitsAndDedicatedGroupOverrides(t *testing.T) {
	billing, _, _ := newDefaultPricingFixture(t)
	ctx := context.Background()
	resolver := NewModelPricingResolver(nil, billing)
	saveDefault(t, billing, "request-model", `{"billing_mode":"per_request","per_request_price":0.25}`)
	require.NoError(t, billing.PreflightTokenPricing(ctx, "request-model", nil, resolver))
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "request-model", Resolver: resolver, RateMultiplier: 1.5, RequestCount: 3})
	require.NoError(t, err)
	require.InDelta(t, 1.125, cost.ActualCost, 1e-12)
	saveDefault(t, billing, "grok-imagine-image", `{"billing_mode":"image","image_price_1k":0,"image_price_2k":0.4,"image_price_4k":0.8}`)
	require.NoError(t, billing.PreflightImagePricing(ctx, "grok-imagine-image", "1K", nil, nil, resolver))
	cost, err = billing.CalculateImageCostChecked("grok-imagine-image", "1K", 3, nil, 1.5)
	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)
	cost, err = billing.CalculateImageCostChecked("grok-imagine-image", "4K", 2, nil, 1.5)
	require.NoError(t, err)
	require.InDelta(t, 2.4, cost.ActualCost, 1e-12)
	saveDefault(t, billing, "grok-imagine-video-1.5", `{"billing_mode":"video","video_price_480p":0.01,"video_price_720p":0.02,"video_price_1080p":0.03}`)
	require.NoError(t, billing.PreflightVideoPricing(ctx, "grok-imagine-video-1.5", "1080p", nil, nil, resolver))
	cost, err = billing.CalculateVideoCostChecked("grok-imagine-video-1.5", "1080p", 2, 6, nil, 1.5)
	require.NoError(t, err)
	require.InDelta(t, 0.54, cost.ActualCost, 1e-12)
	cost, err = billing.CalculateCostUnified(CostInput{Ctx: ctx, Model: "grok-imagine-video-1.5", Resolver: resolver, RateMultiplier: 1.5, RequestCount: 2, DurationSeconds: 6, SizeTier: "1080p"})
	require.NoError(t, err)
	require.InDelta(t, 0.54, cost.ActualCost, 1e-12)
	groupPrice := 0.2
	cost, err = billing.CalculateVideoCostChecked("grok-imagine-video-1.5", "1080p", 2, 6, &VideoPriceConfig{Price1080P: &groupPrice}, 1.5)
	require.NoError(t, err)
	require.InDelta(t, 3.6, cost.ActualCost, 1e-12)
	quote := NewPriceQuoteService(resolver, nil).Quote(ctx, PriceQuoteInput{Model: "grok-imagine-video-1.5"})
	require.Equal(t, "per_second", quote.Unit)
	require.Empty(t, quote.UnknownFields)
	quote = NewPriceQuoteService(resolver, nil).Quote(ctx, PriceQuoteInput{Model: "grok-imagine-image"})
	require.Equal(t, "per_image", quote.Unit)
	require.NotNil(t, quote.PerRequestPrice)
	require.Zero(t, *quote.PerRequestPrice)
}

func TestDefaultModelPricingCrossInstancePollingAndStartupFailure(t *testing.T) {
	billing, _, repo := newDefaultPricingFixture(t)
	replica := NewDefaultModelPricingService(repo)
	require.NoError(t, replica.Initialize(context.Background()))
	defer replica.Stop()
	saveDefault(t, billing, "replicated-v1", `{"billing_mode":"token","input_price":0,"output_price":0}`)
	require.Eventually(t, func() bool { return replica.LoadedVersion() == "1" }, 5*time.Second, 25*time.Millisecond)
	repo.mu.Lock()
	repo.fail = true
	repo.mu.Unlock()
	broken := NewDefaultModelPricingService(repo)
	require.Error(t, broken.Initialize(context.Background()))
	require.Nil(t, broken.snapshot.Load())
}

func TestDefaultModelPricingMediaReachesUsageAndBalanceCommands(t *testing.T) {
	for _, tc := range []struct {
		name, model, patch string
		result             OpenAIForwardResult
		total, actual      float64
	}{
		{"request", "custom-request", `{"billing_mode":"per_request","per_request_price":0.25}`, OpenAIForwardResult{}, 0.25, 0.375},
		{"image", "grok-imagine-image", `{"billing_mode":"image","image_price_1k":0.1,"image_price_2k":0.2,"image_price_4k":0.3}`, OpenAIForwardResult{ImageCount: 2, ImageSize: "2K"}, 0.4, 0.6},
		{"video", "grok-imagine-video-1.5", `{"billing_mode":"video","video_price_480p":0.01,"video_price_720p":0.02,"video_price_1080p":0.03}`, OpenAIForwardResult{VideoCount: 2, VideoResolution: "1080p", VideoDurationSeconds: 6}, 0.36, 0.54},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing, _, _ := newDefaultPricingFixture(t)
			saveDefault(t, billing, tc.model, tc.patch)
			usageRepo := &openAIRecordUsageLogRepoStub{}
			billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
			gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			gateway.billingService = billing
			gateway.resolver = NewModelPricingResolver(nil, billing)
			gateway.userGroupRateResolver = nil
			groupID := int64(13)
			result := tc.result
			result.Model = tc.model
			result.RequestID = "pricing-media-" + tc.name
			result.Duration = time.Second
			err := gateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &result, APIKey: &APIKey{ID: 1, GroupID: &groupID, Group: &Group{ID: groupID, RateMultiplier: 1.5}}, User: &User{ID: 2}, Account: &Account{ID: 3}})
			require.NoError(t, err)
			require.Equal(t, 1, billingRepo.calls)
			usage := requireUnifiedUsageLog(t, billingRepo, usageRepo)
			require.InDelta(t, tc.total, usage.TotalCost, 1e-12)
			require.InDelta(t, tc.actual, usage.ActualCost, 1e-12)
			require.InDelta(t, tc.actual, billingRepo.lastCmd.BalanceCost, 1e-12)
		})
	}
}

func TestDefaultModelPricingPartialGroupOverridesSkipChannelAndKeepManualFallback(t *testing.T) {
	billing, _, _ := newDefaultPricingFixture(t)
	saveDefault(t, billing, "claude-sonnet-4", `{"input_price":0.000002,"output_price":0.000008,"image_input_price":0}`)
	resolver := newResolverWithChannel(t, []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(10e-6), OutputPrice: testPtrFloat64(20e-6)}})
	resolver.billingService = billing
	group := &Group{ID: 100, ModelPricing: []ChannelModelPricing{{Models: []string{"claude-sonnet-4"}, BillingMode: BillingModeToken, InputPrice: testPtrFloat64(0)}}}
	resolved := resolver.Resolve(context.Background(), PricingInput{Model: "claude-sonnet-4", GroupID: groupIDPtr(), Group: group})
	require.Equal(t, PricingSourceGroup, resolved.Source)
	require.Zero(t, resolved.BasePricing.InputPricePerToken)
	require.Equal(t, 8e-6, resolved.BasePricing.OutputPricePerToken)
	require.NoError(t, billing.PreflightTokenPricing(context.Background(), "claude-sonnet-4", groupIDPtr(), resolver, group))
	cost, err := billing.CalculateCostUnified(CostInput{Ctx: context.Background(), Model: "claude-sonnet-4", GroupID: groupIDPtr(), Group: group, Resolver: resolver, RateMultiplier: 1.5, Tokens: UsageTokens{InputTokens: 100, OutputTokens: 500, ImageInputTokens: 100, ImageOutputTokens: 500}})
	require.NoError(t, err)
	require.InDelta(t, 0.006, cost.ActualCost, 1e-12)
}

func TestDefaultModelPricingQuoteKeepsDedicatedMediaGroupTier(t *testing.T) {
	billing, _, _ := newDefaultPricingFixture(t)
	saveDefault(t, billing, "grok-imagine-image", `{"billing_mode":"image","image_price_1k":0.1,"image_price_2k":0.2,"image_price_4k":0.3}`)
	quote := NewPriceQuoteService(NewModelPricingResolver(nil, billing), nil).Quote(context.Background(), PriceQuoteInput{Model: "grok-imagine-image", Group: &Group{ImagePrice1K: testPtrFloat64(0)}})
	require.Equal(t, PricingSourceGroup, quote.Source)
	require.Equal(t, "per_image", quote.Unit)
	require.Zero(t, *quote.PerRequestPrice)
	require.Equal(t, 0.2, *quote.Intervals[1].PerRequestPrice)
}

func TestDefaultModelPricingInvalidRefreshAndRevisionRegressionKeepSnapshot(t *testing.T) {
	billing, defaults, repo := newDefaultPricingFixture(t)
	saveDefault(t, billing, "stable-model", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008}`)
	repo.mu.Lock()
	valid := repo.values[SettingKeyModelDefaultPricing]
	repo.values[SettingKeyModelDefaultPricing] = `{"schema_version":1,"revision":2,"models":{"incomplete":{"billing_mode":"token","input_price":0}}}`
	repo.mu.Unlock()
	require.Error(t, defaults.Refresh(context.Background()))
	require.Equal(t, "1", defaults.LoadedVersion())
	price, err := billing.GetModelPricing("stable-model")
	require.NoError(t, err)
	require.Equal(t, 2e-6, price.InputPricePerToken)
	repo.mu.Lock()
	repo.values[SettingKeyModelDefaultPricing] = valid
	repo.mu.Unlock()
	require.NoError(t, defaults.Refresh(context.Background()))
	repo.mu.Lock()
	repo.values[SettingKeyModelDefaultPricing] = ""
	repo.mu.Unlock()
	require.Error(t, defaults.Refresh(context.Background()))
	_, _, err = billing.SaveDefaultPricing(context.Background(), "new-model", "0", defaultPatch(t, `{"billing_mode":"token","input_price":0,"output_price":0}`), false)
	require.Error(t, err)
	require.Equal(t, "1", defaults.LoadedVersion())
}

func TestDefaultModelPricingCASFailureDoesNotPublish(t *testing.T) {
	billing, defaults, repo := newDefaultPricingFixture(t)
	saveDefault(t, billing, "no-partial-commit", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008}`)
	repo.mu.Lock()
	repo.failCAS = true
	before := repo.values[SettingKeyModelDefaultPricing]
	repo.mu.Unlock()
	_, _, err := billing.SaveDefaultPricing(context.Background(), "no-partial-commit", "1", defaultPatch(t, `{"input_price":0.000099}`), false)
	require.Error(t, err)
	require.Equal(t, "1", defaults.LoadedVersion())
	pricing, err := billing.GetModelPricing("no-partial-commit")
	require.NoError(t, err)
	require.Equal(t, 2e-6, pricing.InputPricePerToken)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, before, repo.values[SettingKeyModelDefaultPricing])
}

func TestDefaultModelPricingTokenImageDispatchAndRequestMediaUnits(t *testing.T) {
	for _, tc := range []struct {
		name, model, patch string
		result             OpenAIForwardResult
		total              float64
	}{
		{"image tokens", "gpt-image-2.5-flare", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008,"image_input_price":0,"image_output_price":0.00001}`, OpenAIForwardResult{ImageCount: 2, ImageSize: "1K", Usage: OpenAIUsage{InputTokens: 1000, ImageInputTokens: 100, OutputTokens: 500, ImageOutputTokens: 500}}, 0.0068},
		{"image request", "grok-imagine-image", `{"billing_mode":"per_request","per_request_price":0.25}`, OpenAIForwardResult{ImageCount: 2, ImageSize: "1K"}, 0.25},
		{"video request", "grok-imagine-video-1.5", `{"billing_mode":"per_request","per_request_price":0.25}`, OpenAIForwardResult{VideoCount: 3, VideoDurationSeconds: 6, VideoResolution: "480p"}, 0.25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			billing, _, _ := newDefaultPricingFixture(t)
			saveDefault(t, billing, tc.model, tc.patch)
			resolver := NewModelPricingResolver(nil, billing)
			if tc.result.ImageCount > 0 {
				require.NoError(t, billing.PreflightImagePricing(context.Background(), tc.model, "1K", nil, nil, resolver))
			} else {
				require.NoError(t, billing.PreflightVideoPricing(context.Background(), tc.model, "480p", nil, nil, resolver))
			}
			usageRepo := &openAIRecordUsageLogRepoStub{}
			billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
			gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
			gateway.billingService = billing
			gateway.resolver = resolver
			gateway.userGroupRateResolver = nil
			result := tc.result
			result.Model = tc.model
			result.RequestID = tc.name
			result.Duration = time.Second
			gid := int64(13)
			err := gateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &result, APIKey: &APIKey{ID: 1, GroupID: &gid, Group: &Group{ID: gid, RateMultiplier: 1.5}}, User: &User{ID: 2}, Account: &Account{ID: 3}})
			require.NoError(t, err)
			usage := requireUnifiedUsageLog(t, billingRepo, usageRepo)
			require.InDelta(t, tc.total, usage.TotalCost, 1e-12)
			require.InDelta(t, tc.total*1.5, billingRepo.lastCmd.BalanceCost, 1e-12)
			if tc.result.ImageCount > 0 {
				require.Equal(t, 2, usage.ImageCount)
			}
		})
	}
}

func TestDefaultModelPricingGenericImageTokenAndLegacyPerRequest(t *testing.T) {
	billing, _, _ := newDefaultPricingFixture(t)
	saveDefault(t, billing, "custom-image-token", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008}`)
	gateway := &GatewayService{billingService: billing, resolver: NewModelPricingResolver(nil, billing)}
	gid := int64(13)
	result := &ForwardResult{Model: "custom-image-token", ImageCount: 2, ImageSize: "1K"}
	result.Usage.InputTokens = 1000
	result.Usage.OutputTokens = 500
	cost, err := gateway.calculateRecordUsageCost(context.Background(), result, &APIKey{GroupID: &gid, Group: &Group{ID: gid}}, result.Model, 1.5, 1.5, time.Time{}, &recordUsageOpts{})
	require.NoError(t, err)
	require.InDelta(t, 0.009, cost.ActualCost, 1e-12)
	saveDefault(t, billing, "custom-request", `{"billing_mode":"per_request","per_request_price":0.25}`)
	cost, err = billing.CalculateCostWithLongContext("custom-request", UsageTokens{InputTokens: 400000}, 1.5, 200000, 2)
	require.NoError(t, err)
	require.InDelta(t, 0.375, cost.ActualCost, 1e-12)
}

func TestDefaultModelPricingMixedUnitGroupOverridesStayConditional(t *testing.T) {
	billing, _, _ := newDefaultPricingFixture(t)
	saveDefault(t, billing, "gpt-image-2.5-flare", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008}`)
	group := &Group{ImagePrice1K: testPtrFloat64(0.04), VideoPrice480P: testPtrFloat64(0.03)}
	quote := NewPriceQuoteService(NewModelPricingResolver(nil, billing), nil).Quote(context.Background(), PriceQuoteInput{Model: "gpt-image-2.5-flare", Group: group})
	require.Equal(t, "per_1m_tokens", quote.Unit)
	require.Len(t, quote.PriceConditions, 2)
	require.Equal(t, "per_image", quote.PriceConditions[0].Unit)
	require.Equal(t, 0.04, *quote.PriceConditions[0].PerRequestPrice)
	require.Equal(t, "per_second", quote.PriceConditions[1].Unit)
	require.Equal(t, 0.03, *quote.PriceConditions[1].PerRequestPrice)
}

func TestDefaultModelPricingMissingNewModelCacheIsNotAccidentallyFree(t *testing.T) {
	billing, defaults, _ := newDefaultPricingFixture(t)
	detail := saveDefault(t, billing, "new-cache-model", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008}`)
	require.True(t, detail.CacheFallbackToInput)
	require.Equal(t, 2e-6, *detail.EffectivePricing.CacheReadPrice)
	require.Equal(t, 2e-6, *detail.EffectivePricing.CacheWrite1hPrice)
	fields, _, _, ok := defaults.Lookup("new-cache-model")
	require.True(t, ok)
	require.Nil(t, fields.CacheReadPrice)
	require.Nil(t, fields.CacheWritePrice)
	usageRepo := &openAIRecordUsageLogRepoStub{}
	billingRepo := &openAIRecordUsageBillingRepoStub{result: &UsageBillingApplyResult{Applied: true}}
	gateway := newOpenAIRecordUsageServiceWithBillingRepoForTest(usageRepo, billingRepo, &openAIRecordUsageUserRepoStub{}, &openAIRecordUsageSubRepoStub{}, nil)
	gateway.billingService = billing
	gateway.resolver = NewModelPricingResolver(nil, billing)
	gateway.userGroupRateResolver = nil
	gid := int64(13)
	err := gateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{Model: "new-cache-model", RequestID: "all-cache-input", Usage: OpenAIUsage{InputTokens: 1000, CacheReadInputTokens: 1000}}, APIKey: &APIKey{ID: 1, GroupID: &gid, Group: &Group{ID: gid, RateMultiplier: 1.5}}, User: &User{ID: 2}, Account: &Account{ID: 3}})
	require.NoError(t, err)
	usage := requireUnifiedUsageLog(t, billingRepo, usageRepo)
	require.Zero(t, usage.InputCost)
	require.InDelta(t, 0.002, usage.CacheReadCost, 1e-12)
	require.InDelta(t, 0.003, billingRepo.lastCmd.BalanceCost, 1e-12)
	saveDefault(t, billing, "new-cache-model", `{"cache_read_price":0,"cache_write_price":0}`)
	cost, err := billing.CalculateCost("new-cache-model", UsageTokens{CacheReadTokens: 1000, CacheCreationTokens: 1000, CacheCreation1hTokens: 1000}, 1.5)
	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)
}

func TestDefaultModelPricingImageTokenQuotesTrackZeroAndPriceChanges(t *testing.T) {
	billing, _, _ := newDefaultPricingFixture(t)
	saveDefault(t, billing, "image-rate-model", `{"billing_mode":"token","input_price":0.000002,"output_price":0.000008,"image_input_price":0,"image_output_price":0.00001}`)
	quotes := NewPriceQuoteService(NewModelPricingResolver(nil, billing), nil)
	first := quotes.Quote(context.Background(), PriceQuoteInput{Model: "image-rate-model"})
	require.NotNil(t, first.ImageInputPerMillion)
	require.Zero(t, *first.ImageInputPerMillion)
	require.Equal(t, 10.0, *first.ImageOutputPerMillion)
	saveDefault(t, billing, "image-rate-model", `{"image_output_price":0.00002}`)
	second := quotes.Quote(context.Background(), PriceQuoteInput{Model: "image-rate-model"})
	require.Equal(t, 20.0, *second.ImageOutputPerMillion)
	require.NotEqual(t, first.QuoteVersion, second.QuoteVersion)
}

func TestDefaultModelPricingVideoAliasesRespectExactOverridesAndReset(t *testing.T) {
	billing, defaults, _ := newDefaultPricingFixture(t)
	saveDefault(t, billing, "grok-imagine-video-1.5", `{"billing_mode":"video","video_price_480p":0.01,"video_price_720p":0.02,"video_price_1080p":0.03}`)
	cost, err := billing.CalculateVideoCostChecked("grok-video-1.5", "480p", 1, 10, nil, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.1, cost.TotalCost, 1e-12)
	detail, err := billing.DefaultPricingDetail(context.Background(), "grok-video-1.5")
	require.NoError(t, err)
	require.False(t, detail.HasAdminOverride)
	require.Equal(t, "grok-imagine-video-1.5", detail.PricingKey)
	require.Equal(t, 0.01, *detail.EffectivePricing.VideoPrice480P)
	saveDefault(t, billing, "grok-video-1.5", `{"billing_mode":"video","video_price_480p":0.04}`)
	cost, err = billing.CalculateVideoCostChecked("grok-video-1.5", "480p", 1, 10, nil, 1)
	require.NoError(t, err)
	require.InDelta(t, 0.4, cost.TotalCost, 1e-12)
	reset := DefaultPricingPatch{}
	for _, field := range DefaultPricingEditableFields() {
		reset[field] = json.RawMessage("null")
	}
	detail, _, err = billing.SaveDefaultPricing(context.Background(), "grok-video-1.5", defaults.LoadedVersion(), reset, true)
	require.NoError(t, err)
	require.Equal(t, 0.01, *detail.EffectivePricing.VideoPrice480P)
	require.Equal(t, "grok-imagine-video-2.0", adminDefaultPricingAliasKey("grok-imagine-video-2.0"))
	require.Equal(t, "unrelated-video-1.5-model", adminDefaultPricingAliasKey("unrelated-video-1.5-model"))
}
