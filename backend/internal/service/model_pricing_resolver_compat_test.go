package service

// Legacy pure-data fixture adapter; production uses intervalToModelPricingWithBase.
func intervalToModelPricing(iv *PricingInterval, supportsCacheBreakdown bool, chPricing *ChannelModelPricing) *ModelPricing {
	return intervalToModelPricingWithBase(iv, &ModelPricing{SupportsCacheBreakdown: supportsCacheBreakdown}, chPricing)
}
