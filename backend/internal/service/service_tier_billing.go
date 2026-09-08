package service

import (
	"log/slog"
	"strings"
)

// OpenAIFastTierUltrafast is the explicit Codex/API ultrafast service tier.
const OpenAIFastTierUltrafast = "ultrafast"

// ServiceTierBillingResolution records the requested, observed, and billable
// tiers for one request. A response may only lower the bill; it never promotes
// an untiered or cheaper request.
type ServiceTierBillingResolution struct {
	Requested  string
	Observed   string
	Billing    string
	Downgraded bool
}

// ResolveBillingServiceTier applies the official 0.2.0 response-tier contract
// without depending on gateway response DTOs.
func ResolveBillingServiceTier(requested, observed string) ServiceTierBillingResolution {
	requested = normalizeBillingServiceTier(requested)
	observed = normalizeBillingServiceTier(observed)
	resolution := ServiceTierBillingResolution{Requested: requested, Observed: observed, Billing: requested}
	if observed == "" || observed == requested {
		return resolution
	}
	observedRank, known := serviceTierCostRank(observed)
	if !known {
		return resolution
	}
	requestedRank, _ := serviceTierCostRank(requested)
	if observedRank >= requestedRank {
		return resolution
	}
	resolution.Billing = observed
	resolution.Downgraded = true
	return resolution
}

// ResolveOpenAIServiceTierBilling applies the response-tier contract for the
// selected credential. ChatGPT/Codex OAuth-like responses commonly echo
// "default" even when the outbound turn used a faster tier, so that one
// response value is observational rather than authoritative for billing.
func ResolveOpenAIServiceTierBilling(account *Account, requested, observed string) ServiceTierBillingResolution {
	if account != nil && account.IsOpenAIOAuthLike() && codexOAuthResponseTierIsNonAuthoritative(observed) {
		return ServiceTierBillingResolution{
			Requested: normalizeBillingServiceTier(requested),
			Observed:  normalizeBillingServiceTier(observed),
			Billing:   normalizeBillingServiceTier(requested),
		}
	}
	return ResolveBillingServiceTier(requested, observed)
}

func codexOAuthResponseTierIsNonAuthoritative(observed string) bool {
	return normalizeBillingServiceTier(observed) == "default"
}

func serviceTierCostRank(tier string) (int, bool) {
	switch normalizeBillingServiceTier(tier) {
	case "flex":
		return 0, true
	case "", "default", "standard", "auto", "scale":
		return 1, true
	case "priority", "fast":
		return 2, true
	default:
		return 1, false
	}
}

// CalculateCostWithObservedServiceTier keeps pricing and response-tier
// reconciliation in one service-level operation. Gateway adapters can call it
// once they have both requested and observed tier values.
func (s *BillingService) CalculateCostWithObservedServiceTier(model string, tokens UsageTokens, rateMultiplier float64, requested, observed string) (*CostBreakdown, ServiceTierBillingResolution, error) {
	resolution := ResolveBillingServiceTier(requested, observed)
	cost, err := s.CalculateCostWithServiceTier(model, tokens, rateMultiplier, resolution.Billing)
	return cost, resolution, err
}

// ApplyOpenAIServiceTierBillingResolution lowers the billable tier only when the
// selected credential's upstream response tier is authoritative.
func ApplyOpenAIServiceTierBillingResolution(account *Account, result *OpenAIForwardResult) ServiceTierBillingResolution {
	if result == nil {
		return ServiceTierBillingResolution{}
	}
	resolution := ResolveOpenAIServiceTierBilling(account, optionalStringValue(result.ServiceTier), result.UpstreamResponseServiceTier)
	if resolution.Downgraded {
		billing := resolution.Billing
		result.ServiceTier = &billing
	}
	return resolution
}

// ApplyForwardServiceTierBillingResolution is the generic ForwardResult counterpart.
func ApplyForwardServiceTierBillingResolution(result *ForwardResult) ServiceTierBillingResolution {
	if result == nil {
		return ServiceTierBillingResolution{}
	}
	resolution := ResolveBillingServiceTier(optionalStringValue(result.ServiceTier), result.UpstreamResponseServiceTier)
	if resolution.Downgraded {
		billing := resolution.Billing
		result.ServiceTier = &billing
	}
	return resolution
}

func logServiceTierBillingDowngrade(component string, account *Account, requestID string, resolution ServiceTierBillingResolution) {
	if !resolution.Downgraded {
		return
	}
	attrs := []any{"component", component, "request_id", strings.TrimSpace(requestID), "requested_tier", resolution.Requested, "response_tier", resolution.Observed, "billed_tier", resolution.Billing}
	if account != nil {
		attrs = append(attrs, "platform", account.Platform, "account_id", account.ID)
	}
	slog.Info("billing.service_tier_downgraded", attrs...)
}
