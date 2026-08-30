package service

import "math"

// isFinitePositiveBillingMultiplier accepts only the multiplier values that are
// safe to apply to a configured price. Unlike account/group rate multipliers,
// channel tier/interval multipliers never use zero as a valid free-price value;
// an explicit free price must be represented by the corresponding price field.
func isFinitePositiveBillingMultiplier(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value > 0
}

func validateModelPricingMultipliers(model, kind string, pricing *ModelPricing) error {
	if pricing == nil {
		return nil
	}
	for _, multiplier := range []struct {
		field string
		value *float64
	}{
		{field: "fast_multiplier", value: pricing.FastMultiplier},
		{field: "flex_multiplier", value: pricing.FlexMultiplier},
	} {
		if multiplier.value == nil {
			continue
		}
		if !isFinitePositiveBillingMultiplier(*multiplier.value) {
			return billingPricingInvalid(model, kind, multiplier.field, *multiplier.value)
		}
	}
	return nil
}

func validateChannelPricingMultipliers(model string, pricing *ChannelModelPricing) error {
	if pricing == nil {
		return nil
	}
	for _, multiplier := range []struct {
		field string
		value *float64
	}{
		{field: "fast_multiplier", value: pricing.FastMultiplier},
		{field: "flex_multiplier", value: pricing.FlexMultiplier},
	} {
		if multiplier.value == nil {
			continue
		}
		if !isFinitePositiveBillingMultiplier(*multiplier.value) {
			return billingPricingInvalid(model, "channel", multiplier.field, *multiplier.value)
		}
	}
	for i := range pricing.Intervals {
		if err := validatePricingIntervalMultipliers(model, "channel", &pricing.Intervals[i]); err != nil {
			return err
		}
	}
	return nil
}

func validatePricingIntervalMultipliers(model, kind string, interval *PricingInterval) error {
	if interval == nil {
		return nil
	}
	for _, multiplier := range []struct {
		field string
		value *float64
	}{
		{field: "input_multiplier", value: interval.InputMultiplier},
		{field: "output_multiplier", value: interval.OutputMultiplier},
		{field: "cache_write_multiplier", value: interval.CacheWriteMultiplier},
		{field: "cache_read_multiplier", value: interval.CacheReadMultiplier},
	} {
		if multiplier.value == nil {
			continue
		}
		if !isFinitePositiveBillingMultiplier(*multiplier.value) {
			return billingPricingInvalid(model, kind, multiplier.field, *multiplier.value)
		}
	}
	return nil
}

func validateResolvedPricingMultipliers(model string, resolved *ResolvedPricing) error {
	if resolved == nil {
		return nil
	}
	if err := validateModelPricingMultipliers(model, "resolved", resolved.BasePricing); err != nil {
		return err
	}
	if err := validateChannelPricingMultipliers(model, resolved.channelPricing); err != nil {
		return err
	}
	for i := range resolved.Intervals {
		if err := validatePricingIntervalMultipliers(model, "resolved", &resolved.Intervals[i]); err != nil {
			return err
		}
	}
	for i := range resolved.RequestTiers {
		if err := validatePricingIntervalMultipliers(model, "resolved", &resolved.RequestTiers[i]); err != nil {
			return err
		}
	}
	return nil
}
