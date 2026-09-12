//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPriceQuoteService_UsesModelPricingResolverAndKeepsUnknownZeroAbsent(t *testing.T) {
	resolver := NewModelPricingResolver(nil, newTestBillingServiceForResolver())
	service := NewPriceQuoteService(resolver, nil)
	quote := service.Quote(context.Background(), PriceQuoteInput{Model: "claude-sonnet-4"})
	require.NotNil(t, quote)
	require.NotNil(t, quote.InputPerMillion)
	require.InDelta(t, 3, *quote.InputPerMillion, 1e-9)
	require.NotNil(t, quote.OutputPerMillion)
	require.NotEmpty(t, quote.QuoteVersion)
	require.Equal(t, "not_configured", quote.DynamicFactorStatus)

	unknown := service.Quote(context.Background(), PriceQuoteInput{Model: "model-without-price"})
	require.Nil(t, unknown.InputPerMillion)
	require.Nil(t, unknown.OutputPerMillion)
	require.Contains(t, unknown.UnknownFields, "input_per_million")
	require.NotContains(t, unknown.UnknownFields, "0")
}

func TestPriceQuoteService_ExplicitDynamicFactorIsNotFabricated(t *testing.T) {
	factor := &priceFactorStub{factor: 0.75}
	resolver := NewModelPricingResolver(nil, newTestBillingServiceForResolver())
	service := NewPriceQuoteService(resolver, factor)
	quote := service.Quote(context.Background(), PriceQuoteInput{Model: "claude-sonnet-4", GroupID: 1, UserID: 2})
	require.Equal(t, "available", quote.DynamicFactorStatus)
	require.NotNil(t, quote.DynamicFactor)
	require.InDelta(t, 0.75, quote.DynamicFactor.Factor, 1e-9)

	without := NewPriceQuoteService(resolver, nil).Quote(context.Background(), PriceQuoteInput{Model: "claude-sonnet-4", GroupID: 1})
	require.Nil(t, without.DynamicFactor)
	require.Equal(t, "not_configured", without.DynamicFactorStatus)
}

type priceFactorStub struct{ factor float64 }

func (s *priceFactorStub) ResolveDynamicPriceFactor(context.Context, DynamicPriceFactorInput) (*DynamicPriceFactor, error) {
	return &DynamicPriceFactor{Factor: s.factor, Source: "test", Version: "v1"}, nil
}

func TestPriceQuoteService_ImageIndependentRateOverridesConsumerAndPeak(t *testing.T) {
	inputPrice := 1e-6
	outputPrice := 2e-6
	group := &Group{
		Platform: "anthropic", RateMultiplier: 2, PeakRateEnabled: true, PeakStart: "00:00", PeakEnd: "23:59",
		PeakRateMultiplier: 3, ImageRateIndependent: true, ImageRateMultiplier: 0.5,
		ModelPricing: []ChannelModelPricing{{
			Models: []string{"image-model"}, BillingMode: BillingModeImage,
			InputPrice: &inputPrice, OutputPrice: &outputPrice,
		}},
	}
	service := NewPriceQuoteService(NewModelPricingResolver(nil, newTestBillingServiceForResolver()), nil)
	quote := service.Quote(context.Background(), PriceQuoteInput{Model: "image-model", GroupID: 1, Group: group, StaticRateMultiplier: priceTestFloatPtr(2), PeakRateMultiplier: priceTestFloatPtr(3)})
	require.True(t, quote.ImageRateIndependent)
	require.NotNil(t, quote.ImageRateMultiplier)
	require.InDelta(t, 0.5, *quote.ImageRateMultiplier, 1e-12)
	require.NotNil(t, quote.EffectiveRateMultiplier)
	require.InDelta(t, 0.5, *quote.EffectiveRateMultiplier, 1e-12)
	require.Equal(t, "image_independent", quote.RateSource)
	require.Nil(t, quote.PeakRateMultiplier)
}

func TestPriceQuoteService_MultipleMatchingGroupPricesAreExposedAsConditions(t *testing.T) {
	firstInput, firstOutput := 1e-6, 2e-6
	secondInput, secondOutput := 3e-6, 4e-6
	group := &Group{ModelPricing: []ChannelModelPricing{
		{Models: []string{"priced-model"}, InputPrice: &firstInput, OutputPrice: &firstOutput},
		{Models: []string{"priced-model"}, InputPrice: &secondInput, OutputPrice: &secondOutput},
	}}
	service := NewPriceQuoteService(NewModelPricingResolver(nil, newTestBillingServiceForResolver()), nil)
	quote := service.Quote(context.Background(), PriceQuoteInput{Model: "priced-model", GroupID: 1, Group: group})
	require.Len(t, quote.PriceConditions, 2)
	require.NotEqual(t, quote.PriceConditions[0].InputPerMillion, quote.PriceConditions[1].InputPerMillion)
}

func priceTestFloatPtr(value float64) *float64 {
	return &value
}
