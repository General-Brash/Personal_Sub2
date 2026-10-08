package service

import "strings"

// SystemPricingMatch comes from the actual resolver result, not a second pricing
// lookup with looser equality. Exact means this requested ID has its own entry.
type SystemPricingMatch struct {
	Source    string
	ModelID   string
	MatchType string
	Exact     bool
}

func (s *PricingService) GetExactModelPricing(model string) *LiteLLMModelPricing {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pricingData[strings.ToLower(strings.TrimSpace(model))]
}

func (s *PricingService) GetModelPricingWithMatch(model string) (*LiteLLMModelPricing, SystemPricingMatch) {
	match := SystemPricingMatch{Source: "unavailable", MatchType: "none"}
	if s == nil {
		return nil, match
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	model = strings.ToLower(strings.TrimSpace(model))
	pricing := s.getModelPricingLocked(model)
	if pricing == nil {
		return nil, match
	}
	match.Source, match.ModelID = pricing.catalogSource, pricing.catalogModelID
	if match.Source == "" {
		match.Source = PricingSourceLiteLLM
		// Programmatic/test catalogs do not pass through the JSON parser.
		for key, p := range s.pricingData {
			if p == pricing && (match.ModelID == "" || key < match.ModelID) {
				match.ModelID = key
			}
		}
	}
	if match.ModelID == "" {
		match.Source = "code"
		switch pricing {
		case openAIGPT6AstraFallbackPricing:
			match.ModelID = "gpt-6-astra"
		case openAIGPT54FallbackPricing:
			match.ModelID = "gpt-5.4"
		case openAIGPT54MiniFallbackPricing:
			match.ModelID = "gpt-5.4-mini"
		case openAIGPT54NanoFallbackPricing:
			match.ModelID = "gpt-5.4-nano"
		case openAIGPT55FallbackPricing:
			match.ModelID = "gpt-5.5"
		case openAIGPT55ProFallbackPricing:
			match.ModelID = "gpt-5.5-pro"
		case openAIGPT56SolFallbackPricing:
			match.ModelID = "gpt-5.6-sol"
		case openAIGPT56TerraFallbackPricing:
			match.ModelID = "gpt-5.6-terra"
		case openAIGPT56LunaFallbackPricing:
			match.ModelID = "gpt-5.6-luna"
		case openAIGPTImage25FallbackPricing:
			match.ModelID = strings.TrimSuffix(model, "-2026-09-08")
		}
	}
	match.MatchType = "family"
	match.Exact = model == match.ModelID
	if match.Exact {
		match.MatchType = "exact"
	} else if s.lookupIdentifiedModelPricingLocked(s.buildModelLookupCandidates(model)) == pricing {
		match.MatchType = "alias"
	}
	return pricing, match
}
