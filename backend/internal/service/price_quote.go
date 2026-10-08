package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

type DynamicPriceFactorInput struct {
	Mode           string
	Model          string
	GroupID        int64
	UserID         int64
	At             time.Time
	SourceVersions map[string]string
}

// DynamicPriceFactor is intentionally explicit. A nil resolver result means
// the factor is not currently available; callers must not substitute 1.
type DynamicPriceFactor struct {
	Factor  float64           `json:"factor"`
	Source  string            `json:"source"`
	Version string            `json:"version,omitempty"`
	Details map[string]string `json:"details,omitempty"`
}

type DynamicPriceFactorResolver interface {
	ResolveDynamicPriceFactor(ctx context.Context, input DynamicPriceFactorInput) (*DynamicPriceFactor, error)
}

type PriceQuoteInput struct {
	Model                string
	GroupID              int64
	Group                *Group
	UserID               int64
	StaticRateMultiplier *float64
	UserRateMultiplier   *float64
	PeakRateMultiplier   *float64
	RateSource           string
	SourceVersions       map[string]string
}

type PriceQuoteInterval struct {
	MinTokens              int      `json:"min_tokens,omitempty"`
	MaxTokens              *int     `json:"max_tokens,omitempty"`
	TierLabel              string   `json:"tier_label,omitempty"`
	InputPerMillion        *float64 `json:"input_per_million,omitempty"`
	OutputPerMillion       *float64 `json:"output_per_million,omitempty"`
	CacheWritePerMillion   *float64 `json:"cache_write_per_million,omitempty"`
	CacheWrite1hPerMillion *float64 `json:"cache_write_1h_per_million,omitempty"`
	CacheReadPerMillion    *float64 `json:"cache_read_per_million,omitempty"`
	PerRequestPrice        *float64 `json:"per_request_price,omitempty"`
}

type PriceQuoteCondition struct {
	ImageInputPerMillion   *float64             `json:"image_input_per_million,omitempty"`
	ImageOutputPerMillion  *float64             `json:"image_output_per_million,omitempty"`
	Pattern                string               `json:"pattern"`
	Unit                   string               `json:"pricing_unit"`
	BillingMode            string               `json:"billing_mode,omitempty"`
	InputPerMillion        *float64             `json:"input_per_million,omitempty"`
	OutputPerMillion       *float64             `json:"output_per_million,omitempty"`
	CacheWritePerMillion   *float64             `json:"cache_write_per_million,omitempty"`
	CacheWrite1hPerMillion *float64             `json:"cache_write_1h_per_million,omitempty"`
	CacheReadPerMillion    *float64             `json:"cache_read_per_million,omitempty"`
	PerRequestPrice        *float64             `json:"per_request_price,omitempty"`
	Intervals              []PriceQuoteInterval `json:"intervals,omitempty"`
}

type ModelPriceQuote struct {
	ImageInputPerMillion    *float64              `json:"image_input_per_million,omitempty"`
	ImageOutputPerMillion   *float64              `json:"image_output_per_million,omitempty"`
	Currency                string                `json:"currency"`
	Unit                    string                `json:"pricing_unit"`
	InputPerMillion         *float64              `json:"input_per_million,omitempty"`
	OutputPerMillion        *float64              `json:"output_per_million,omitempty"`
	CacheWritePerMillion    *float64              `json:"cache_write_per_million,omitempty"`
	CacheWrite1hPerMillion  *float64              `json:"cache_write_1h_per_million,omitempty"`
	CacheReadPerMillion     *float64              `json:"cache_read_per_million,omitempty"`
	PerRequestPrice         *float64              `json:"per_request_price,omitempty"`
	Intervals               []PriceQuoteInterval  `json:"intervals,omitempty"`
	StaticRateMultiplier    *float64              `json:"static_rate_multiplier,omitempty"`
	UserRateMultiplier      *float64              `json:"user_rate_multiplier,omitempty"`
	PeakRateMultiplier      *float64              `json:"peak_rate_multiplier,omitempty"`
	EffectiveRateMultiplier *float64              `json:"effective_rate_multiplier,omitempty"`
	RateSource              string                `json:"rate_source,omitempty"`
	ImageRateIndependent    bool                  `json:"image_rate_independent,omitempty"`
	ImageRateMultiplier     *float64              `json:"image_rate_multiplier,omitempty"`
	ChannelTimeMultiplier   *float64              `json:"channel_time_multiplier,omitempty"`
	PriceConditions         []PriceQuoteCondition `json:"price_conditions,omitempty"`
	DynamicFactor           *DynamicPriceFactor   `json:"dynamic_factor,omitempty"`
	DynamicFactorStatus     string                `json:"dynamic_factor_status"`
	Source                  string                `json:"source"`
	SourceVersions          map[string]string     `json:"source_versions,omitempty"`
	UnknownFields           []string              `json:"unknown_fields,omitempty"`
	QuoteVersion            string                `json:"quote_version"`
	PricedAt                time.Time             `json:"priced_at"`
}

type PriceQuoteService struct {
	resolver *ModelPricingResolver
	dynamic  DynamicPriceFactorResolver
	now      func() time.Time
}

func NewPriceQuoteService(resolver *ModelPricingResolver, dynamic DynamicPriceFactorResolver) *PriceQuoteService {
	return &PriceQuoteService{resolver: resolver, dynamic: dynamic, now: time.Now}
}

func (s *PriceQuoteService) SetClock(now func() time.Time) {
	if s != nil && now != nil {
		s.now = now
	}
}

// Quote calls the existing ModelPricingResolver; it does not implement a
// second pricing rule. Missing prices stay absent rather than becoming zero.
func (s *PriceQuoteService) Quote(ctx context.Context, input PriceQuoteInput) *ModelPriceQuote {
	at := time.Now().UTC()
	if s != nil && s.now != nil {
		at = s.now().UTC()
	}
	quote := &ModelPriceQuote{
		Currency:            "USD",
		Unit:                "per_1m_tokens",
		DynamicFactorStatus: "not_configured",
		Source:              "unknown",
		SourceVersions:      cloneCatalogStringMap(input.SourceVersions),
		PricedAt:            at,
	}
	if s == nil || s.resolver == nil {
		quote.UnknownFields = []string{"pricing"}
		quote.QuoteVersion = priceQuoteVersion(quote, nil, input, nil)
		return quote
	}
	resolved := s.resolver.Resolve(ctx, PricingInput{Model: input.Model, GroupID: optionalGroupID(input.GroupID), Group: input.Group})
	if resolved == nil {
		quote.UnknownFields = []string{"pricing"}
		quote.QuoteVersion = priceQuoteVersion(quote, nil, input, nil)
		return quote
	}
	resolved = applyDedicatedMediaQuotePrices(input.Model, input.Group, resolved)
	quote.Source = resolved.Source
	revision := resolved.DefaultPricingRevision
	if revision == "" && resolved.BasePricing != nil {
		revision = resolved.BasePricing.DefaultPricingRevision
	}
	if revision != "" {
		if quote.SourceVersions == nil {
			quote.SourceVersions = map[string]string{}
		}
		quote.SourceVersions["admin_default_pricing"] = revision
	}
	quote.Unit = resolvedPricingQuoteUnit(resolved)
	if quote.Unit == "" {
		quote.Unit = "per_1m_tokens"
	}
	quote.InputPerMillion = perMillionPrice(baseInputPrice(resolved))
	quote.OutputPerMillion = perMillionPrice(baseOutputPrice(resolved))
	quote.ImageInputPerMillion = perMillionPrice(baseImageInputPrice(resolved))
	quote.ImageOutputPerMillion = perMillionPrice(baseImageOutputPrice(resolved))
	quote.CacheWritePerMillion = perMillionPrice(baseCacheWritePrice(resolved))
	quote.CacheWrite1hPerMillion = perMillionPrice(baseCacheWrite1hPrice(resolved))
	quote.CacheReadPerMillion = perMillionPrice(baseCacheReadPrice(resolved))
	if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo {
		quote.Unit = resolvedPricingQuoteUnit(resolved)
		if resolved.DefaultPerRequestPricePresent && validUnitPrice(resolved.DefaultPerRequestPrice) {
			value := resolved.DefaultPerRequestPrice
			quote.PerRequestPrice = &value
		}
		for _, tier := range resolved.RequestTiers {
			item := PriceQuoteInterval{TierLabel: tier.TierLabel, MinTokens: tier.MinTokens, MaxTokens: cloneCatalogInt(tier.MaxTokens)}
			if tier.PerRequestPrice != nil && validUnitPrice(*tier.PerRequestPrice) {
				value := *tier.PerRequestPrice
				item.PerRequestPrice = &value
			}
			quote.Intervals = append(quote.Intervals, item)
		}
	} else if len(resolved.Intervals) > 0 {
		quote.Unit = "per_1m_tokens"
		for _, tier := range resolved.Intervals {
			item := PriceQuoteInterval{TierLabel: tier.TierLabel, MinTokens: tier.MinTokens, MaxTokens: cloneCatalogInt(tier.MaxTokens)}
			item.InputPerMillion = perMillionPrice(tier.InputPrice)
			item.OutputPerMillion = perMillionPrice(tier.OutputPrice)
			item.CacheWritePerMillion = perMillionPrice(tier.CacheWritePrice)
			item.CacheWrite1hPerMillion = perMillionPrice(tier.CacheWrite1hPrice)
			item.CacheReadPerMillion = perMillionPrice(tier.CacheReadPrice)
			quote.Intervals = append(quote.Intervals, item)
		}
	}
	quote.StaticRateMultiplier = validMultiplier(input.StaticRateMultiplier)
	quote.UserRateMultiplier = validMultiplier(input.UserRateMultiplier)
	quote.PeakRateMultiplier = validMultiplier(input.PeakRateMultiplier)
	quote.RateSource = strings.TrimSpace(input.RateSource)
	quote.EffectiveRateMultiplier = quote.UserRateMultiplier
	if quote.EffectiveRateMultiplier == nil {
		quote.EffectiveRateMultiplier = quote.StaticRateMultiplier
	}
	if quote.RateSource == "" && quote.UserRateMultiplier != nil {
		quote.RateSource = "user_override"
	}
	if resolved.Mode == BillingModeImage && input.Group != nil && input.Group.ImageRateIndependent {
		if imageRate := validMultiplier(&input.Group.ImageRateMultiplier); imageRate != nil {
			quote.ImageRateIndependent = true
			quote.ImageRateMultiplier = imageRate
			quote.EffectiveRateMultiplier = imageRate
			quote.RateSource = "image_independent"
			quote.PeakRateMultiplier = nil
		}
	}
	if resolved.channelPricing != nil && resolved.channelPricing.TimePricing != nil {
		multiplier := resolvedChannelTimeMultiplier(resolved, at)
		if multiplier >= 0 && !math.IsNaN(multiplier) && !math.IsInf(multiplier, 0) {
			quote.ChannelTimeMultiplier = &multiplier
		}
	}
	quote.PriceConditions = s.collectGroupPriceConditions(ctx, input)
	quote.PriceConditions = append(quote.PriceConditions, defaultMediaGroupPriceConditions(input.Model, input.Group, resolved)...)
	if s.dynamic != nil {
		factor, err := s.dynamic.ResolveDynamicPriceFactor(ctx, DynamicPriceFactorInput{
			Mode: priceQuoteDynamicMode(resolved), Model: input.Model, GroupID: input.GroupID, UserID: input.UserID, At: at, SourceVersions: cloneCatalogStringMap(input.SourceVersions),
		})
		if err != nil {
			quote.DynamicFactorStatus = "unavailable"
		} else if factor == nil {
			quote.DynamicFactorStatus = "unknown"
		} else if !validPositivePrice(factor.Factor) {
			quote.DynamicFactorStatus = "invalid"
		} else {
			quote.DynamicFactor = factor
			quote.DynamicFactorStatus = "available"
		}
	}
	quote.UnknownFields = priceQuoteUnknownFields(quote)
	quote.QuoteVersion = priceQuoteVersion(quote, resolved, input, quote.DynamicFactor)
	return quote
}

func (s *PriceQuoteService) collectGroupPriceConditions(ctx context.Context, input PriceQuoteInput) []PriceQuoteCondition {
	if s == nil || s.resolver == nil || input.Group == nil || len(input.Group.ModelPricing) == 0 {
		return nil
	}
	normalizedModel := normalizeChannelPricingModelName(input.Model)
	type matchedPrice struct {
		entry   ChannelModelPricing
		pattern string
	}
	exact := make([]matchedPrice, 0)
	wildcard := make([]matchedPrice, 0)
	for i := range input.Group.ModelPricing {
		entry := input.Group.ModelPricing[i]
		for _, pattern := range entry.Models {
			normalizedPattern := normalizeChannelPricingModelName(pattern)
			if normalizedPattern == normalizedModel {
				exact = append(exact, matchedPrice{entry: entry.Clone(), pattern: pattern})
				break
			}
			if strings.HasSuffix(normalizedPattern, "*") && strings.HasPrefix(normalizedModel, strings.TrimSuffix(normalizedPattern, "*")) {
				wildcard = append(wildcard, matchedPrice{entry: entry.Clone(), pattern: pattern})
				break
			}
		}
	}
	matched := exact
	if len(matched) == 0 {
		matched = wildcard
	}
	if len(matched) < 2 {
		return nil
	}
	conditions := make([]PriceQuoteCondition, 0, len(matched))
	seen := make(map[string]struct{}, len(matched))
	for _, item := range matched {
		group := *input.Group
		group.ModelPricing = []ChannelModelPricing{item.entry}
		resolved := s.resolver.Resolve(ctx, PricingInput{Model: input.Model, GroupID: optionalGroupID(input.GroupID), Group: &group})
		if resolved == nil {
			continue
		}
		condition := priceQuoteConditionFromResolved(item.pattern, resolved)
		raw, err := json.Marshal(condition)
		if err != nil {
			continue
		}
		key := string(raw)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		conditions = append(conditions, condition)
	}
	if len(conditions) < 2 {
		return nil
	}
	return conditions
}

func priceQuoteConditionFromResolved(pattern string, resolved *ResolvedPricing) PriceQuoteCondition {
	condition := PriceQuoteCondition{Pattern: strings.TrimSpace(pattern), Unit: resolvedPricingQuoteUnit(resolved), BillingMode: string(resolved.Mode)}
	if condition.Unit == "" {
		condition.Unit = "per_1m_tokens"
	}
	condition.InputPerMillion = perMillionPrice(baseInputPrice(resolved))
	condition.OutputPerMillion = perMillionPrice(baseOutputPrice(resolved))
	condition.ImageInputPerMillion = perMillionPrice(baseImageInputPrice(resolved))
	condition.ImageOutputPerMillion = perMillionPrice(baseImageOutputPrice(resolved))
	condition.CacheWritePerMillion = perMillionPrice(baseCacheWritePrice(resolved))
	condition.CacheWrite1hPerMillion = perMillionPrice(baseCacheWrite1hPrice(resolved))
	condition.CacheReadPerMillion = perMillionPrice(baseCacheReadPrice(resolved))
	if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeImage || resolved.Mode == BillingModeVideo {
		condition.Unit = resolvedPricingQuoteUnit(resolved)
		if resolved.DefaultPerRequestPricePresent && validUnitPrice(resolved.DefaultPerRequestPrice) {
			value := resolved.DefaultPerRequestPrice
			condition.PerRequestPrice = &value
		}
		for _, tier := range resolved.RequestTiers {
			item := PriceQuoteInterval{TierLabel: tier.TierLabel, MinTokens: tier.MinTokens, MaxTokens: cloneCatalogInt(tier.MaxTokens)}
			if tier.PerRequestPrice != nil && validUnitPrice(*tier.PerRequestPrice) {
				value := *tier.PerRequestPrice
				item.PerRequestPrice = &value
			}
			condition.Intervals = append(condition.Intervals, item)
		}
	} else if len(resolved.Intervals) > 0 {
		for _, tier := range resolved.Intervals {
			item := PriceQuoteInterval{TierLabel: tier.TierLabel, MinTokens: tier.MinTokens, MaxTokens: cloneCatalogInt(tier.MaxTokens)}
			item.InputPerMillion = perMillionPrice(tier.InputPrice)
			item.OutputPerMillion = perMillionPrice(tier.OutputPrice)
			item.CacheWritePerMillion = perMillionPrice(tier.CacheWritePrice)
			item.CacheWrite1hPerMillion = perMillionPrice(tier.CacheWrite1hPrice)
			item.CacheReadPerMillion = perMillionPrice(tier.CacheReadPrice)
			condition.Intervals = append(condition.Intervals, item)
		}
	}
	return condition
}

func optionalGroupID(groupID int64) *int64 {
	if groupID <= 0 {
		return nil
	}
	value := groupID
	return &value
}

func baseInputPrice(resolved *ResolvedPricing) *float64 {
	if resolved == nil || resolved.BasePricing == nil {
		return nil
	}
	if resolved.BasePricing.InputPricePresent || resolved.BasePricing.InputPricePerToken > 0 {
		value := resolved.BasePricing.InputPricePerToken
		return &value
	}
	return nil
}

func baseOutputPrice(resolved *ResolvedPricing) *float64 {
	if resolved == nil || resolved.BasePricing == nil {
		return nil
	}
	if resolved.BasePricing.OutputPricePresent || resolved.BasePricing.OutputPricePerToken > 0 {
		value := resolved.BasePricing.OutputPricePerToken
		return &value
	}
	return nil
}

func baseCacheWritePrice(resolved *ResolvedPricing) *float64 {
	if resolved == nil || resolved.BasePricing == nil {
		return nil
	}
	value := resolved.BasePricing.CacheCreationPricePerToken
	if value <= 0 && !resolved.BasePricing.CacheCreationPriceExplicit {
		value = resolved.BasePricing.CacheCreation5mPrice
	}
	if value <= 0 && !resolved.BasePricing.CacheCreationPriceExplicit {
		return nil
	}
	return &value
}

func baseCacheWrite1hPrice(resolved *ResolvedPricing) *float64 {
	if resolved == nil || resolved.BasePricing == nil {
		return nil
	}
	value := resolved.BasePricing.CacheCreation1hPrice
	if value <= 0 && !resolved.BasePricing.CacheCreation1hPriceExplicit {
		return nil
	}
	return &value
}

func baseCacheReadPrice(resolved *ResolvedPricing) *float64 {
	if resolved == nil || resolved.BasePricing == nil || (resolved.BasePricing.CacheReadPricePerToken <= 0 && !resolved.BasePricing.CacheReadPriceExplicit) {
		return nil
	}
	value := resolved.BasePricing.CacheReadPricePerToken
	return &value
}

func perMillionPrice(value *float64) *float64 {
	if value == nil || !validUnitPrice(*value) {
		return nil
	}
	out := *value * 1_000_000
	if !validUnitPrice(out) {
		return nil
	}
	return &out
}

func validPositivePrice(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func validMultiplier(value *float64) *float64 {
	if value == nil || *value < 0 || math.IsNaN(*value) || math.IsInf(*value, 0) {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func priceQuoteUnknownFields(quote *ModelPriceQuote) []string {
	if quote == nil {
		return []string{"pricing"}
	}
	fields := []string{}
	if quote.Unit != "per_1m_tokens" {
		if quote.PerRequestPrice == nil && len(quote.Intervals) == 0 {
			fields = append(fields, "per_request_price")
		}
		return fields
	}
	if quote.InputPerMillion == nil {
		fields = append(fields, "input_per_million")
	}
	if quote.OutputPerMillion == nil {
		fields = append(fields, "output_per_million")
	}
	if quote.CacheWritePerMillion == nil {
		fields = append(fields, "cache_write_per_million")
	}
	if quote.CacheReadPerMillion == nil {
		fields = append(fields, "cache_read_per_million")
	}
	if quote.Unit == "per_request" && quote.PerRequestPrice == nil && len(quote.Intervals) == 0 {
		fields = append(fields, "per_request_price")
	}
	return fields
}

func priceQuoteVersion(quote *ModelPriceQuote, resolved *ResolvedPricing, input PriceQuoteInput, factor *DynamicPriceFactor) string {
	payload := struct {
		ImageInput       *float64              `json:"image_input,omitempty"`
		ImageOutput      *float64              `json:"image_output,omitempty"`
		Model            string                `json:"model"`
		GroupID          int64                 `json:"group_id"`
		Source           string                `json:"source"`
		Unit             string                `json:"unit"`
		Input            *float64              `json:"input"`
		Output           *float64              `json:"output"`
		CacheWrite       *float64              `json:"cache_write"`
		CacheWrite1h     *float64              `json:"cache_write_1h"`
		CacheRead        *float64              `json:"cache_read"`
		PerRequest       *float64              `json:"per_request"`
		Intervals        []PriceQuoteInterval  `json:"intervals,omitempty"`
		Static           *float64              `json:"static"`
		User             *float64              `json:"user"`
		Peak             *float64              `json:"peak"`
		Effective        *float64              `json:"effective"`
		RateSource       string                `json:"rate_source,omitempty"`
		ImageIndependent bool                  `json:"image_independent,omitempty"`
		ImageRate        *float64              `json:"image_rate,omitempty"`
		ChannelTime      *float64              `json:"channel_time,omitempty"`
		Conditions       []PriceQuoteCondition `json:"conditions,omitempty"`
		DynamicFactor    *float64              `json:"dynamic_factor,omitempty"`
		DynamicVersion   string                `json:"dynamic_version,omitempty"`
		SourceVersions   map[string]string     `json:"source_versions,omitempty"`
		PricingSourceID  string                `json:"pricing_source_id,omitempty"`
		PricingUpdatedAt string                `json:"pricing_updated_at,omitempty"`
	}{
		Model: strings.ToLower(strings.TrimSpace(input.Model)), GroupID: input.GroupID, Source: quote.Source, Unit: quote.Unit,
		ImageInput: quote.ImageInputPerMillion, ImageOutput: quote.ImageOutputPerMillion,
		Input: quote.InputPerMillion, Output: quote.OutputPerMillion, CacheWrite: quote.CacheWritePerMillion,
		CacheWrite1h: quote.CacheWrite1hPerMillion, CacheRead: quote.CacheReadPerMillion, PerRequest: quote.PerRequestPrice,
		Intervals: quote.Intervals, Static: quote.StaticRateMultiplier, User: quote.UserRateMultiplier, Peak: quote.PeakRateMultiplier,
		Effective: quote.EffectiveRateMultiplier, RateSource: quote.RateSource, ImageIndependent: quote.ImageRateIndependent,
		ImageRate: quote.ImageRateMultiplier, ChannelTime: quote.ChannelTimeMultiplier, Conditions: quote.PriceConditions,
		SourceVersions: cloneCatalogStringMap(quote.SourceVersions),
	}
	if factor != nil {
		factorValue := factor.Factor
		payload.DynamicFactor = &factorValue
		payload.DynamicVersion = factor.Version
	}
	if resolved != nil && resolved.channelPricing != nil {
		payload.PricingSourceID = fmt.Sprintf("%d:%d", resolved.channelPricing.ChannelID, resolved.channelPricing.ID)
		if !resolved.channelPricing.UpdatedAt.IsZero() {
			payload.PricingUpdatedAt = resolved.channelPricing.UpdatedAt.UTC().Format(time.RFC3339Nano)
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "sha256:unavailable"
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func priceQuoteDynamicMode(resolved *ResolvedPricing) string {
	if resolved != nil {
		switch resolved.Mode {
		case BillingModeImage:
			return DynamicRateModeImage
		case BillingModeVideo:
			return DynamicRateModeVideo
		}
	}
	return DynamicRateModeText
}

func validUnitPrice(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func resolvedPricingQuoteUnit(resolved *ResolvedPricing) string {
	if resolved.Unit != "" {
		return resolved.Unit
	}
	if resolved.Mode == BillingModeImage {
		return "per_image"
	}
	if resolved.Mode == BillingModePerRequest || resolved.Mode == BillingModeVideo {
		return "per_request"
	}
	return "per_1m_tokens"
}

// The existing dedicated media group prices precede channel/default prices.
// Overlay only configured tiers; missing tiers keep the resolved default unit.
func applyDedicatedMediaQuotePrices(model string, group *Group, resolved *ResolvedPricing) *ResolvedPricing {
	if group == nil || resolved == nil || resolved.Source == PricingSourceGroup {
		return resolved
	}
	var labels []string
	var prices []*float64
	switch resolved.Mode {
	case BillingModeImage:
		labels = []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K}
		prices = []*float64{group.ImagePrice1K, group.ImagePrice2K, group.ImagePrice4K}
	case BillingModeVideo:
		labels = []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P}
		for _, label := range labels {
			prices = append(prices, group.GetVideoPriceForModel(model, label))
		}
	default:
		return resolved
	}
	present := false
	for _, price := range prices {
		present = present || price != nil
	}
	if !present {
		return resolved
	}
	clone := *resolved
	clone.Source = PricingSourceGroup
	clone.Unit = defaultPricingUnit(resolved.Mode)
	clone.RequestTiers = nil
	for i, label := range labels {
		price := prices[i]
		if price == nil {
			for _, tier := range resolved.RequestTiers {
				if strings.EqualFold(tier.TierLabel, label) {
					price = tier.PerRequestPrice
					break
				}
			}
			if price == nil && resolved.DefaultPerRequestPricePresent {
				p := resolved.DefaultPerRequestPrice
				price = &p
			}
		}
		if price != nil {
			clone.RequestTiers = append(clone.RequestTiers, PricingInterval{TierLabel: label, PerRequestPrice: price})
		}
		if i == 0 {
			clone.DefaultPerRequestPricePresent = price != nil
			if price != nil {
				clone.DefaultPerRequestPrice = *price
			}
		}
	}
	return &clone
}

// A token/request default may also be invoked through a media endpoint. Dedicated
// group media prices then use a different unit, so show conditional base prices
// instead of folding USD/image or USD/second into the token/request quote.
func defaultMediaGroupPriceConditions(model string, group *Group, resolved *ResolvedPricing) []PriceQuoteCondition {
	if group == nil || resolved == nil || resolved.Source != PricingSourceAdmin || (resolved.Mode != BillingModeToken && resolved.Mode != BillingModePerRequest) {
		return nil
	}
	conditions := []PriceQuoteCondition{}
	for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
		if price := group.GetImagePrice(tier); price != nil && validUnitPrice(*price) {
			conditions = append(conditions, PriceQuoteCondition{Pattern: "image_generation:" + tier, Unit: "per_image", BillingMode: string(BillingModeImage), PerRequestPrice: price})
		}
	}
	for _, tier := range []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P} {
		if price := group.GetVideoPriceForModel(model, tier); price != nil && validUnitPrice(*price) {
			conditions = append(conditions, PriceQuoteCondition{Pattern: "video_generation:" + tier, Unit: "per_second", BillingMode: string(BillingModeVideo), PerRequestPrice: price})
		}
	}
	return conditions
}

func baseImageInputPrice(resolved *ResolvedPricing) *float64 {
	if resolved == nil || resolved.BasePricing == nil {
		return nil
	}
	p := resolved.BasePricing
	return presentDefaultPrice(p.ImageInputPricePerToken, p.ImageInputPriceExplicit)
}
func baseImageOutputPrice(resolved *ResolvedPricing) *float64 {
	if resolved == nil || resolved.BasePricing == nil {
		return nil
	}
	p := resolved.BasePricing
	return presentDefaultPrice(p.ImageOutputPricePerToken, p.ImageOutputPriceExplicit)
}
