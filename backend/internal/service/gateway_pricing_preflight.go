package service

import (
	"context"
	"strings"
)

func normalizedChannelMappedModel(mapping ChannelMappingResult, requestedModel string) string {
	if mapped := strings.TrimSpace(mapping.MappedModel); mapped != "" {
		return mapped
	}
	return strings.TrimSpace(requestedModel)
}

func gatewayBillingModelForPreflight(account *Account, mapping ChannelMappingResult, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	channelMappedModel := normalizedChannelMappedModel(mapping, requestedModel)
	switch mapping.BillingModelSource {
	case BillingModelSourceRequested:
		return requestedModel
	case BillingModelSourceUpstream:
		return resolveAccountUpstreamModel(account, channelMappedModel)
	default:
		return channelMappedModel
	}
}

func openAIBillingModelForPreflight(account *Account, mapping ChannelMappingResult, requestedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	channelMappedModel := normalizedChannelMappedModel(mapping, requestedModel)
	switch mapping.BillingModelSource {
	case BillingModelSourceRequested:
		return requestedModel
	case BillingModelSourceUpstream:
		forwardModel := resolveOpenAIForwardModel(account, channelMappedModel, "")
		return normalizeOpenAIModelForUpstream(account, forwardModel)
	default:
		return channelMappedModel
	}
}

func groupIDAndImagePricing(apiKey *APIKey) (*int64, *ImagePriceConfig) {
	if apiKey == nil {
		return nil, nil
	}
	return apiKey.GroupID, imagePriceConfigFromAPIKey(apiKey)
}

func groupIDAndVideoPricing(apiKey *APIKey) (*int64, *VideoPriceConfig) {
	if apiKey == nil {
		return nil, nil
	}
	return apiKey.GroupID, videoPriceConfigFromAPIKey(apiKey)
}

func (s *GatewayService) PreflightTokenRequestPricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult) error {
	model := gatewayBillingModelForPreflight(account, mapping, requestedModel)
	var groupID *int64
	if apiKey != nil {
		groupID = apiKey.GroupID
	}
	if s == nil || s.billingService == nil {
		return billingPricingUnavailable(model, "token")
	}
	return s.billingService.PreflightTokenPricing(ctx, model, groupID, s.resolver)
}

func (s *GatewayService) PreflightImageRequestPricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult, sizeTier string) error {
	model := gatewayBillingModelForPreflight(account, mapping, requestedModel)
	groupID, groupConfig := groupIDAndImagePricing(apiKey)
	if s == nil || s.billingService == nil {
		return billingPricingUnavailable(model, "image")
	}
	return s.billingService.PreflightImagePricing(ctx, model, sizeTier, groupID, groupConfig, s.resolver)
}

func (s *OpenAIGatewayService) PreflightTokenRequestPricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult) error {
	model := openAIBillingModelForPreflight(account, mapping, requestedModel)
	var groupID *int64
	if apiKey != nil {
		groupID = apiKey.GroupID
	}
	if s == nil || s.billingService == nil {
		return billingPricingUnavailable(model, "token")
	}
	return s.billingService.PreflightTokenPricing(ctx, model, groupID, s.resolver)
}

func (s *OpenAIGatewayService) PreflightImageRequestPricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult, sizeTier string) error {
	model := openAIBillingModelForPreflight(account, mapping, requestedModel)
	if s != nil {
		apiKey = s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey)
	}
	groupID, groupConfig := groupIDAndImagePricing(apiKey)
	if s == nil || s.billingService == nil {
		return billingPricingUnavailable(model, "image")
	}
	return s.billingService.PreflightImagePricing(ctx, model, sizeTier, groupID, groupConfig, s.resolver)
}

func (s *OpenAIGatewayService) PreflightVideoRequestPricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult, resolution string) error {
	model := openAIBillingModelForPreflight(account, mapping, requestedModel)
	if s != nil {
		apiKey = s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey)
	}
	groupID, groupConfig := groupIDAndVideoPricing(apiKey)
	if s == nil || s.billingService == nil {
		return billingPricingUnavailable(model, "video")
	}
	return s.billingService.PreflightVideoPricing(ctx, model, resolution, groupID, groupConfig, s.resolver)
}

func (s *OpenAIGatewayService) PreflightResponsesRequestPricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string, mapping ChannelMappingResult, body []byte) error {
	// A Responses request carrying the native image tool may still complete as a
	// text response (or fall back to text after the tool is not invoked). Validate
	// both possible billing branches before the upstream request starts.
	if err := s.PreflightTokenRequestPricing(ctx, apiKey, account, requestedModel, mapping); err != nil {
		return err
	}
	if !IsExplicitImageGenerationIntent(openAIResponsesEndpoint, requestedModel, body) {
		return nil
	}
	fallbackModel := openAIBillingModelForPreflight(account, mapping, requestedModel)
	imageConfig, err := resolveOpenAIResponsesImageBillingConfigDetailedFromBody(body, fallbackModel)
	if err != nil {
		return err
	}
	model := strings.TrimSpace(imageConfig.Model)
	if model == "" {
		model = fallbackModel
	}
	if s != nil {
		apiKey = s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey)
	}
	groupID, groupConfig := groupIDAndImagePricing(apiKey)
	if s == nil || s.billingService == nil {
		return billingPricingUnavailable(model, "image")
	}
	return s.billingService.PreflightImagePricing(ctx, model, imageConfig.SizeTier, groupID, groupConfig, s.resolver)
}

// PreflightMessagesRequestPricing validates the effective model used by the
// Anthropic /v1/messages compatibility bridge. The bridge applies channel
// mapping first, then account mapping, and finally the group's messages
// dispatch default; preflight must use that same order or it can approve a
// request whose effective upstream model has no price.
func (s *OpenAIGatewayService) PreflightMessagesRequestPricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel, defaultMappedModel string, mapping ChannelMappingResult) error {
	model := openAIMessagesBillingModelForPreflight(account, mapping, requestedModel, defaultMappedModel)
	var groupID *int64
	if apiKey != nil {
		groupID = apiKey.GroupID
	}
	if s == nil || s.billingService == nil {
		return billingPricingUnavailable(model, "token")
	}
	return s.billingService.PreflightTokenPricing(ctx, model, groupID, s.resolver)
}

func openAIMessagesBillingModelForPreflight(account *Account, mapping ChannelMappingResult, requestedModel, defaultMappedModel string) string {
	requestedModel = strings.TrimSpace(requestedModel)
	channelModel := normalizedChannelMappedModel(mapping, requestedModel)
	defaultMappedModel = strings.TrimSpace(defaultMappedModel)
	if mapping.BillingModelSource == BillingModelSourceRequested {
		return requestedModel
	}
	if mapping.BillingModelSource == BillingModelSourceChannelMapped && mapping.Mapped {
		return channelModel
	}
	effective := resolveOpenAIForwardModel(account, channelModel, defaultMappedModel)
	if mapping.BillingModelSource == BillingModelSourceUpstream {
		return normalizeOpenAIModelForUpstream(account, effective)
	}
	return effective
}
