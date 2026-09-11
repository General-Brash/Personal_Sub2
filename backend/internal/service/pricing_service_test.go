package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestPricingSchedulerBlankRemoteURLDoesNotStart(t *testing.T) {
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{RemoteURL: "  \t  "}}, nil)
	defer svc.Stop()

	svc.startUpdateScheduler()
	done := make(chan struct{})
	go func() {
		svc.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("blank remote URL must not start scheduler")
	}
}

func TestPricingNonEmptyInvalidRemoteURLStillReturnsValidationError(t *testing.T) {
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{
		RemoteURL: "://invalid",
		DataDir:   t.TempDir(),
	}}, nil)

	err := svc.ForceUpdate()

	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid pricing url")
}

func TestParsePricingData_ParsesPriorityAndServiceTierFields(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_creation_input_token_cost": 0.0000025,
			"cache_creation_input_token_cost_priority": 0.000005,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"long_context_input_token_threshold": 272000,
			"long_context_input_cost_multiplier": 2,
			"long_context_output_cost_multiplier": 1.5,
			"supports_service_tier": true,
			"supports_prompt_caching": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 3e-5, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 5e-6, pricing.CacheCreationInputTokenCostPriority, 1e-12)
	require.InDelta(t, 5e-7, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.Equal(t, 272000, pricing.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, pricing.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, pricing.LongContextOutputCostMultiplier, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestBillingService_GPT56CacheWritePricingUsesOfficialMultiplier(t *testing.T) {
	tests := []struct {
		model             string
		input             float64
		inputPriority     float64
		output            float64
		outputPriority    float64
		cacheRead         float64
		cacheReadPriority float64
	}{
		{model: "gpt-5.6-sol", input: 5e-6, inputPriority: 10e-6, output: 30e-6, outputPriority: 60e-6, cacheRead: 0.5e-6, cacheReadPriority: 1e-6},
		{model: "gpt-5.6-terra", input: 2e-6, inputPriority: 4e-6, output: 12e-6, outputPriority: 24e-6, cacheRead: 0.2e-6, cacheReadPriority: 0.4e-6},
		{model: "gpt-5.6-luna", input: 0.2e-6, inputPriority: 0.4e-6, output: 1.2e-6, outputPriority: 2.4e-6, cacheRead: 0.02e-6, cacheReadPriority: 0.04e-6},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				tt.model: {
					InputCostPerToken:               tt.input,
					InputCostPerTokenPriority:       tt.inputPriority,
					OutputCostPerToken:              tt.output,
					OutputCostPerTokenPriority:      tt.outputPriority,
					CacheReadInputTokenCost:         tt.cacheRead,
					CacheReadInputTokenCostPriority: tt.cacheReadPriority,
				},
			}}
			svc := NewBillingService(&config.Config{}, pricingSvc)

			pricing, err := svc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input*1.25, pricing.CacheCreationPricePerToken, 1e-12)
			require.InDelta(t, tt.inputPriority*1.25, pricing.CacheCreationPricePerTokenPriority, 1e-12)
			require.Equal(t, 272000, pricing.LongContextInputThreshold)
			require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
			require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)

			tokens := UsageTokens{InputTokens: 700, OutputTokens: 50, CacheCreationTokens: 200, CacheReadTokens: 100}
			standard, err := svc.CalculateCostWithServiceTier(tt.model, tokens, 1, "")
			require.NoError(t, err)
			require.InDelta(t, 200*tt.input*1.25, standard.CacheCreationCost, 1e-12)

			priority, err := svc.CalculateCostWithServiceTier(tt.model, tokens, 1, "priority")
			require.NoError(t, err)
			require.InDelta(t, 200*tt.inputPriority*1.25, priority.CacheCreationCost, 1e-12)

			flex, err := svc.CalculateCostWithServiceTier(tt.model, tokens, 1, "flex")
			require.NoError(t, err)
			require.InDelta(t, 200*tt.input*1.25*0.5, flex.CacheCreationCost, 1e-12)
		})
	}
}

func TestBillingService_GPT56UsesLongContextPricingAcrossModelsAndTiers(t *testing.T) {
	models := []struct {
		name               string
		input, cached      float64
		cacheWrite, output float64
	}{
		{name: "gpt-5.6-sol", input: 5e-6, cached: 0.5e-6, cacheWrite: 6.25e-6, output: 30e-6},
		{name: "gpt-5.6-terra", input: 2e-6, cached: 0.2e-6, cacheWrite: 2.5e-6, output: 12e-6},
		{name: "gpt-5.6-luna", input: 0.2e-6, cached: 0.02e-6, cacheWrite: 0.25e-6, output: 1.2e-6},
	}
	tiers := []struct {
		name       string
		priceScale float64
	}{
		{name: "standard", priceScale: 1},
		{name: "priority", priceScale: 2},
		{name: "flex", priceScale: 0.5},
	}
	tokens := UsageTokens{
		InputTokens:         100000,
		CacheCreationTokens: 100000,
		CacheReadTokens:     73000,
		OutputTokens:        10,
	}

	for _, model := range models {
		for _, tier := range tiers {
			t.Run(model.name+"/"+tier.name, func(t *testing.T) {
				svc := NewBillingService(&config.Config{}, nil)
				serviceTier := ""
				if tier.name != "standard" {
					serviceTier = tier.name
				}
				cost, err := svc.CalculateCostWithServiceTier(model.name, tokens, 1, serviceTier)
				require.NoError(t, err)
				require.InDelta(t, float64(tokens.InputTokens)*model.input*tier.priceScale*2, cost.InputCost, 1e-12)
				require.InDelta(t, float64(tokens.CacheCreationTokens)*model.cacheWrite*tier.priceScale*2, cost.CacheCreationCost, 1e-12)
				require.InDelta(t, float64(tokens.CacheReadTokens)*model.cached*tier.priceScale*2, cost.CacheReadCost, 1e-12)
				require.InDelta(t, float64(tokens.OutputTokens)*model.output*tier.priceScale*1.5, cost.OutputCost, 1e-12)
			})
		}
	}
}

func TestBillingService_GPT56LongContextBoundaryIsExclusive(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 100000, CacheCreationTokens: 100000, CacheReadTokens: 72000, OutputTokens: 10}

	cost, err := svc.CalculateCost("gpt-5.6-sol", tokens, 1)
	require.NoError(t, err)
	require.InDelta(t, 100000*5e-6, cost.InputCost, 1e-12)
	require.InDelta(t, 100000*6.25e-6, cost.CacheCreationCost, 1e-12)
	require.InDelta(t, 72000*0.5e-6, cost.CacheReadCost, 1e-12)
	require.InDelta(t, 10*30e-6, cost.OutputCost, 1e-12)
}

func TestPricingService_BareGPT56AliasDeterministicallyUsesSol(t *testing.T) {
	pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gpt-5.6-sol":   {InputCostPerToken: 5e-6},
		"gpt-5.6-terra": {InputCostPerToken: 2e-6},
		"gpt-5.6-luna":  {InputCostPerToken: 0.2e-6},
		"gpt-5.4":       {InputCostPerToken: 2.5e-6},
	}}

	for i := 0; i < 100; i++ {
		for _, alias := range []string{"gpt-5.6", "openai/gpt-5.6"} {
			pricing := pricingSvc.GetModelPricing(alias)
			require.NotNil(t, pricing)
			require.InDelta(t, 5e-6, pricing.InputCostPerToken, 1e-12, "iteration=%d alias=%s", i, alias)
		}
	}

	billingSvc := NewBillingService(&config.Config{}, pricingSvc)
	for _, alias := range []string{"gpt-5.6", "openai/gpt-5.6"} {
		pricing, err := billingSvc.GetModelPricing(alias)
		require.NoError(t, err)
		require.InDelta(t, 5e-6, pricing.InputPricePerToken, 1e-12)
		require.InDelta(t, 6.25e-6, pricing.CacheCreationPricePerToken, 1e-12)
	}
}

func TestDefaultPricingIncludesOfficialGPT56Rates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	pricingSvc := &PricingService{}
	pricingData, err := pricingSvc.parsePricingData(data)
	require.NoError(t, err)
	pricingSvc.pricingData = pricingData
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	tests := []struct {
		model                                                             string
		input, cached, cacheWrite, output                                 float64
		inputPriority, cachedPriority, cacheWritePriority, outputPriority float64
	}{
		{model: "gpt-5.6-sol", input: 5e-6, cached: 0.5e-6, cacheWrite: 6.25e-6, output: 30e-6, inputPriority: 10e-6, cachedPriority: 1e-6, cacheWritePriority: 12.5e-6, outputPriority: 60e-6},
		{model: "gpt-5.6-terra", input: 2e-6, cached: 0.2e-6, cacheWrite: 2.5e-6, output: 12e-6, inputPriority: 4e-6, cachedPriority: 0.4e-6, cacheWritePriority: 5e-6, outputPriority: 24e-6},
		{model: "gpt-5.6-luna", input: 0.2e-6, cached: 0.02e-6, cacheWrite: 0.25e-6, output: 1.2e-6, inputPriority: 0.4e-6, cachedPriority: 0.04e-6, cacheWritePriority: 0.5e-6, outputPriority: 2.4e-6},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			pricing, err := billingSvc.GetModelPricing(tt.model)
			require.NoError(t, err)
			require.InDelta(t, tt.input, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, tt.cached, pricing.CacheReadPricePerToken, 1e-12)
			require.InDelta(t, tt.cacheWrite, pricing.CacheCreationPricePerToken, 1e-12)
			require.InDelta(t, tt.output, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, tt.inputPriority, pricing.InputPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.cachedPriority, pricing.CacheReadPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.cacheWritePriority, pricing.CacheCreationPricePerTokenPriority, 1e-12)
			require.InDelta(t, tt.outputPriority, pricing.OutputPricePerTokenPriority, 1e-12)
			require.Equal(t, 272000, pricing.LongContextInputThreshold)
			require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
			require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)
		})
	}
}

func TestGPT56DedicatedFallbacksUseOfficialRates(t *testing.T) {
	tests := []struct {
		model                             string
		input, cached, cacheWrite, output float64
	}{
		{model: "gpt-5.6-sol", input: 5e-6, cached: 0.5e-6, cacheWrite: 6.25e-6, output: 30e-6},
		{model: "gpt-5.6-terra", input: 2e-6, cached: 0.2e-6, cacheWrite: 2.5e-6, output: 12e-6},
		{model: "gpt-5.6-luna", input: 0.2e-6, cached: 0.02e-6, cacheWrite: 0.25e-6, output: 1.2e-6},
	}

	for _, tt := range tests {
		t.Run(tt.model+"/pricing_service", func(t *testing.T) {
			pricingSvc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
			}}
			svc := NewBillingService(&config.Config{}, pricingSvc)
			pricing, err := svc.GetModelPricing(tt.model + "-preview")
			require.NoError(t, err)
			assertGPT56FallbackPricing(t, pricing, tt.input, tt.cached, tt.cacheWrite, tt.output)
		})

		t.Run(tt.model+"/billing_service", func(t *testing.T) {
			svc := NewBillingService(&config.Config{}, nil)
			pricing, err := svc.GetModelPricing(tt.model)
			require.NoError(t, err)
			assertGPT56FallbackPricing(t, pricing, tt.input, tt.cached, tt.cacheWrite, tt.output)
		})
	}
}

func assertGPT56FallbackPricing(t *testing.T, pricing *ModelPricing, input, cached, cacheWrite, output float64) {
	t.Helper()
	require.InDelta(t, input, pricing.InputPricePerToken, 1e-12)
	require.InDelta(t, cached, pricing.CacheReadPricePerToken, 1e-12)
	require.InDelta(t, cacheWrite, pricing.CacheCreationPricePerToken, 1e-12)
	require.InDelta(t, output, pricing.OutputPricePerToken, 1e-12)
	require.Equal(t, 272000, pricing.LongContextInputThreshold)
	require.InDelta(t, 2.0, pricing.LongContextInputMultiplier, 1e-12)
	require.InDelta(t, 1.5, pricing.LongContextOutputMultiplier, 1e-12)
}

func TestParsePricingData_KeepsImageOnlyPricing(t *testing.T) {
	svc := &PricingService{}
	body := []byte(`{
		"image-only-model": {
			"output_cost_per_image": 0.034,
			"litellm_provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`)

	data, err := svc.parsePricingData(body)
	require.NoError(t, err)
	pricing := data["image-only-model"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.034, pricing.OutputCostPerImage, 1e-12)
	require.Equal(t, "image_generation", pricing.Mode)
	// 仅有图片价的条目必须标记 token 价缺失，供 token 计费路径 fail-closed。
	require.True(t, pricing.TokenPricingAbsent)
}

func TestBillingService_GetModelPricing_FailsClosedForImageOnlyEntries(t *testing.T) {
	pricingSvc := &PricingService{}
	data, err := pricingSvc.parsePricingData([]byte(`{
		"imagen-9.0-generate": {
			"output_cost_per_image": 0.04,
			"litellm_provider": "vertex_ai-image-models",
			"mode": "image_generation"
		},
		"gemini-image-with-token-price": {
			"input_cost_per_token": 0.0,
			"output_cost_per_token": 0.0,
			"output_cost_per_image": 0.034,
			"litellm_provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`))
	require.NoError(t, err)
	pricingSvc.pricingData = data
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	// image-only 条目不得进入 token 计费（否则 token 流量按 $0 计费），
	// 必须落到 fallback / ErrModelPricingUnavailable 的 fail-closed 路径。
	_, err = billingSvc.GetModelPricing("imagen-9.0-generate")
	require.ErrorIs(t, err, ErrModelPricingUnavailable)

	// 显式 0 token 价的免费条目保持历史行为：正常返回。
	pricing, err := billingSvc.GetModelPricing("gemini-image-with-token-price")
	require.NoError(t, err)
	require.Zero(t, pricing.InputPricePerToken)

	// 图片计费路径不受影响：仍能读到 image-only 条目的图片单价。
	raw := pricingSvc.GetModelPricing("imagen-9.0-generate")
	require.NotNil(t, raw)
	require.InDelta(t, 0.04, raw.OutputCostPerImage, 1e-12)
}

func TestPricingService_MergesFallbackOnlyModels(t *testing.T) {
	dir := t.TempDir()
	fallbackFile := filepath.Join(dir, "fallback.json")
	require.NoError(t, os.WriteFile(fallbackFile, []byte(`{
		"remote-model": {
			"input_cost_per_token": 0.000001,
			"litellm_provider": "test",
			"mode": "chat"
		},
		"gemini-3.1-flash-lite-image": {
			"output_cost_per_image": 0.034,
			"litellm_provider": "vertex_ai-language-models",
			"mode": "image_generation"
		}
	}`), 0644))

	svc := &PricingService{cfg: &config.Config{}}
	svc.cfg.Pricing.FallbackFile = fallbackFile
	remoteData, err := svc.parsePricingData([]byte(`{
		"remote-model": {
			"input_cost_per_token": 0.000002,
			"litellm_provider": "test",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	merged := svc.mergeFallbackPricingData(remoteData)
	require.InDelta(t, 0.000002, merged["remote-model"].InputCostPerToken, 1e-12)
	require.NotNil(t, merged["gemini-3.1-flash-lite-image"])
	require.InDelta(t, 0.034, merged["gemini-3.1-flash-lite-image"].OutputCostPerImage, 1e-12)
}

func TestGetModelPricing_Gpt53CodexSparkUsesGpt51CodexPricing(t *testing.T) {
	sparkPricing := &LiteLLMModelPricing{InputCostPerToken: 1}
	gpt53Pricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": sparkPricing,
			"gpt-5.3":       gpt53Pricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex-spark")
	require.Same(t, sparkPricing, got)
}

func TestGetModelPricing_Gpt53CodexFallbackStillUsesGpt52Codex(t *testing.T) {
	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)
}

func TestGetModelPricing_OpenAIFallbackMatchedLoggedAsInfo(t *testing.T) {
	logSink, restore := captureStructuredLog(t)
	defer restore()

	gpt52CodexPricing := &LiteLLMModelPricing{InputCostPerToken: 2}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.2-codex": gpt52CodexPricing,
		},
	}

	got := svc.GetModelPricing("gpt-5.3-codex")
	require.Same(t, gpt52CodexPricing, got)

	require.True(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "info"))
	require.False(t, logSink.ContainsMessageAtLevel("[Pricing] OpenAI fallback matched gpt-5.3-codex -> gpt-5.2-codex", "warn"))
}

func TestGetModelPricing_Gpt54UsesStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": &LiteLLMModelPricing{InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4")
	require.NotNil(t, got)
	require.InDelta(t, 2.5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 15e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2.5e-7, got.CacheReadInputTokenCost, 1e-12)
	require.Equal(t, 272000, got.LongContextInputTokenThreshold)
	require.InDelta(t, 2.0, got.LongContextInputCostMultiplier, 1e-12)
	require.InDelta(t, 1.5, got.LongContextOutputCostMultiplier, 1e-12)
}

func TestGetModelPricing_OpenAICompactAliasUsesStaticFallback(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("openai/gpt5.5")
	require.NotNil(t, got)
	require.InDelta(t, 5e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 30e-6, got.OutputCostPerToken, 1e-12)
}

func TestPricingService_Gemini36FlashThinkingTiersUseBasePricing(t *testing.T) {
	basePricing := &LiteLLMModelPricing{
		InputCostPerToken:       1.5e-6,
		OutputCostPerToken:      7.5e-6,
		CacheReadInputTokenCost: 0.15e-6,
	}
	svc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gemini-3.6-flash": basePricing,
	}}

	for _, model := range []string{
		"gemini-3.6-flash",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
	} {
		t.Run(model, func(t *testing.T) {
			require.Same(t, basePricing, svc.GetModelPricing(model))
		})
	}
}

func TestPricingService_Gemini36FlashTierSpecificPricingTakesPrecedence(t *testing.T) {
	basePricing := &LiteLLMModelPricing{InputCostPerToken: 1.5e-6}
	tierPricing := &LiteLLMModelPricing{InputCostPerToken: 2e-6}
	svc := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"gemini-3.6-flash":     basePricing,
		"gemini-3.6-flash-low": tierPricing,
	}}

	require.Same(t, tierPricing, svc.GetModelPricing("models/gemini-3.6-flash-low"))
}

func TestBillingService_Gemini36FlashThinkingTierFallbacksAreBillable(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	tokens := UsageTokens{InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000}

	for _, model := range []string{
		"gemini-3.6-flash",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-low",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-tiered",
	} {
		t.Run(model, func(t *testing.T) {
			cost, err := svc.CalculateCost(model, tokens, 1)
			require.NoError(t, err)
			require.InDelta(t, 1.5, cost.InputCost, 1e-12)
			require.InDelta(t, 7.5, cost.OutputCost, 1e-12)
			require.InDelta(t, 0.15, cost.CacheReadCost, 1e-12)
			require.InDelta(t, 9.15, cost.TotalCost, 1e-12)
		})
	}
}

func TestDefaultPricingIncludesGemini36FlashRates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	pricingSvc := &PricingService{}
	pricingData, err := pricingSvc.parsePricingData(data)
	require.NoError(t, err)
	pricingSvc.pricingData = pricingData
	billingSvc := NewBillingService(&config.Config{}, pricingSvc)

	for _, model := range []string{"gemini-3.6-flash", "gemini-3.6-flash-low", "gemini-3.6-flash-high"} {
		t.Run(model, func(t *testing.T) {
			pricing, err := billingSvc.GetModelPricing(model)
			require.NoError(t, err)
			require.InDelta(t, 1.5e-6, pricing.InputPricePerToken, 1e-12)
			require.InDelta(t, 7.5e-6, pricing.OutputPricePerToken, 1e-12)
			require.InDelta(t, 0.15e-6, pricing.CacheReadPricePerToken, 1e-12)
		})
	}
}

func TestDefaultPricingUsesCurrentCodexAutoReviewBaseRates(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "resources", "model-pricing", "model_prices_and_context_window.json"))
	require.NoError(t, err)

	svc := &PricingService{}
	pricingData, err := svc.parsePricingData(data)
	require.NoError(t, err)
	svc.pricingData = pricingData

	got := svc.GetModelPricing("codex-auto-review")
	require.NotNil(t, got)
	require.InDelta(t, 0.2e-6, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.2e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.02e-6, got.CacheReadInputTokenCost, 1e-12)

	// Auto-review is an internal Codex model. Do not infer public GPT-5.6 API
	// service-tier, cache-write, or long-context pricing without an upstream
	// usage contract for this dedicated model.
	require.Zero(t, got.InputCostPerTokenPriority)
	require.Zero(t, got.OutputCostPerTokenPriority)
	require.Zero(t, got.CacheReadInputTokenCostPriority)
	require.Zero(t, got.CacheCreationInputTokenCost)
	require.Zero(t, got.CacheCreationInputTokenCostPriority)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_Gpt54MiniUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-mini")
	require.NotNil(t, got)
	require.InDelta(t, 7.5e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 4.5e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 7.5e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_Gpt54NanoUsesDedicatedStaticFallbackWhenRemoteMissing(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-5.1-codex": {InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.GetModelPricing("gpt-5.4-nano")
	require.NotNil(t, got)
	require.InDelta(t, 2e-7, got.InputCostPerToken, 1e-12)
	require.InDelta(t, 1.25e-6, got.OutputCostPerToken, 1e-12)
	require.InDelta(t, 2e-8, got.CacheReadInputTokenCost, 1e-12)
	require.Zero(t, got.LongContextInputTokenThreshold)
}

func TestGetModelPricing_ImageModelDoesNotFallbackToTextModel(t *testing.T) {
	imagePricing := &LiteLLMModelPricing{InputCostPerToken: 3}
	textPricing := &LiteLLMModelPricing{InputCostPerToken: 9}

	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2": imagePricing,
			"gpt-5.4":     textPricing,
		},
	}

	got := svc.GetModelPricing("gpt-image-3")
	require.Same(t, imagePricing, got)
}

func TestParsePricingData_PreservesPriorityAndServiceTierFields(t *testing.T) {
	raw := map[string]any{
		"gpt-5.4": map[string]any{
			"input_cost_per_token":                 2.5e-6,
			"input_cost_per_token_priority":        5e-6,
			"output_cost_per_token":                15e-6,
			"output_cost_per_token_priority":       30e-6,
			"cache_read_input_token_cost":          0.25e-6,
			"cache_read_input_token_cost_priority": 0.5e-6,
			"supports_service_tier":                true,
			"supports_prompt_caching":              true,
			"litellm_provider":                     "openai",
			"mode":                                 "chat",
		},
	}
	body, err := json.Marshal(raw)
	require.NoError(t, err)

	svc := &PricingService{}
	pricingMap, err := svc.parsePricingData(body)
	require.NoError(t, err)

	pricing := pricingMap["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 2.5e-6, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 5e-6, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 15e-6, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 30e-6, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.25e-6, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.5e-6, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

func TestParsePricingData_PreservesServiceTierPriorityFields(t *testing.T) {
	svc := &PricingService{}
	pricingData, err := svc.parsePricingData([]byte(`{
		"gpt-5.4": {
			"input_cost_per_token": 0.0000025,
			"input_cost_per_token_priority": 0.000005,
			"output_cost_per_token": 0.000015,
			"output_cost_per_token_priority": 0.00003,
			"cache_read_input_token_cost": 0.00000025,
			"cache_read_input_token_cost_priority": 0.0000005,
			"supports_service_tier": true,
			"litellm_provider": "openai",
			"mode": "chat"
		}
	}`))
	require.NoError(t, err)

	pricing := pricingData["gpt-5.4"]
	require.NotNil(t, pricing)
	require.InDelta(t, 0.0000025, pricing.InputCostPerToken, 1e-12)
	require.InDelta(t, 0.000005, pricing.InputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.000015, pricing.OutputCostPerToken, 1e-12)
	require.InDelta(t, 0.00003, pricing.OutputCostPerTokenPriority, 1e-12)
	require.InDelta(t, 0.00000025, pricing.CacheReadInputTokenCost, 1e-12)
	require.InDelta(t, 0.0000005, pricing.CacheReadInputTokenCostPriority, 1e-12)
	require.True(t, pricing.SupportsServiceTier)
}

// ---------------------------------------------------------------------------
// ListModelNamesByProvider
// ---------------------------------------------------------------------------

func TestListModelNamesByProvider_ReturnsMatchingModels(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"claude-opus-4-5-20251101": {LiteLLMProvider: "anthropic", InputCostPerToken: 1.5e-5},
			"claude-sonnet-4-5":        {LiteLLMProvider: "anthropic", InputCostPerToken: 3e-6},
			"gpt-4o":                   {LiteLLMProvider: "openai", InputCostPerToken: 5e-6},
			"gemini-2.5-pro":           {LiteLLMProvider: "google", InputCostPerToken: 1.25e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.ElementsMatch(t, []string{"claude-opus-4-5-20251101", "claude-sonnet-4-5"}, got)
	// Must be sorted
	require.Equal(t, "claude-opus-4-5-20251101", got[0])
	require.Equal(t, "claude-sonnet-4-5", got[1])
}

func TestListModelNamesByProvider_CaseInsensitive(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-4o": {LiteLLMProvider: "OpenAI", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.Equal(t, []string{"gpt-4o"}, got)

	got2 := svc.ListModelNamesByProvider("OPENAI")
	require.Equal(t, []string{"gpt-4o"}, got2)
}

func TestListModelNamesByProvider_NoMatch(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-4o": {LiteLLMProvider: "openai", InputCostPerToken: 5e-6},
		},
	}

	got := svc.ListModelNamesByProvider("anthropic")
	require.NotNil(t, got)
	require.Empty(t, got)
}

func TestListModelNamesByProvider_EmptyCatalog(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{},
	}

	got := svc.ListModelNamesByProvider("openai")
	require.NotNil(t, got)
	require.Empty(t, got)
}

// ---------------------------------------------------------------------------
// GPT Image 2.5 dedicated fallback (2026-09-08)
// ---------------------------------------------------------------------------

func TestPricingService_Image25ExactIDsUseDedicatedFallback(t *testing.T) {
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2":   {InputCostPerToken: 111},
			"gpt-image-1.5": {InputCostPerToken: 222},
			"gpt-image-1":   {InputCostPerToken: 333},
		},
	}
	tests := []struct {
		name  string
		model string
	}{
		{name: "flare", model: "gpt-image-2.5-flare"},
		{name: "sunburst", model: "gpt-image-2.5-sunburst"},
		{name: "flare date snapshot", model: "gpt-image-2.5-flare-2026-09-08"},
		{name: "sunburst date snapshot", model: "gpt-image-2.5-sunburst-2026-09-08"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.GetModelPricing(tt.model)
			require.Same(t, openAIGPTImage25FallbackPricing, got)
			require.InDelta(t, 5e-06, got.InputCostPerToken, 1e-12)
			require.InDelta(t, 1.25e-06, got.CacheReadInputTokenCost, 1e-12)
			require.InDelta(t, 8e-06, got.InputCostPerImageToken, 1e-12)
			require.InDelta(t, 3e-05, got.OutputCostPerImageToken, 1e-12)
			require.Equal(t, "openai", got.LiteLLMProvider)
			require.Equal(t, "image_generation", got.Mode)
			require.True(t, got.SupportsPromptCaching)
		})
	}
}

func TestPricingService_Image25ExactCatalogEntriesWinOverFallback(t *testing.T) {
	flare := &LiteLLMModelPricing{InputCostPerToken: 1e-6, OutputCostPerImageToken: 9e-6}
	sunburst := &LiteLLMModelPricing{InputCostPerToken: 2e-6, OutputCostPerImageToken: 8e-6}
	flareDate := &LiteLLMModelPricing{InputCostPerToken: 3e-6, OutputCostPerImageToken: 7e-6}
	sunburstDate := &LiteLLMModelPricing{InputCostPerToken: 4e-6, OutputCostPerImageToken: 6e-6}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2.5-flare":               flare,
			"gpt-image-2.5-sunburst":            sunburst,
			"gpt-image-2.5-flare-2026-09-08":    flareDate,
			"gpt-image-2.5-sunburst-2026-09-08": sunburstDate,
		},
	}
	tests := []struct {
		name  string
		model string
		want  *LiteLLMModelPricing
	}{
		{name: "flare", model: "gpt-image-2.5-flare", want: flare},
		{name: "sunburst", model: "gpt-image-2.5-sunburst", want: sunburst},
		{name: "flare date snapshot", model: "gpt-image-2.5-flare-2026-09-08", want: flareDate},
		{name: "sunburst date snapshot", model: "gpt-image-2.5-sunburst-2026-09-08", want: sunburstDate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.GetModelPricing(tt.model)
			require.Same(t, tt.want, got)
		})
	}
}

func TestPricingService_Image25DateSnapshotMissingUsesDedicatedFallbackBeforeOldImages(t *testing.T) {
	flare := &LiteLLMModelPricing{InputCostPerToken: 1e-6}
	sunburst := &LiteLLMModelPricing{InputCostPerToken: 2e-6}
	oldImage := &LiteLLMModelPricing{InputCostPerToken: 111}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2.5-flare":    flare,
			"gpt-image-2.5-sunburst": sunburst,
			"gpt-image-2":            oldImage,
		},
	}
	tests := []struct {
		model string
	}{
		{"gpt-image-2.5-flare-2026-09-08"},
		{"gpt-image-2.5-sunburst-2026-09-08"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := svc.GetModelPricing(tt.model)
			// 日期快照缺精确条目时必须使用 2.5 专用回退；绝不掉到 Image2/1.5/1，
			// 也不得仅仅因为主 ID 存在就被当作同一价格卡。
			require.Same(t, openAIGPTImage25FallbackPricing, got)
			require.NotSame(t, oldImage, got)
			require.NotSame(t, flare, got)
			require.NotSame(t, sunburst, got)
		})
	}
}

func TestPricingService_Image25MissingEntryUsesDedicatedFallbackNotOldImages(t *testing.T) {
	image2 := &LiteLLMModelPricing{InputCostPerToken: 111}
	image15 := &LiteLLMModelPricing{InputCostPerToken: 222}
	image1 := &LiteLLMModelPricing{InputCostPerToken: 333}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2":   image2,
			"gpt-image-1.5": image15,
			"gpt-image-1":   image1,
		},
	}
	tests := []struct {
		name  string
		model string
	}{
		{name: "flare", model: "gpt-image-2.5-flare"},
		{name: "sunburst", model: "gpt-image-2.5-sunburst"},
		{name: "flare date snapshot", model: "gpt-image-2.5-flare-2026-09-08"},
		{name: "sunburst date snapshot", model: "gpt-image-2.5-sunburst-2026-09-08"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.GetModelPricing(tt.model)
			require.Same(t, openAIGPTImage25FallbackPricing, got)
		})
	}
}

func TestPricingService_Image25UnknownNeighborNotCaptured(t *testing.T) {
	image2 := &LiteLLMModelPricing{InputCostPerToken: 111}
	image15 := &LiteLLMModelPricing{InputCostPerToken: 222}
	image1 := &LiteLLMModelPricing{InputCostPerToken: 333}
	svc := &PricingService{
		pricingData: map[string]*LiteLLMModelPricing{
			"gpt-image-2":   image2,
			"gpt-image-1.5": image15,
			"gpt-image-1":   image1,
		},
	}
	tests := []struct {
		model    string
		wantSame *LiteLLMModelPricing
	}{
		{model: "gpt-image-2.5-foo", wantSame: image2},
		{model: "gpt-image-2.5-flare-2026-09-09", wantSame: image2},
		{model: "gpt-image-2.5-2026-09-08", wantSame: image2},
		{model: "gpt-image-2.5", wantSame: image2},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			got := svc.GetModelPricing(tt.model)
			require.Same(t, tt.wantSame, got)
		})
	}
}

func TestPricingService_Image25ExplicitOverrideAndHotReloadPreservePriority(t *testing.T) {
	svc := newHotReloadPricingService(t,
		`{`+
			hotReloadModelJSON("gpt-image-2.5-flare", 6e-6, 12e-6)+`,`+
			hotReloadModelJSON("gpt-image-2.5-flare-2026-09-08", 8e-6, 16e-6)+`,`+
			hotReloadModelJSON("gpt-image-2.5-sunburst", 7e-6, 14e-6)+`,`+
			hotReloadModelJSON("gpt-image-2.5-sunburst-2026-09-08", 10e-6, 20e-6)+`}`,
		`{
			"gpt-image-2.5-flare": {"input_cost_per_token": 9e-06},
			"gpt-image-2.5-sunburst-2026-09-08": {"input_cost_per_token": 1.1e-05}
		}`)

	for _, tc := range []struct {
		model         string
		input, output float64
	}{
		{"gpt-image-2.5-flare", 9e-6, 12e-6},
		{"gpt-image-2.5-flare-2026-09-08", 8e-6, 16e-6},
		{"gpt-image-2.5-sunburst", 7e-6, 14e-6},
		{"gpt-image-2.5-sunburst-2026-09-08", 11e-6, 20e-6},
	} {
		t.Run(tc.model, func(t *testing.T) {
			pricing := svc.GetModelPricing(tc.model)
			require.NotSame(t, openAIGPTImage25FallbackPricing, pricing)
			require.InDelta(t, tc.input, pricing.InputCostPerToken, 1e-12)
			require.InDelta(t, tc.output, pricing.OutputCostPerToken, 1e-12, "partial overrides must preserve the other catalog fields")
		})
	}

	// A real fallback-file price change must rebuild the snapshot without
	// displacing the explicit override on the other Image 2.5 model.
	require.NoError(t, os.WriteFile(svc.cfg.Pricing.FallbackFile, []byte(`{`+
		hotReloadModelJSON("gpt-image-2.5-flare", 6e-6, 12e-6)+`,`+
		hotReloadModelJSON("gpt-image-2.5-sunburst", 13e-6, 14e-6)+`}`), 0644))
	svc.reloadIfCustomFilesChanged()
	require.InDelta(t, 9e-6, svc.GetModelPricing("gpt-image-2.5-flare").InputCostPerToken, 1e-12)
	require.InDelta(t, 13e-6, svc.GetModelPricing("gpt-image-2.5-sunburst").InputCostPerToken, 1e-12)
}

func TestPricingService_Image25FallbackPreservesExplicitZeroForTokenBilling(t *testing.T) {
	catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	billing := &BillingService{pricingService: catalog}
	for _, model := range []string{
		"gpt-image-2.5-flare", "gpt-image-2.5-sunburst",
		"gpt-image-2.5-flare-2026-09-08", "gpt-image-2.5-sunburst-2026-09-08",
	} {
		t.Run(model, func(t *testing.T) {
			pricing, err := billing.GetModelPricing(model)
			require.NoError(t, err)
			require.True(t, pricing.InputPricePresent)
			require.True(t, pricing.OutputPricePresent)
			require.Zero(t, pricing.OutputPricePerToken, "the reference has an explicit zero text-output rate")
			require.InDelta(t, 8e-6, pricing.ImageInputPricePerToken, 1e-12)
			require.InDelta(t, 30e-6, pricing.ImageOutputPricePerToken, 1e-12)
			require.True(t, pricing.ImageOutputPriceExplicit)
			require.NoError(t, billing.PreflightTokenPricing(context.Background(), model, nil, nil))
		})
	}
}

func TestPricingService_Image25ExistingImageOverridesRemainAuthoritative(t *testing.T) {
	catalog := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	billing := &BillingService{pricingService: catalog}
	for _, model := range []string{"gpt-image-2.5-flare", "gpt-image-2.5-sunburst"} {
		t.Run(model, func(t *testing.T) {
			// A per-image-token rate is not an invented per-image price. Without
			// a configured per-image price, the existing image preflight stays closed.
			require.Error(t, billing.PreflightImagePricing(context.Background(), model, ImageBillingSize1K, nil, nil, nil))
			for _, unitPrice := range []float64{0, 0.12} {
				group := &ImagePriceConfig{Price1K: &unitPrice}
				require.NoError(t, billing.PreflightImagePricing(context.Background(), model, ImageBillingSize1K, nil, group, nil))
				cost, err := billing.CalculateImageCostChecked(model, ImageBillingSize1K, 2, group, 1.5)
				require.NoError(t, err)
				require.InDelta(t, unitPrice*2, cost.TotalCost, 1e-12)
				require.InDelta(t, unitPrice*3, cost.ActualCost, 1e-12)
				require.Equal(t, string(BillingModeImage), cost.BillingMode)
			}

			imageOutputPrice := 40e-6
			channel := &ChannelModelPricing{ImageOutputPrice: &imageOutputPrice}
			pricing, err := billing.GetModelPricingWithChannel(model, channel)
			require.NoError(t, err)
			require.Equal(t, imageOutputPrice, pricing.ImageOutputPricePerToken)
			require.True(t, pricing.ImageOutputPriceExplicit)

			// Existing channel semantics intentionally use explicit zero when
			// the channel leaves its image-output price unset.
			channel.ImageOutputPrice = nil
			pricing, err = billing.GetModelPricingWithChannel(model, channel)
			require.NoError(t, err)
			require.Zero(t, pricing.ImageOutputPricePerToken)
			require.True(t, pricing.ImageOutputPriceExplicit)
			require.Equal(t, 30e-6, catalog.GetModelPricing(model).OutputCostPerImageToken, "overrides must not mutate the shared fallback card")
		})
	}
}
