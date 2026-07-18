package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const maxBillingUnitPriceUSD = 1_000_000

var (
	ErrBillingPricingUnavailable = infraerrors.New(
		http.StatusServiceUnavailable,
		"BILLING_PRICING_UNAVAILABLE",
		"billing pricing is unavailable for the requested model",
	)
	ErrBillingPricingInvalid = infraerrors.New(
		http.StatusServiceUnavailable,
		"BILLING_PRICING_INVALID",
		"billing pricing is invalid for the requested model",
	)
)

func billingPricingUnavailable(model, kind string) error {
	cause := fmt.Errorf("%w for model: %s", ErrModelPricingUnavailable, strings.TrimSpace(model))
	return ErrBillingPricingUnavailable.WithMetadata(map[string]string{
		"model": strings.TrimSpace(model),
		"kind":  strings.TrimSpace(kind),
	}).WithCause(cause)
}

func billingPricingInvalid(model, kind, field string, value float64) error {
	return ErrBillingPricingInvalid.WithMetadata(map[string]string{
		"model": strings.TrimSpace(model),
		"kind":  strings.TrimSpace(kind),
		"field": strings.TrimSpace(field),
	}).WithCause(fmt.Errorf("invalid billing price %s=%v", field, value))
}

func validateBillingPrice(field string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > maxBillingUnitPriceUSD {
		return billingPricingInvalid("", "price", field, value)
	}
	return nil
}

func validateBillingPriceFor(model, kind, field string, value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > maxBillingUnitPriceUSD {
		return billingPricingInvalid(model, kind, field, value)
	}
	return nil
}

func validateLiteLLMRawEntry(model string, entry *LiteLLMRawEntry) error {
	if entry == nil {
		return billingPricingInvalid(model, "remote", "entry", math.NaN())
	}
	prices := []struct {
		name  string
		value *float64
	}{
		{"input_cost_per_token", entry.InputCostPerToken},
		{"input_cost_per_token_priority", entry.InputCostPerTokenPriority},
		{"output_cost_per_token", entry.OutputCostPerToken},
		{"output_cost_per_token_priority", entry.OutputCostPerTokenPriority},
		{"cache_creation_input_token_cost", entry.CacheCreationInputTokenCost},
		{"cache_creation_input_token_cost_priority", entry.CacheCreationInputTokenCostPriority},
		{"cache_creation_input_token_cost_above_1hr", entry.CacheCreationInputTokenCostAbove1hr},
		{"cache_read_input_token_cost", entry.CacheReadInputTokenCost},
		{"cache_read_input_token_cost_priority", entry.CacheReadInputTokenCostPriority},
		{"output_cost_per_image", entry.OutputCostPerImage},
		{"output_cost_per_image_token", entry.OutputCostPerImageToken},
		{"input_cost_per_image_token", entry.InputCostPerImageToken},
	}
	for _, price := range prices {
		if price.value == nil {
			continue
		}
		if err := validateBillingPriceFor(model, "remote", price.name, *price.value); err != nil {
			return err
		}
	}
	multipliers := []struct {
		name  string
		value *float64
	}{
		{"long_context_input_cost_multiplier", entry.LongContextInputCostMultiplier},
		{"long_context_output_cost_multiplier", entry.LongContextOutputCostMultiplier},
	}
	for _, multiplier := range multipliers {
		if multiplier.value == nil {
			continue
		}
		value := *multiplier.value
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > maxBillingUnitPriceUSD {
			return billingPricingInvalid(model, "remote", multiplier.name, value)
		}
	}
	if entry.LongContextInputTokenThreshold != nil && *entry.LongContextInputTokenThreshold < 0 {
		return billingPricingInvalid(model, "remote", "long_context_input_token_threshold", float64(*entry.LongContextInputTokenThreshold))
	}
	return nil
}

func modelPricingInputPresent(pricing *ModelPricing) bool {
	return pricing != nil && (pricing.InputPricePresent || pricing.InputPricePerToken != 0)
}

func modelPricingOutputPresent(pricing *ModelPricing) bool {
	return pricing != nil && (pricing.OutputPricePresent || pricing.OutputPricePerToken != 0)
}

func validateTokenModelPricing(model string, pricing *ModelPricing) error {
	if pricing == nil {
		return billingPricingUnavailable(model, "token")
	}
	prices := []struct {
		name  string
		value float64
	}{
		{"input_price", pricing.InputPricePerToken},
		{"input_priority_price", pricing.InputPricePerTokenPriority},
		{"image_input_price", pricing.ImageInputPricePerToken},
		{"output_price", pricing.OutputPricePerToken},
		{"output_priority_price", pricing.OutputPricePerTokenPriority},
		{"cache_write_price", pricing.CacheCreationPricePerToken},
		{"cache_write_priority_price", pricing.CacheCreationPricePerTokenPriority},
		{"cache_read_price", pricing.CacheReadPricePerToken},
		{"cache_read_priority_price", pricing.CacheReadPricePerTokenPriority},
		{"cache_write_5m_price", pricing.CacheCreation5mPrice},
		{"cache_write_1h_price", pricing.CacheCreation1hPrice},
		{"image_output_price", pricing.ImageOutputPricePerToken},
	}
	for _, price := range prices {
		if err := validateBillingPriceFor(model, "token", price.name, price.value); err != nil {
			return err
		}
	}
	if !modelPricingInputPresent(pricing) || !modelPricingOutputPresent(pricing) {
		return billingPricingUnavailable(model, "token")
	}
	return nil
}

// PreflightTokenPricing proves that every pricing branch reachable by token
// billing has explicit, finite input and output prices before upstream I/O.
func (s *BillingService) PreflightTokenPricing(ctx context.Context, model string, groupID *int64, resolver *ModelPricingResolver) error {
	model = strings.TrimSpace(model)
	if model == "" || s == nil {
		return billingPricingUnavailable(model, "token")
	}
	if resolver == nil {
		pricing, err := s.GetModelPricing(model)
		if err != nil {
			return billingPricingUnavailable(model, "token")
		}
		return validateTokenModelPricing(model, pricing)
	}

	resolved := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
	if resolved == nil {
		return billingPricingUnavailable(model, "token")
	}
	switch resolved.Mode {
	case BillingModePerRequest, BillingModeImage:
		return validateResolvedPerRequestPricing(model, resolved)
	case BillingModeToken, "":
		// A complete interval table is self-contained: GetIntervalPricing will
		// never fall back to BasePricing for any positive token count. This is
		// valid for channel-only custom models that have no global/base entry.
		if !tokenPricingIntervalsCoverPositiveTokens(resolved.Intervals) {
			if err := validateTokenModelPricing(model, resolved.BasePricing); err != nil {
				return err
			}
		}
		for i := range resolved.Intervals {
			pricing := intervalToModelPricing(&resolved.Intervals[i], resolved.SupportsCacheBreakdown, resolved.channelPricing)
			if err := validateTokenModelPricing(model, pricing); err != nil {
				return err
			}
		}
		return nil
	default:
		return billingPricingInvalid(model, "token", "billing_mode", math.NaN())
	}
}

func tokenPricingIntervalsCoverPositiveTokens(intervals []PricingInterval) bool {
	priced := make([]PricingInterval, 0, len(intervals))
	for _, interval := range intervals {
		if interval.InputPrice != nil && interval.OutputPrice != nil {
			priced = append(priced, interval)
		}
	}
	return pricingIntervalsCoverPositiveTokens(priced)
}

func validateResolvedPerRequestPricing(model string, resolved *ResolvedPricing) error {
	if resolved == nil {
		return billingPricingUnavailable(model, "per_request")
	}
	if resolved.DefaultPerRequestPricePresent {
		if err := validateBillingPriceFor(model, "per_request", "default_per_request_price", resolved.DefaultPerRequestPrice); err != nil {
			return err
		}
	}
	for _, tier := range resolved.RequestTiers {
		if tier.PerRequestPrice == nil {
			continue
		}
		if err := validateBillingPriceFor(model, "per_request", "tier_per_request_price", *tier.PerRequestPrice); err != nil {
			return err
		}
	}
	if resolved.DefaultPerRequestPricePresent || requestPricingIntervalsCoverPositiveTokens(resolved.RequestTiers) {
		return nil
	}
	return billingPricingUnavailable(model, "per_request")
}

func requestPricingIntervalsCoverPositiveTokens(intervals []PricingInterval) bool {
	priced := make([]PricingInterval, 0, len(intervals))
	for _, interval := range intervals {
		if interval.PerRequestPrice != nil {
			priced = append(priced, interval)
		}
	}
	return pricingIntervalsCoverPositiveTokens(priced)
}

func pricingIntervalsCoverPositiveTokens(priced []PricingInterval) bool {
	if len(priced) == 0 {
		return false
	}
	sort.Slice(priced, func(i, j int) bool { return priced[i].MinTokens < priced[j].MinTokens })
	if priced[0].MinTokens != 0 {
		return false
	}
	for i := range priced {
		if priced[i].MaxTokens == nil {
			return true
		}
		if i+1 >= len(priced) || priced[i+1].MinTokens != *priced[i].MaxTokens {
			return false
		}
	}
	return false
}

func selectedImageGroupPrice(config *ImagePriceConfig, sizeTier string) *float64 {
	if config == nil {
		return nil
	}
	switch NormalizeImageBillingTierOrDefault(sizeTier) {
	case ImageBillingSize1K:
		return config.Price1K
	case ImageBillingSize4K:
		return config.Price4K
	default:
		return config.Price2K
	}
}

func selectedVideoGroupPrice(config *VideoPriceConfig, resolution string) *float64 {
	if config == nil {
		return nil
	}
	switch NormalizeVideoBillingResolutionOrDefault(resolution) {
	case VideoBillingResolution720P:
		return config.Price720P
	case VideoBillingResolution1080P:
		return config.Price1080P
	default:
		return config.Price480P
	}
}

func resolvedTierPrice(resolver *ModelPricingResolver, resolved *ResolvedPricing, tier string) (float64, bool) {
	if resolver == nil || resolved == nil {
		return 0, false
	}
	if price, ok := resolver.FindRequestTierPrice(resolved, tier); ok {
		return price, true
	}
	if resolved.DefaultPerRequestPricePresent {
		return resolved.DefaultPerRequestPrice, true
	}
	return 0, false
}

func (s *BillingService) PreflightImagePricing(ctx context.Context, model, sizeTier string, groupID *int64, groupConfig *ImagePriceConfig, resolver *ModelPricingResolver) error {
	model = strings.TrimSpace(model)
	sizeTier = NormalizeImageBillingTierOrDefault(sizeTier)
	if configured := selectedImageGroupPrice(groupConfig, sizeTier); configured != nil {
		return validateBillingPriceFor(model, "image", "group_image_price", *configured)
	}
	if resolver != nil {
		resolved := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
		if resolved != nil && resolved.Source == PricingSourceChannel {
			if resolved.Mode == BillingModeToken || resolved.Mode == "" {
				return s.PreflightTokenPricing(ctx, model, groupID, resolver)
			}
			if price, ok := resolvedTierPrice(resolver, resolved, sizeTier); ok {
				return validateBillingPriceFor(model, "image", "channel_image_price", price)
			}
			return billingPricingUnavailable(model, "image")
		}
	}
	_, err := s.getDefaultImagePriceChecked(model, sizeTier)
	return err
}

func (s *BillingService) PreflightVideoPricing(ctx context.Context, model, resolution string, groupID *int64, groupConfig *VideoPriceConfig, resolver *ModelPricingResolver) error {
	model = strings.TrimSpace(model)
	resolution = NormalizeVideoBillingResolutionOrDefault(resolution)
	if configured := selectedVideoGroupPrice(groupConfig, resolution); configured != nil {
		return validateBillingPriceFor(model, "video", "group_video_price", *configured)
	}
	if resolver != nil {
		resolved := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
		if resolved != nil && resolved.Source == PricingSourceChannel {
			if resolved.Mode == BillingModeToken || resolved.Mode == "" {
				return s.PreflightTokenPricing(ctx, model, groupID, resolver)
			}
			if price, ok := resolvedTierPrice(resolver, resolved, resolution); ok {
				return validateBillingPriceFor(model, "video", "channel_video_price", price)
			}
			return billingPricingUnavailable(model, "video")
		}
	}
	_, err := s.getDefaultVideoPriceChecked(model, resolution)
	return err
}

func (s *BillingService) getDefaultImagePriceChecked(model, imageSize string) (float64, error) {
	if price, ok := getDefaultGrokImagineImagePrice(model, imageSize); ok {
		return price, validateBillingPriceFor(model, "image", "default_image_price", price)
	}
	if s == nil || s.pricingService == nil {
		return 0, billingPricingUnavailable(model, "image")
	}
	pricing := s.pricingService.GetModelPricing(model)
	if pricing == nil || (!pricing.OutputCostPerImagePresent && pricing.OutputCostPerImage == 0) {
		return 0, billingPricingUnavailable(model, "image")
	}
	basePrice := pricing.OutputCostPerImage
	if err := validateBillingPriceFor(model, "image", "output_cost_per_image", basePrice); err != nil {
		return 0, err
	}
	switch NormalizeImageBillingTierOrDefault(imageSize) {
	case ImageBillingSize2K:
		basePrice *= 1.5
	case ImageBillingSize4K:
		basePrice *= 2
	}
	if err := validateBillingPriceFor(model, "image", "sized_output_cost_per_image", basePrice); err != nil {
		return 0, err
	}
	return basePrice, nil
}

func (s *BillingService) getDefaultVideoPriceChecked(model, resolution string) (float64, error) {
	if price, ok := getDefaultGrokImagineVideoPrice(model, resolution); ok {
		return price, validateBillingPriceFor(model, "video", "default_video_price", price)
	}
	return 0, billingPricingUnavailable(model, "video")
}
