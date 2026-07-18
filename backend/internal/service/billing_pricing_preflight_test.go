package service

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestBillingPricingPreflightUnknownModelIsTypedUnavailable(t *testing.T) {
	svc := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{}})

	err := svc.PreflightTokenPricing(context.Background(), "totally-unknown-model", nil, nil)

	require.Error(t, err)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Equal(t, 503, infraerrors.Code(err))
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(err))
}

func TestBillingPricingPreflightExplicitZeroTokenPriceIsAllowed(t *testing.T) {
	pricingService := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"free-model": {
			InputCostPerToken:         0,
			OutputCostPerToken:        0,
			InputCostPerTokenPresent:  true,
			OutputCostPerTokenPresent: true,
		},
	}}
	svc := NewBillingService(&config.Config{}, pricingService)

	require.NoError(t, svc.PreflightTokenPricing(context.Background(), "free-model", nil, nil))
	cost, err := svc.CalculateCost("free-model", UsageTokens{InputTokens: 100, OutputTokens: 50}, 1)
	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)
}

func TestBillingPricingPreflightKnownZeroFallbacksAreAllowed(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	for _, model := range []string{"glm-4.5-flash", "glm-4.7-flash", "doubao-embedding-vision"} {
		t.Run(model, func(t *testing.T) {
			require.NoError(t, svc.PreflightTokenPricing(context.Background(), model, nil, nil))
		})
	}
}

func TestBillingPricingPreflightIntervalOnlyTokenPricing(t *testing.T) {
	inputLow, outputLow := 0.001, 0.002
	inputHigh, outputHigh := 0.003, 0.004
	boundary := 100

	tests := []struct {
		name      string
		intervals []PricingInterval
		wantError bool
	}{
		{
			name: "continuous from zero to unbounded",
			intervals: []PricingInterval{
				{MinTokens: 0, MaxTokens: &boundary, InputPrice: &inputLow, OutputPrice: &outputLow},
				{MinTokens: boundary, InputPrice: &inputHigh, OutputPrice: &outputHigh},
			},
		},
		{
			name: "gap still requires base pricing",
			intervals: []PricingInterval{
				{MinTokens: 0, MaxTokens: &boundary, InputPrice: &inputLow, OutputPrice: &outputLow},
				{MinTokens: boundary + 1, InputPrice: &inputHigh, OutputPrice: &outputHigh},
			},
			wantError: true,
		},
		{
			name: "every interval requires input and output",
			intervals: []PricingInterval{
				{MinTokens: 0, MaxTokens: &boundary, InputPrice: &inputLow, OutputPrice: &outputLow},
				{MinTokens: boundary, InputPrice: &inputHigh},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const (
				groupID = int64(901)
				model   = "interval-only-model"
			)
			billing := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{}})
			channel := &ChannelService{}
			cache := newEmptyChannelCache()
			cache.loadedAt = time.Now()
			cache.groupPlatform[groupID] = PlatformOpenAI
			cache.channelByGroupID[groupID] = &Channel{ID: 902, Status: StatusActive, BillingModelSource: BillingModelSourceChannelMapped}
			cache.pricingByGroupModel[channelModelKey{groupID: groupID, platform: PlatformOpenAI, model: model}] = &ChannelModelPricing{
				Models:      []string{model},
				Platform:    PlatformOpenAI,
				BillingMode: BillingModeToken,
				Intervals:   tt.intervals,
			}
			channel.cache.Store(cache)
			resolver := NewModelPricingResolver(channel, billing)

			err := billing.PreflightTokenPricing(context.Background(), model, func() *int64 { value := groupID; return &value }(), resolver)
			if tt.wantError {
				require.Error(t, err)
				require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(err))
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestBillingPricingPreflightExplicitZeroPerRequestDoesNotFallThrough(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	resolved := &ResolvedPricing{
		Mode:                          BillingModeImage,
		DefaultPerRequestPrice:        0,
		DefaultPerRequestPricePresent: true,
	}

	cost, err := svc.CalculateCostUnified(CostInput{
		Model:          "free-image-model",
		RequestCount:   3,
		RateMultiplier: 1,
		Resolver:       &ModelPricingResolver{},
		Resolved:       resolved,
	})

	require.NoError(t, err)
	require.Zero(t, cost.ActualCost)
}

func TestBillingPricingPreflightMissingImageSizeUsesExplicitDefaultTier(t *testing.T) {
	free := 0.0
	svc := NewBillingService(&config.Config{}, nil)

	err := svc.PreflightImagePricing(
		context.Background(),
		"custom-image-model",
		"",
		nil,
		&ImagePriceConfig{Price2K: &free},
		nil,
	)

	require.NoError(t, err)
}

func TestBillingPricingPreflightUnknownMediaPricesAreRejected(t *testing.T) {
	svc := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{}})

	imageErr := svc.PreflightImagePricing(context.Background(), "unknown-image", "1K", nil, nil, nil)
	videoErr := svc.PreflightVideoPricing(context.Background(), "unknown-video", "720p", nil, nil, nil)

	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(imageErr))
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(videoErr))
}

func TestResponsesPricingPreflightValidatesTokenAndImageBranches(t *testing.T) {
	const (
		pricedTokenModel  = "mixed-intent-token-priced"
		missingTokenModel = "mixed-intent-token-missing"
		missingImageModel = "mixed-intent-image-missing"
	)
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		pricedTokenModel: {
			InputCostPerToken:         0.000001,
			OutputCostPerToken:        0.000002,
			InputCostPerTokenPresent:  true,
			OutputCostPerTokenPresent: true,
		},
	}}
	billing := NewBillingService(&config.Config{}, pricing)
	svc := &OpenAIGatewayService{billingService: billing}
	imagePrice := 0.02
	imageBody := func(model, imageModel string) []byte {
		return []byte(`{"model":"` + model + `","tools":[{"type":"image_generation","model":"` + imageModel + `","size":"1024x1024"}]}`)
	}

	t.Run("configured image price cannot hide missing token price", func(t *testing.T) {
		apiKey := &APIKey{Group: &Group{ImagePrice1K: &imagePrice}}
		err := svc.PreflightResponsesRequestPricing(
			context.Background(), apiKey, nil, missingTokenModel,
			ChannelMappingResult{MappedModel: missingTokenModel},
			imageBody(missingTokenModel, missingImageModel),
		)

		require.Error(t, err)
		require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(err))
	})

	t.Run("configured token price cannot hide missing image price", func(t *testing.T) {
		err := svc.PreflightResponsesRequestPricing(
			context.Background(), &APIKey{}, nil, pricedTokenModel,
			ChannelMappingResult{MappedModel: pricedTokenModel},
			imageBody(pricedTokenModel, missingImageModel),
		)

		require.Error(t, err)
		require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(err))
	})

	t.Run("both possible billing branches are priced", func(t *testing.T) {
		apiKey := &APIKey{Group: &Group{ImagePrice1K: &imagePrice}}
		err := svc.PreflightResponsesRequestPricing(
			context.Background(), apiKey, nil, pricedTokenModel,
			ChannelMappingResult{MappedModel: pricedTokenModel},
			imageBody(pricedTokenModel, missingImageModel),
		)

		require.NoError(t, err)
	})
}

func TestMessagesPricingPreflightUsesEffectiveDispatchModel(t *testing.T) {
	const (
		requestedModel = "claude-sonnet-priced-only-as-requested"
		effectiveModel = "messages-effective-priced"
		missingModel   = "messages-effective-missing"
	)
	pricing := &PricingService{pricingData: map[string]*LiteLLMModelPricing{}}
	addTokenPrice := func(model string) {
		pricing.pricingData[model] = &LiteLLMModelPricing{
			InputCostPerToken:         0.000001,
			OutputCostPerToken:        0.000002,
			InputCostPerTokenPresent:  true,
			OutputCostPerTokenPresent: true,
		}
	}
	addTokenPrice(requestedModel)
	addTokenPrice(effectiveModel)
	billing := NewBillingService(&config.Config{}, pricing)
	svc := &OpenAIGatewayService{billingService: billing}
	mapping := ChannelMappingResult{MappedModel: requestedModel}

	t.Run("missing dispatch model is rejected even when requested model is priced", func(t *testing.T) {
		err := svc.PreflightMessagesRequestPricing(
			context.Background(), &APIKey{}, nil, requestedModel, missingModel, mapping,
		)

		require.Error(t, err)
		require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(err))
	})

	t.Run("priced dispatch model is accepted", func(t *testing.T) {
		err := svc.PreflightMessagesRequestPricing(
			context.Background(), &APIKey{}, nil, requestedModel, effectiveModel, mapping,
		)

		require.NoError(t, err)
	})

	t.Run("explicit requested billing source keeps requested price", func(t *testing.T) {
		requestedMapping := mapping
		requestedMapping.BillingModelSource = BillingModelSourceRequested
		err := svc.PreflightMessagesRequestPricing(
			context.Background(), &APIKey{}, nil, requestedModel, missingModel, requestedMapping,
		)

		require.NoError(t, err)
	})
}

func TestBillingPricingPreflightRejectsNonFiniteAndOutOfRangePrices(t *testing.T) {
	tests := []float64{math.NaN(), math.Inf(1), math.Inf(-1), -0.01, maxBillingUnitPriceUSD + 1}
	for _, value := range tests {
		t.Run("invalid", func(t *testing.T) {
			svc := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{
				"bad-model": {
					InputCostPerToken:         value,
					OutputCostPerToken:        1,
					InputCostPerTokenPresent:  true,
					OutputCostPerTokenPresent: true,
				},
			}})

			err := svc.PreflightTokenPricing(context.Background(), "bad-model", nil, nil)
			require.Error(t, err)
			require.Equal(t, "BILLING_PRICING_INVALID", infraerrors.Reason(err))
		})
	}
}

func TestPricingServiceRejectsInvalidRemoteEntry(t *testing.T) {
	svc := &PricingService{}
	_, err := svc.parsePricingData([]byte(`{
		"bad-model": {"input_cost_per_token": -1, "output_cost_per_token": 0}
	}`))

	require.Error(t, err)
	require.Contains(t, err.Error(), "no valid pricing entries")
}

func TestChannelPricingValidationRejectsNonFiniteInterval(t *testing.T) {
	bad := math.NaN()
	err := validatePricingEntries([]ChannelModelPricing{{
		Models:      []string{"bad-model"},
		BillingMode: BillingModeToken,
		Intervals: []PricingInterval{{
			MinTokens:  0,
			InputPrice: &bad,
		}},
	}})

	require.Error(t, err)
	require.Equal(t, "INVALID_PRICE", infraerrors.Reason(err))
}

func TestBatchImagePricingResolverAllowsExplicitZero(t *testing.T) {
	pricingService := &PricingService{pricingData: map[string]*LiteLLMModelPricing{
		"free-batch-image": {
			InputCostPerTokenPresent:       true,
			OutputCostPerTokenPresent:      true,
			OutputCostPerImageTokenPresent: true,
		},
	}}
	billing := NewBillingService(&config.Config{}, pricingService)
	resolver := &BatchImageModelPricingResolver{Resolver: NewModelPricingResolver(nil, billing)}

	price, err := resolver.BatchImageUnitPrice(context.Background(), &BatchImageJob{Model: "free-batch-image"})

	require.NoError(t, err)
	require.Zero(t, price)
}

func TestBatchImagePricingUnavailableKeepsLegacySentinel(t *testing.T) {
	billing := NewBillingService(&config.Config{}, &PricingService{pricingData: map[string]*LiteLLMModelPricing{}})
	resolver := &BatchImageModelPricingResolver{Resolver: NewModelPricingResolver(nil, billing)}

	_, err := resolver.BatchImageUnitPrice(context.Background(), &BatchImageJob{Model: "missing"})

	require.Error(t, err)
	require.True(t, errors.Is(err, ErrBatchImageSettlementPricingMissing))
	require.Equal(t, "BILLING_PRICING_UNAVAILABLE", infraerrors.Reason(err))
}
