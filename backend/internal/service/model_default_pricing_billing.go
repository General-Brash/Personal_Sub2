package service

import (
	"context"
	"strconv"
	"strings"
)

const PricingSourceAdmin = "admin_default"

// DefaultPricingBaseline describes the actual system lookup, without the DB layer.
type DefaultPricingBaseline struct {
	tokenPricing     *ModelPricing        // immutable system lookup reused within one resolution
	Fields           DefaultPricingFields `json:"prices"`
	Source           string               `json:"source"`
	MatchedModelID   string               `json:"matched_model_id"`
	MatchType        string               `json:"match_type"`
	HasExactStandard bool                 `json:"has_exact_standard"`
}

type ModelDefaultPricingDetail struct {
	CacheFallbackToInput bool `json:"cache_fallback_to_input"`
	DefaultPricingStatus
	RequestedModelID          string                 `json:"requested_model_id"`
	PricingKey                string                 `json:"pricing_key"`
	MatchedModelID            string                 `json:"matched_model_id"`
	MatchType                 string                 `json:"match_type"`
	Source                    string                 `json:"source"`
	Currency                  string                 `json:"currency"`
	Unit                      string                 `json:"pricing_unit"`
	HasAdminOverride          bool                   `json:"has_admin_override"`
	HasExactSystemStandard    bool                   `json:"has_exact_system_standard"`
	EffectivePricingAvailable bool                   `json:"effective_pricing_available"`
	SystemBaseline            DefaultPricingBaseline `json:"system_baseline"`
	AdminOverride             DefaultPricingFields   `json:"admin_override"`
	EffectivePricing          DefaultPricingFields   `json:"effective_pricing"`
	EditableFields            []string               `json:"editable_fields"`
	SupportedModes            []BillingMode          `json:"supported_modes"`
}

func defaultPricingUnit(mode BillingMode) string {
	switch mode {
	case BillingModePerRequest:
		return "per_request"
	case BillingModeImage:
		return "per_image"
	case BillingModeVideo:
		return "per_second"
	default:
		return "per_token"
	}
}

func presentDefaultPrice(value float64, present bool) *float64 {
	if !present && value == 0 {
		return nil
	}
	return &value
}

func defaultFieldsFromToken(p *ModelPricing) DefaultPricingFields {
	if p == nil {
		return DefaultPricingFields{}
	}
	return DefaultPricingFields{
		BillingMode: BillingModeToken,
		InputPrice:  presentDefaultPrice(p.InputPricePerToken, p.InputPricePresent), OutputPrice: presentDefaultPrice(p.OutputPricePerToken, p.OutputPricePresent),
		CacheWritePrice:   presentDefaultPrice(p.CacheCreationPricePerToken, p.CacheCreationPriceExplicit),
		CacheWrite1hPrice: presentDefaultPrice(p.CacheCreation1hPrice, p.CacheCreation1hPriceExplicit),
		CacheReadPrice:    presentDefaultPrice(p.CacheReadPricePerToken, p.CacheReadPriceExplicit),
		ImageInputPrice:   presentDefaultPrice(p.ImageInputPricePerToken, p.ImageInputPriceExplicit),
		ImageOutputPrice:  presentDefaultPrice(p.ImageOutputPricePerToken, p.ImageOutputPriceExplicit),
	}
}

func applyAdminTokenPricing(base *ModelPricing, fields DefaultPricingFields, key, revision string) *ModelPricing {
	out := &ModelPricing{}
	if base != nil {
		*out = *base
	}
	if fields.InputPrice != nil {
		out.InputPricePerToken = *fields.InputPrice
		out.InputPricePresent = true
	}
	if fields.OutputPrice != nil {
		out.OutputPricePerToken = *fields.OutputPrice
		out.OutputPricePresent = true
	}
	if fields.CacheWritePrice != nil {
		out.CacheCreationPricePerToken = *fields.CacheWritePrice
		out.CacheCreation5mPrice = *fields.CacheWritePrice
		out.CacheCreationPriceExplicit = true
	}
	if fields.CacheWrite1hPrice != nil {
		out.CacheCreation1hPrice = *fields.CacheWrite1hPrice
		out.CacheCreation1hPriceExplicit = true
		out.SupportsCacheBreakdown = true
	}
	if fields.CacheReadPrice != nil {
		out.CacheReadPricePerToken = *fields.CacheReadPrice
		out.CacheReadPriceExplicit = true
	}
	if fields.ImageInputPrice != nil {
		out.ImageInputPricePerToken = *fields.ImageInputPrice
		out.ImageInputPriceExplicit = true
	}
	if fields.ImageOutputPrice != nil {
		out.ImageOutputPricePerToken = *fields.ImageOutputPrice
		out.ImageOutputPriceExplicit = true
	}
	out.DefaultPricingApplied, out.PricingSource, out.PricingKey, out.DefaultPricingRevision = true, PricingSourceAdmin, key, revision
	return out
}

func (s *BillingService) GetModelPricing(model string) (*ModelPricing, error) {
	base, err := s.getSystemModelPricing(model)
	if s == nil || s.defaultPricing == nil {
		return base, err
	}
	fields, key, revision, ok := s.defaultPricing.Lookup(model)
	if !ok {
		return base, err
	}
	if fields.BillingMode != "" && fields.BillingMode != BillingModeToken {
		return nil, billingPricingUnavailable(model, "token")
	}
	pricing := s.effectiveAdminTokenPricing(model, base, fields, key, revision)
	if err := validateTokenModelPricing(model, pricing); err != nil {
		return nil, err
	}
	return pricing, nil
}

func (s *BillingService) GetSystemDefaultPricing(model string) DefaultPricingBaseline {
	model = strings.ToLower(strings.TrimSpace(model))
	result := DefaultPricingBaseline{Source: "unavailable", MatchType: "none", MatchedModelID: model}
	if s == nil {
		return result
	}
	if token, err := s.getSystemModelPricing(model); err == nil {
		result.tokenPricing = token
		result.Fields = defaultFieldsFromToken(token)
		result.Source, result.MatchedModelID, result.MatchType = token.PricingSource, token.PricingKey, token.PricingMatchType
		result.HasExactStandard = token.PricingMatchType == "exact" || s.fallbackPrices[model] != nil
		if s.pricingService != nil {
			if exact := s.pricingService.GetExactModelPricing(model); exact != nil && !exact.TokenPricingAbsent {
				result.HasExactStandard = true
			}
		}
		return result
	}
	image1, e1 := s.getSystemImagePriceChecked(model, ImageBillingSize1K)
	image2, e2 := s.getSystemImagePriceChecked(model, ImageBillingSize2K)
	image4, e4 := s.getSystemImagePriceChecked(model, ImageBillingSize4K)
	if e1 == nil && e2 == nil && e4 == nil {
		result.Fields = DefaultPricingFields{BillingMode: BillingModeImage, ImagePrice1K: &image1, ImagePrice2K: &image2, ImagePrice4K: &image4}
		result.Source, result.MatchType, result.HasExactStandard = "code", "exact", true
		if _, ok := getDefaultGrokImagineImagePrice(model, ImageBillingSize1K); !ok && s.pricingService != nil {
			_, match := s.pricingService.GetModelPricingWithMatch(model)
			result.Source, result.MatchedModelID, result.MatchType, result.HasExactStandard = match.Source, match.ModelID, match.MatchType, match.Exact
		}
		return result
	}
	video480, e480 := s.getSystemVideoPriceChecked(model, VideoBillingResolution480P)
	video720, e720 := s.getSystemVideoPriceChecked(model, VideoBillingResolution720P)
	video1080, e1080 := s.getSystemVideoPriceChecked(model, VideoBillingResolution1080P)
	if e480 == nil && e720 == nil && e1080 == nil {
		result.Fields = DefaultPricingFields{BillingMode: BillingModeVideo, VideoPrice480P: &video480, VideoPrice720P: &video720, VideoPrice1080P: &video1080}
		result.Source = "code"
		native := adminDefaultPricingAliasKey(model)
		result.MatchedModelID = VideoPriceFamilyGrokImagineVideo
		if strings.HasPrefix(native, "grok-imagine-video-1.5") {
			result.MatchedModelID = VideoPriceFamilyGrokImagineVideo15
		}
		result.HasExactStandard = model == result.MatchedModelID
		result.MatchType = "family"
		if result.HasExactStandard {
			result.MatchType = "exact"
		} else if native == result.MatchedModelID {
			result.MatchType = "alias"
		}

	}
	return result
}

func (s *BillingService) DefaultPricingDetail(ctx context.Context, model string) (*ModelDefaultPricingDetail, error) {
	key, err := NormalizeDefaultPricingModel(model)
	if err != nil {
		return nil, err
	}
	snapshot, _, err := s.defaultPricing.read(ctx)
	if err != nil {
		return nil, err
	}
	fields, exists := snapshot.Models[key]
	base := s.GetSystemDefaultPricing(key)
	detail := s.makeDefaultPricingDetail(model, key, fields, exists, s.defaultPricing.status(snapshot.Revision), base)
	s.applyInheritedDefaultPricingDetail(detail, key, snapshot, base)
	return detail, nil
}

func (s *BillingService) makeDefaultPricingDetail(model, key string, fields DefaultPricingFields, exists bool, status DefaultPricingStatus, base DefaultPricingBaseline) *ModelDefaultPricingDetail {
	effective := mergeDefaultPricingFields(base.Fields, fields)
	cacheFallback := validateTokenModelPricing(model, base.tokenPricing) != nil
	if exists && effective.BillingMode == BillingModeToken {
		effective = defaultFieldsFromToken(s.effectiveAdminTokenPricing(model, base.tokenPricing, fields, key, status.Version))
	}
	if effective.BillingMode == BillingModeToken {
		if effective.ImageInputPrice == nil {
			effective.ImageInputPrice = effective.InputPrice
		}
		if effective.ImageOutputPrice == nil {
			effective.ImageOutputPrice = effective.OutputPrice
		}
	}
	detail := &ModelDefaultPricingDetail{
		CacheFallbackToInput: cacheFallback, DefaultPricingStatus: status, RequestedModelID: model, PricingKey: key, MatchedModelID: base.MatchedModelID, MatchType: base.MatchType, Source: base.Source,
		Currency: "USD", Unit: defaultPricingUnit(effective.BillingMode), HasAdminOverride: exists, HasExactSystemStandard: base.HasExactStandard,
		EffectivePricingAvailable: validateDefaultPricingStandard(effective) == nil, SystemBaseline: base, AdminOverride: fields, EffectivePricing: effective,
		EditableFields: DefaultPricingEditableFields(), SupportedModes: []BillingMode{BillingModeToken, BillingModePerRequest, BillingModeImage, BillingModeVideo},
	}
	if exists {
		detail.Source, detail.MatchType, detail.MatchedModelID = PricingSourceAdmin, "exact", key
	} else if base.MatchedModelID != "" {
		detail.PricingKey = base.MatchedModelID
	}
	return detail
}

func (s *BillingService) getDefaultImagePriceChecked(model, size string) (float64, error) {
	if s != nil && s.defaultPricing != nil {
		fields, _, _, ok := s.defaultPricing.Lookup(model)
		if ok {
			if fields.BillingMode != "" && fields.BillingMode != BillingModeImage {
				return 0, billingPricingUnavailable(model, "image")
			}
			var price *float64
			switch NormalizeImageBillingTierOrDefault(size) {
			case ImageBillingSize1K:
				price = fields.ImagePrice1K
			case ImageBillingSize4K:
				price = fields.ImagePrice4K
			default:
				price = fields.ImagePrice2K
			}
			if price != nil {
				return *price, validateBillingPriceFor(model, "image", "admin_image_price", *price)
			}
		}
	}
	return s.getSystemImagePriceChecked(model, size)
}

func (s *BillingService) getDefaultVideoPriceChecked(model, resolution string) (float64, error) {
	if s != nil && s.defaultPricing != nil {
		fields, _, _, ok := s.defaultPricing.Lookup(model)
		if ok {
			if fields.BillingMode != "" && fields.BillingMode != BillingModeVideo {
				return 0, billingPricingUnavailable(model, "video")
			}
			var price *float64
			switch NormalizeVideoBillingResolutionOrDefault(resolution) {
			case VideoBillingResolution720P:
				price = fields.VideoPrice720P
			case VideoBillingResolution1080P:
				price = fields.VideoPrice1080P
			default:
				price = fields.VideoPrice480P
			}
			if price != nil {
				return *price, validateBillingPriceFor(model, "video", "admin_video_price", *price)
			}
		}
	}
	return s.getSystemVideoPriceChecked(model, resolution)
}

// resolveDefaultPricing reads the DB overlay once for a per-resolution immutable
// result. Video carries an explicit unit; configured cards retain their caller-supplied usage units.
func (s *BillingService) resolveDefaultPricing(model string) *ResolvedPricing {
	base := s.GetSystemDefaultPricing(model)
	fields, key, revision, has := s.defaultPricing.Lookup(model)
	effective := mergeDefaultPricingFields(base.Fields, fields)
	if effective.BillingMode == "" || effective.BillingMode == BillingModeToken {
		token := base.tokenPricing
		if has {
			token = s.effectiveAdminTokenPricing(model, token, fields, key, revision)
			if validateTokenModelPricing(model, token) != nil {
				token = nil
			}
		}
		source := base.Source
		if has {
			source = PricingSourceAdmin
		}
		return &ResolvedPricing{Mode: BillingModeToken, BasePricing: token, Source: source, DefaultPricingRevision: revision, SupportsCacheBreakdown: token != nil && token.SupportsCacheBreakdown}
	}
	source := base.Source
	if has {
		source = PricingSourceAdmin
	}
	resolved := &ResolvedPricing{Mode: effective.BillingMode, Source: source, Unit: defaultPricingUnit(effective.BillingMode), DefaultPricingRevision: revision}
	if effective.BillingMode == BillingModePerRequest && effective.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice, resolved.DefaultPerRequestPricePresent = *effective.PerRequestPrice, true
	}
	var labels []string
	var prices []*float64
	switch effective.BillingMode {
	case BillingModeImage:
		labels = []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K}
		prices = []*float64{effective.ImagePrice1K, effective.ImagePrice2K, effective.ImagePrice4K}
	case BillingModeVideo:
		labels = []string{VideoBillingResolution480P, VideoBillingResolution720P, VideoBillingResolution1080P}
		prices = []*float64{effective.VideoPrice480P, effective.VideoPrice720P, effective.VideoPrice1080P}
	}
	for i, label := range labels {
		if prices[i] != nil {
			resolved.RequestTiers = append(resolved.RequestTiers, PricingInterval{TierLabel: label, PerRequestPrice: prices[i]})
		}
	}
	if len(prices) > 0 && prices[0] != nil {
		resolved.DefaultPerRequestPrice, resolved.DefaultPerRequestPricePresent = *prices[0], true
	}
	return resolved
}

func (s *DefaultModelPricingService) LoadedVersion() string {
	if s == nil {
		return ""
	}
	if snapshot := s.snapshot.Load(); snapshot != nil {
		return strconv.FormatUint(snapshot.Revision, 10)
	}
	return ""
}

func (s *BillingService) SaveDefaultPricing(ctx context.Context, model, version string, patch DefaultPricingPatch, reset bool) (*ModelDefaultPricingDetail, *DefaultPricingChange, error) {
	base := s.GetSystemDefaultPricing(model)
	// A fuzzy family estimate is only a hint, never a complete new exact standard.
	validationBase := base.Fields
	if base.MatchType == "family" {
		validationBase.InputPrice = nil
		validationBase.OutputPrice = nil
	}
	change, err := s.defaultPricing.Save(ctx, model, version, patch, validationBase, reset)
	if err != nil {
		return nil, nil, err
	}
	revision, _ := strconv.ParseUint(change.NewVersion, 10, 64)
	exists := change.After.BillingMode != ""
	for _, price := range change.After.prices() {
		exists = exists || price != nil
	}
	detail := s.makeDefaultPricingDetail(model, change.ModelID, change.After, exists, s.defaultPricing.status(revision), base)
	s.applyInheritedDefaultPricingDetail(detail, change.ModelID, change.snapshot, base)
	return detail, change, nil
}

func (s *BillingService) HasExactDefaultPricing(model string) bool {
	key := strings.ToLower(strings.TrimSpace(model))
	if s.defaultPricing != nil {
		if snapshot := s.defaultPricing.snapshot.Load(); snapshot != nil {
			if _, exists := snapshot.Models[key]; exists {
				return true
			}
		}
	}
	return s.GetSystemDefaultPricing(key).HasExactStandard
}

// Presentation adapter only. Capability discovery continues to use accounts/routes.
func defaultResolvedDisplayPricing(resolved *ResolvedPricing, previous *ChannelModelPricing) *ChannelModelPricing {
	if resolved == nil {
		return previous
	}
	if resolved.Mode == BillingModeToken && resolved.BasePricing == nil {
		return previous
	}
	result := &ChannelModelPricing{BillingMode: resolved.Mode, PricingUnit: resolvedPricingQuoteUnit(resolved)}
	if resolved.BasePricing != nil {
		fields := defaultFieldsFromToken(resolved.BasePricing)
		result.InputPrice, result.OutputPrice = fields.InputPrice, fields.OutputPrice
		result.CacheWritePrice, result.CacheWrite1hPrice, result.CacheReadPrice = fields.CacheWritePrice, fields.CacheWrite1hPrice, fields.CacheReadPrice
		result.ImageInputPrice, result.ImageOutputPrice = fields.ImageInputPrice, fields.ImageOutputPrice
		result.MaxReasoningEffortMultiplier = resolved.BasePricing.MaxReasoningEffortMultiplier
	}
	if resolved.DefaultPerRequestPricePresent {
		price := resolved.DefaultPerRequestPrice
		result.PerRequestPrice = &price
	}
	result.Intervals = append([]PricingInterval(nil), resolved.RequestTiers...)
	return result
}

func (s *BillingService) HasIdentifiedTokenPricing(model string) bool {
	if s == nil {
		return false
	}
	if fields, _, _, exists := s.defaultPricing.Lookup(model); exists {
		if fields.BillingMode != "" && fields.BillingMode != BillingModeToken {
			return false
		}
		pricing, err := s.GetModelPricing(model)
		return err == nil && validateTokenModelPricing(model, pricing) == nil
	}
	return s.hasIdentifiedSystemTokenPricing(model)
}

// Media dispatch needs explicit administrator modes, but response-model identity
// guards must still distinguish default pricing from actual group/channel cards.
func (s *BillingService) resolveAdminDefaultMediaPricing(model string) *ResolvedPricing {
	if s == nil || s.defaultPricing == nil {
		return nil
	}
	resolved := s.resolveDefaultPricing(model)
	if resolved.Source == PricingSourceAdmin {
		return resolved
	}
	return nil
}

func (s *BillingService) calculateDefaultMediaRequestCost(ctx context.Context, model string, resolver *ModelPricingResolver, rate float64, resolved *ResolvedPricing) (*CostBreakdown, error) {
	if resolver == nil {
		resolver = NewModelPricingResolver(nil, s)
	}
	// This global unit is per HTTP request, not per returned image/second.
	return s.CalculateCostUnified(CostInput{Ctx: ctx, Model: model, Resolver: resolver, Resolved: resolved, RateMultiplier: rate, RequestCount: 1})
}

// A newly priceable token model has no established cache card. Without an
// explicit cache discount, those input tokens use its normal input rate, rather
// than silently becoming free after cached tokens are subtracted from input.
// Existing valid system cards (including their historical cache behavior) stay
// unchanged. No derived fields are persisted as if an administrator set them.
func (s *BillingService) effectiveAdminTokenPricing(model string, base *ModelPricing, fields DefaultPricingFields, key, revision string) *ModelPricing {
	newStandard := validateTokenModelPricing(model, base) != nil
	pricing := applyAdminTokenPricing(base, fields, key, revision)
	if base == nil {
		pricing.DefaultPricingApplied = false
		pricing = applyAdminTokenPricing(s.applyModelSpecificPricingPolicy(model, pricing), fields, key, revision)
	}
	if newStandard {
		if !pricing.CacheCreationPriceExplicit && pricing.CacheCreationPricePerToken == 0 {
			pricing.CacheCreationPricePerToken = pricing.InputPricePerToken
			pricing.CacheCreationPriceExplicit = pricing.InputPricePresent
		}
		pricing.CacheCreation5mPrice = pricing.CacheCreationPricePerToken
		if !pricing.CacheReadPriceExplicit && pricing.CacheReadPricePerToken == 0 {
			pricing.CacheReadPricePerToken = pricing.InputPricePerToken
			pricing.CacheReadPriceExplicit = pricing.InputPricePresent
		}
		if !pricing.CacheCreation1hPriceExplicit && pricing.CacheCreation1hPrice == 0 {
			pricing.CacheCreation1hPrice = pricing.CacheCreation5mPrice
			pricing.CacheCreation1hPriceExplicit = pricing.CacheCreationPriceExplicit || pricing.CacheCreation5mPrice != 0
		}
		pricing.SupportsCacheBreakdown = true
	}
	return pricing
}

func (s *BillingService) applyInheritedDefaultPricingDetail(detail *ModelDefaultPricingDetail, key string, snapshot *defaultPricingSnapshot, base DefaultPricingBaseline) {
	if detail.HasAdminOverride || snapshot == nil {
		return
	}
	canonical := adminDefaultPricingAliasKey(key)
	if inherited, ok := snapshot.Models[canonical]; ok && canonical != key {
		effective := s.makeDefaultPricingDetail(detail.RequestedModelID, canonical, inherited, true, detail.DefaultPricingStatus, base)
		detail.EffectivePricing, detail.EffectivePricingAvailable, detail.Unit = effective.EffectivePricing, effective.EffectivePricingAvailable, effective.Unit
		detail.PricingKey, detail.MatchedModelID, detail.MatchType, detail.Source = canonical, canonical, "alias", PricingSourceAdmin
	}
}
