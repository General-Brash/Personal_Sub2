package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// ModelPlazaHandler 处理「模型广场」查询。
//
// 广场路由挂 OptionalJWT 中间件：匿名可访问（除非 require_auth 开启），带 token 则
// 识别用户。可见性规则（橱窗语义，与「可用渠道」的可绑定语义不同）：
//   - 匿名：仅非专属分组（订阅型照常展示）；
//   - 登录：非专属分组 + user_allowed_groups 授权的专属分组（不检查订阅有效性）。
type ModelPlazaHandler struct {
	entitlements        *service.EntitlementService
	channelService      *service.ChannelService
	apiKeyService       *service.APIKeyService
	settingService      *service.SettingService
	modelCatalog        *service.ModelCatalogService
	modelAvailability   *service.ModelAvailabilityResolver
	priceQuote          *service.PriceQuoteService
	modelAccessProvider service.ModelAccessProvider
	modelPlazaV2Enabled bool
}

// NewModelPlazaHandler 创建模型广场 handler。
func NewModelPlazaHandler(
	channelService *service.ChannelService,
	apiKeyService *service.APIKeyService,
	settingService *service.SettingService,
) *ModelPlazaHandler {
	return &ModelPlazaHandler{
		channelService: channelService,
		apiKeyService:  apiKeyService,
		settingService: settingService,
	}
}

// modelPlazaOfficialPricing LiteLLM 官方参考价（USD per token）。
type modelPlazaOfficialPricing struct {
	Intervals         []userPricingIntervalDTO `json:"intervals,omitempty"`
	InputPrice        *float64                 `json:"input_price"`
	OutputPrice       *float64                 `json:"output_price"`
	CacheWritePrice   *float64                 `json:"cache_write_price"`
	CacheWrite1hPrice *float64                 `json:"cache_write_1h_price,omitempty"`
	CacheReadPrice    *float64                 `json:"cache_read_price"`
}

// modelPlazaModel 广场模型条目：渠道定价（白名单形态）+ 官方参考价。
type modelPlazaModel struct {
	LongContextBasis string                     `json:"long_context_basis,omitempty"`
	TimePricing      *modelPlazaTimePricing     `json:"time_pricing,omitempty"`
	Name             string                     `json:"name"`
	Platform         string                     `json:"platform"`
	Pricing          *userSupportedModelPricing `json:"pricing"`
	OfficialPricing  *modelPlazaOfficialPricing `json:"official_pricing"`
}

// modelPlazaGroup 广场分组条目（白名单字段）。
type modelPlazaGroup struct {
	LongContextPricingEnabled bool     `json:"long_context_pricing_enabled"`
	ID                        int64    `json:"id"`
	Name                      string   `json:"name"`
	Description               string   `json:"description"`
	Platform                  string   `json:"platform"`
	SubscriptionType          string   `json:"subscription_type"`
	RateMultiplier            float64  `json:"rate_multiplier"`
	UserRateMultiplier        *float64 `json:"user_rate_multiplier,omitempty"`
	PeakRateEnabled           bool     `json:"peak_rate_enabled"`
	PeakStart                 string   `json:"peak_start"`
	PeakEnd                   string   `json:"peak_end"`
	PeakRateMultiplier        float64  `json:"peak_rate_multiplier"`
	IsExclusive               bool     `json:"is_exclusive"`
	// 生图独立倍率：为 true 时图片计费模型的实付倍率取 ImageRateMultiplier，
	// 不取分组/用户专属倍率。
	ImageRateIndependent bool              `json:"image_rate_independent"`
	ImageRateMultiplier  float64           `json:"image_rate_multiplier"`
	Models               []modelPlazaModel `json:"models"`
}

// modelPlazaResponse 广场页响应。
type modelPlazaResponse struct {
	Description string            `json:"description"`
	Groups      []modelPlazaGroup `json:"groups"`
}

// Get 返回模型广场数据。
// GET /api/v1/model-plaza
func (h *ModelPlazaHandler) Get(c *gin.Context) {
	if h.settingService == nil {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}
	rt := h.settingService.GetModelPlazaRuntime(c.Request.Context())
	if !rt.Enabled {
		response.NotFound(c, "Model plaza is not enabled")
		return
	}

	subject, authed := middleware.GetAuthSubjectFromContext(c)
	if rt.RequireAuth && !authed {
		response.Unauthorized(c, "Authentication required")
		return
	}

	groups, err := h.channelService.ListPlazaGroups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	// allowedExclusive == nil 表示匿名；登录用户恒为非 nil（可能为空集合）。
	var allowedExclusive map[int64]struct{}
	var userRates map[int64]float64
	if authed {
		allowedExclusive, err = h.apiKeyService.GetUserAllowedGroupIDSet(c.Request.Context(), subject.UserID)
		if err != nil {
			// 可见性数据拿不到时不能静默降级成匿名视图（会错漏专属分组），直接报错。
			response.ErrorFrom(c, err)
			return
		}
		userRates, err = h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
		if err != nil {
			// 专属倍率仅是展示增强，失败降级为分组默认倍率。
			slog.Warn("model_plaza_user_rates_failed", "error", err, "user_id", subject.UserID)
			userRates = nil
		}
	}

	visible := filterPlazaVisibleGroups(groups, allowedExclusive)

	out := make([]modelPlazaGroup, 0, len(visible))
	for i := range visible {
		out = append(out, toModelPlazaGroupDTO(&visible[i], userRates))
	}
	response.Success(c, modelPlazaResponse{
		Description: rt.Description,
		Groups:      out,
	})
}

// filterPlazaVisibleGroups 按登录态裁剪分组可见性。
// allowedExclusive == nil 表示匿名（仅非专属）；非 nil 表示登录（非专属 + 授权专属）。
func filterPlazaVisibleGroups(
	groups []service.PlazaGroup,
	allowedExclusive map[int64]struct{},
) []service.PlazaGroup {
	visible := make([]service.PlazaGroup, 0, len(groups))
	for _, g := range groups {
		if g.IsExclusive {
			if allowedExclusive == nil {
				continue
			}
			if _, ok := allowedExclusive[g.ID]; !ok {
				continue
			}
		}
		visible = append(visible, g)
	}
	return visible
}

// toModelPlazaGroupDTO 将 service 层广场分组映射为白名单 DTO,并合并用户专属倍率。
func toModelPlazaGroupDTO(g *service.PlazaGroup, userRates map[int64]float64) modelPlazaGroup {
	models := make([]modelPlazaModel, 0, len(g.Models))
	for i := range g.Models {
		m := &g.Models[i]
		models = append(models, modelPlazaModel{
			LongContextBasis: string(m.LongContextBasis),
			TimePricing:      toModelPlazaTimePricing(m.TimePricing),
			Name:             m.Name,
			Platform:         m.Platform,
			Pricing:          toUserPricing(m.Pricing),
			OfficialPricing:  toModelPlazaOfficialPricing(m.OfficialPricing),
		})
	}
	dto := modelPlazaGroup{
		LongContextPricingEnabled: g.LongContextPricingEnabled,
		ID:                        g.ID,
		Name:                      g.Name,
		Description:               g.Description,
		Platform:                  g.Platform,
		SubscriptionType:          g.SubscriptionType,
		RateMultiplier:            g.RateMultiplier,
		PeakRateEnabled:           g.PeakRateEnabled,
		PeakStart:                 g.PeakStart,
		PeakEnd:                   g.PeakEnd,
		PeakRateMultiplier:        g.PeakRateMultiplier,
		IsExclusive:               g.IsExclusive,
		ImageRateIndependent:      g.ImageRateIndependent,
		ImageRateMultiplier:       g.ImageRateMultiplier,
		Models:                    models,
	}
	if rate, ok := userRates[g.ID]; ok {
		dto.UserRateMultiplier = &rate
	}
	return dto
}

// toModelPlazaOfficialPricing 转换官方参考价；nil 透传（前端显示 "-"）。
func toModelPlazaOfficialPricing(p *service.PlazaOfficialPricing) *modelPlazaOfficialPricing {
	if p == nil {
		return nil
	}
	return &modelPlazaOfficialPricing{
		Intervals:         toUserPricingIntervals(p.Intervals),
		InputPrice:        p.InputPrice,
		OutputPrice:       p.OutputPrice,
		CacheWritePrice:   p.CacheWritePrice,
		CacheWrite1hPrice: p.CacheWrite1hPrice,
		CacheReadPrice:    p.CacheReadPrice,
	}
}

type modelPlazaTimePricingPeriod struct {
	StartTime  string  `json:"start_time"`
	EndTime    string  `json:"end_time"`
	Multiplier float64 `json:"multiplier"`
}

// modelPlazaTimePricing 计费会生效的分时倍率（仅倍率 ≠ 1 的时段）。
// WeekdaysOnly 为 true 时时段仅周一至周五生效，周末整天按标准价计费。
type modelPlazaTimePricing struct {
	Timezone     string                        `json:"timezone"`
	WeekdaysOnly bool                          `json:"weekdays_only,omitempty"`
	Periods      []modelPlazaTimePricingPeriod `json:"periods"`
}

func toModelPlazaTimePricing(p *service.TimePricingSchedule) *modelPlazaTimePricing {
	if p == nil || len(p.Periods) == 0 {
		return nil
	}
	periods := make([]modelPlazaTimePricingPeriod, 0, len(p.Periods))
	for _, period := range p.Periods {
		periods = append(periods, modelPlazaTimePricingPeriod{
			StartTime:  period.StartTime,
			EndTime:    period.EndTime,
			Multiplier: period.Multiplier,
		})
	}
	return &modelPlazaTimePricing{Timezone: p.Timezone, WeekdaysOnly: p.WeekdaysOnly, Periods: periods}
}

// SetModelPlazaV2 configures the versioned plaza without changing the legacy
// constructor or shared routes/wire. Disabled is the default.
func (h *ModelPlazaHandler) SetModelPlazaV2(
	enabled bool,
	catalog *service.ModelCatalogService,
	availability *service.ModelAvailabilityResolver,
	quote *service.PriceQuoteService,
	accessProvider service.ModelAccessProvider,
) {
	if h == nil {
		return
	}
	h.modelPlazaV2Enabled = enabled
	h.modelCatalog = catalog
	h.modelAvailability = availability
	h.priceQuote = quote
	h.modelAccessProvider = accessProvider
}

// GetV2 returns the independent catalog response.
// GET /api/v1/model-plaza/v2
func (h *ModelPlazaHandler) GetV2(c *gin.Context) {
	if h == nil || !h.modelPlazaV2Enabled || h.modelCatalog == nil || h.modelAvailability == nil {
		response.NotFound(c, "Model plaza v2 is not enabled")
		return
	}
	if h.entitlements != nil && h.settingService != nil && !h.settingService.IsModelPlazaV2Enabled(c.Request.Context()) {
		response.NotFound(c, "Model plaza v2 is not enabled")
		return
	}
	if h.settingService != nil {
		rt := h.settingService.GetModelPlazaRuntime(c.Request.Context())
		if !rt.Enabled {
			response.NotFound(c, "Model plaza is not enabled")
			return
		}
		if rt.RequireAuth {
			if _, authed := middleware.GetAuthSubjectFromContext(c); !authed {
				response.Unauthorized(c, "Authentication required")
				return
			}
		}
	}
	subject, authed := middleware.GetAuthSubjectFromContext(c)
	access, err := h.resolveV2Access(c, subject, authed)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	items, err := h.modelCatalog.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	userRates := map[int64]float64{}
	if authed && h.apiKeyService != nil {
		rates, rateErr := h.apiKeyService.GetUserGroupRates(c.Request.Context(), subject.UserID)
		if rateErr != nil {
			response.ErrorFrom(c, rateErr)
			return
		}
		userRates = rates
	}
	out := make([]modelPlazaV2Model, 0, len(items))
	for i := range items {
		item := items[i]
		availability := h.modelAvailability.Resolve(item, access)
		if access.Anonymous && availability.State != service.ModelAvailabilityCatalogOnly {
			continue
		}
		if !access.Anonymous && availability.State == service.ModelAvailabilityNotEntitled && !modelPlazaV2ItemHasVisibleRoute(item, access) {
			continue
		}
		choices := make([]modelPlazaV2GroupChoice, 0, len(item.Routes))
		for _, route := range item.Routes {
			if access.Anonymous {
				if route.Exclusive {
					continue
				}
			} else if !modelPlazaV2RouteAllowed(route, access) {
				continue
			}
			choice := modelPlazaV2GroupChoice{
				GroupID: route.GroupID, GroupName: route.GroupName, Platform: route.GroupPlatform,
				RouteKind: route.RouteKind, AvailabilityState: service.ModelAvailabilityTemporarilyUnavailable,
				ReasonCode: route.AvailabilityReason, Schedulable: route.Schedulable,
			}
			if access.Anonymous {
				choice.AvailabilityState = service.ModelAvailabilityCatalogOnly
			} else if route.Schedulable && route.AvailabilityKnown {
				choice.AvailabilityState = service.ModelAvailabilityEligible
			} else if !route.AvailabilityKnown {
				choice.AvailabilityState = service.ModelAvailabilityUnknown
			}
			if h.priceQuote != nil && route.Group != nil {
				staticRate := route.Group.RateMultiplier
				rateSource := "group_default"
				var userRate *float64
				if rate, ok := userRates[route.GroupID]; ok {
					value := rate
					userRate = &value
					rateSource = "user_override"
				}
				peakRate := route.Group.PeakMultiplierAt(time.Now())
				if authed && h.entitlements != nil {
					effective, rateErr := h.entitlements.ResolveGroupRate(c.Request.Context(), subject.UserID, route.GroupID, userRate, staticRate)
					if rateErr != nil {
						response.ErrorFrom(c, rateErr)
						return
					}
					staticRate = effective.Multiplier
					userRate = nil // already resolved: do not multiply an override twice
					rateSource = effective.Source
				}
				sourceVersions := cloneModelPlazaStringMap(item.SourceVersions)
				if route.SourceVersion != "" {
					sourceVersions["route"] = route.SourceVersion
				}
				quoteInput := service.PriceQuoteInput{
					Model: route.RequestModelID, GroupID: route.GroupID, Group: route.Group,
					UserID: subject.UserID, StaticRateMultiplier: &staticRate, UserRateMultiplier: userRate, PeakRateMultiplier: &peakRate,
					RateSource: rateSource, SourceVersions: sourceVersions,
				}
				if q := h.priceQuote.Quote(c.Request.Context(), quoteInput); q != nil {
					var dynamicFactor *modelPlazaV2DynamicFactor
					if q.DynamicFactor != nil {
						dynamicFactor = &modelPlazaV2DynamicFactor{
							Factor: q.DynamicFactor.Factor, Source: q.DynamicFactor.Source, Version: q.DynamicFactor.Version, Details: q.DynamicFactor.Details,
						}
					}
					quoteDTO := modelPlazaV2PriceQuote{
						QuoteVersion: q.QuoteVersion, PricedAt: q.PricedAt.UTC(), Unit: q.Unit, Currency: q.Currency,
						InputPerMillion: q.InputPerMillion, OutputPerMillion: q.OutputPerMillion,
						CacheWritePerMillion: q.CacheWritePerMillion, CacheWrite1hPerMillion: q.CacheWrite1hPerMillion,
						CacheReadPerMillion: q.CacheReadPerMillion, PerRequestPrice: q.PerRequestPrice,
						Intervals: modelPlazaV2QuoteIntervals(q.Intervals), DynamicFactorStatus: q.DynamicFactorStatus,
						DynamicFactor: dynamicFactor, Source: q.Source, UnknownFields: q.UnknownFields,
						EffectiveRateMultiplier: q.EffectiveRateMultiplier, PeakRateMultiplier: q.PeakRateMultiplier, RateSource: q.RateSource,
						ImageRateIndependent: q.ImageRateIndependent, ImageRateMultiplier: q.ImageRateMultiplier,
						ChannelTimeMultiplier: q.ChannelTimeMultiplier, PriceConditions: modelPlazaV2PriceConditions(q.PriceConditions),
					}
					choice.PriceQuote = &quoteDTO
					choice.QuoteVersion = q.QuoteVersion
					choice.PricedAt = &quoteDTO.PricedAt
				}
			}
			choices = append(choices, choice)
		}
		if len(choices) == 0 {
			continue
		}
		model := modelPlazaV2Model{
			ModelID: item.ModelID, DisplayName: item.DisplayName, Platform: item.Platform,
			Capabilities: item.Capabilities, SupportedEndpoints: item.SupportedEndpoints,
			ContextWindow: cloneModelPlazaInt(item.ContextWindow), AvailabilityState: availability.State,
			Eligibility: availability.Eligibility, ReasonCode: availability.ReasonCode,
			GroupChoices: choices, SourceVersions: publicModelPlazaSourceVersions(item.SourceVersions),
		}
		if len(choices) == 1 {
			model.QuoteVersion = choices[0].QuoteVersion
			model.PricedAt = choices[0].PricedAt
		}
		out = append(out, model)
	}
	response.Success(c, modelPlazaV2Response{Models: out, GeneratedAt: time.Now().UTC()})
}

func (h *ModelPlazaHandler) resolveV2Access(c *gin.Context, subject middleware.AuthSubject, authed bool) (service.ModelAccessInput, error) {
	if h.modelAccessProvider != nil {
		return h.modelAccessProvider.ResolveModelAccess(c.Request.Context(), subject.UserID, !authed)
	}
	input := service.ModelAccessInput{Anonymous: !authed}
	if !authed || h.apiKeyService == nil {
		return input, nil
	}
	allowed, err := h.apiKeyService.GetUserAllowedGroupIDSet(c.Request.Context(), subject.UserID)
	if err != nil {
		return service.ModelAccessInput{}, err
	}
	input.AllowedGroupIDs = allowed
	return input, nil
}

func modelPlazaV2RouteAllowed(route service.ModelCatalogRoute, access service.ModelAccessInput) bool {
	if allowed, ok := access.GroupEntitlement[route.GroupID]; ok {
		return allowed
	}
	if _, ok := access.AllowedGroupIDs[route.GroupID]; ok {
		return true
	}
	return !route.Exclusive && !route.Subscription
}

func modelPlazaV2ItemHasVisibleRoute(item service.ModelCatalogItem, access service.ModelAccessInput) bool {
	for _, route := range item.Routes {
		if access.Anonymous {
			if !route.Exclusive {
				return true
			}
			continue
		}
		if modelPlazaV2RouteAllowed(route, access) {
			return true
		}
	}
	return false
}

func modelPlazaV2PriceConditions(values []service.PriceQuoteCondition) []modelPlazaV2PriceCondition {
	if len(values) == 0 {
		return nil
	}
	out := make([]modelPlazaV2PriceCondition, 0, len(values))
	for _, value := range values {
		out = append(out, modelPlazaV2PriceCondition{
			Pattern: value.Pattern, Unit: value.Unit, BillingMode: value.BillingMode,
			InputPerMillion: value.InputPerMillion, OutputPerMillion: value.OutputPerMillion,
			CacheWritePerMillion: value.CacheWritePerMillion, CacheWrite1hPerMillion: value.CacheWrite1hPerMillion,
			CacheReadPerMillion: value.CacheReadPerMillion, PerRequestPrice: value.PerRequestPrice,
			Intervals: modelPlazaV2QuoteIntervals(value.Intervals),
		})
	}
	return out
}

func modelPlazaV2QuoteIntervals(values []service.PriceQuoteInterval) []modelPlazaV2QuoteInterval {
	if len(values) == 0 {
		return nil
	}
	out := make([]modelPlazaV2QuoteInterval, 0, len(values))
	for _, value := range values {
		out = append(out, modelPlazaV2QuoteInterval{
			MinTokens: value.MinTokens, MaxTokens: cloneModelPlazaInt(value.MaxTokens), TierLabel: value.TierLabel,
			InputPerMillion: value.InputPerMillion, OutputPerMillion: value.OutputPerMillion,
			CacheWritePerMillion: value.CacheWritePerMillion, CacheWrite1hPerMillion: value.CacheWrite1hPerMillion,
			CacheReadPerMillion: value.CacheReadPerMillion, PerRequestPrice: value.PerRequestPrice,
		})
	}
	return out
}

func cloneModelPlazaInt(value *int) *int {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func publicModelPlazaSourceVersions(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string)
	material := make([]string, 0, len(value))
	for key, item := range value {
		if strings.HasPrefix(key, "group:") || strings.HasPrefix(key, "account:") {
			material = append(material, key+"="+item)
			continue
		}
		out[key] = item
	}
	if len(material) > 0 {
		sort.Strings(material)
		sum := sha256.Sum256([]byte(strings.Join(material, "\n")))
		out["catalog"] = "sha256:" + hex.EncodeToString(sum[:])
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cloneModelPlazaStringMap(value map[string]string) map[string]string {
	if len(value) == 0 {
		return nil
	}
	out := make(map[string]string, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

type modelPlazaV2Response struct {
	Models      []modelPlazaV2Model `json:"models"`
	GeneratedAt time.Time           `json:"generated_at"`
}

type modelPlazaV2Model struct {
	ModelID            string                           `json:"model_id"`
	DisplayName        string                           `json:"display_name"`
	Platform           string                           `json:"platform"`
	Capabilities       []service.ModelCatalogCapability `json:"capabilities,omitempty"`
	SupportedEndpoints []string                         `json:"supported_endpoints,omitempty"`
	ContextWindow      *int                             `json:"context_window,omitempty"`
	AvailabilityState  service.ModelAvailabilityState   `json:"availability_state"`
	Eligibility        service.ModelAvailabilityState   `json:"eligibility"`
	ReasonCode         string                           `json:"reason_code,omitempty"`
	GroupChoices       []modelPlazaV2GroupChoice        `json:"user_group_choices"`
	SourceVersions     map[string]string                `json:"source_versions,omitempty"`
	QuoteVersion       string                           `json:"quote_version,omitempty"`
	PricedAt           *time.Time                       `json:"priced_at,omitempty"`
}

type modelPlazaV2GroupChoice struct {
	GroupID           int64                          `json:"group_id"`
	GroupName         string                         `json:"group_name"`
	Platform          string                         `json:"platform"`
	RouteKind         string                         `json:"route_kind"`
	AvailabilityState service.ModelAvailabilityState `json:"availability_state"`
	ReasonCode        string                         `json:"reason_code,omitempty"`
	Schedulable       bool                           `json:"schedulable"`
	PriceQuote        *modelPlazaV2PriceQuote        `json:"price_quote,omitempty"`
	QuoteVersion      string                         `json:"quote_version,omitempty"`
	PricedAt          *time.Time                     `json:"priced_at,omitempty"`
}

type modelPlazaV2PriceQuote struct {
	PeakRateMultiplier      *float64                     `json:"peak_rate_multiplier,omitempty"`
	QuoteVersion            string                       `json:"quote_version"`
	PricedAt                time.Time                    `json:"priced_at"`
	Unit                    string                       `json:"pricing_unit"`
	Currency                string                       `json:"currency"`
	InputPerMillion         *float64                     `json:"input_per_million,omitempty"`
	OutputPerMillion        *float64                     `json:"output_per_million,omitempty"`
	CacheWritePerMillion    *float64                     `json:"cache_write_per_million,omitempty"`
	CacheWrite1hPerMillion  *float64                     `json:"cache_write_1h_per_million,omitempty"`
	CacheReadPerMillion     *float64                     `json:"cache_read_per_million,omitempty"`
	PerRequestPrice         *float64                     `json:"per_request_price,omitempty"`
	Intervals               []modelPlazaV2QuoteInterval  `json:"intervals,omitempty"`
	DynamicFactorStatus     string                       `json:"dynamic_factor_status"`
	DynamicFactor           *modelPlazaV2DynamicFactor   `json:"dynamic_factor,omitempty"`
	EffectiveRateMultiplier *float64                     `json:"effective_rate_multiplier,omitempty"`
	RateSource              string                       `json:"rate_source,omitempty"`
	ImageRateIndependent    bool                         `json:"image_rate_independent,omitempty"`
	ImageRateMultiplier     *float64                     `json:"image_rate_multiplier,omitempty"`
	ChannelTimeMultiplier   *float64                     `json:"channel_time_multiplier,omitempty"`
	PriceConditions         []modelPlazaV2PriceCondition `json:"price_conditions,omitempty"`
	Source                  string                       `json:"source,omitempty"`
	UnknownFields           []string                     `json:"unknown_fields,omitempty"`
}

type modelPlazaV2PriceCondition struct {
	Pattern                string                      `json:"pattern"`
	Unit                   string                      `json:"pricing_unit"`
	BillingMode            string                      `json:"billing_mode,omitempty"`
	InputPerMillion        *float64                    `json:"input_per_million,omitempty"`
	OutputPerMillion       *float64                    `json:"output_per_million,omitempty"`
	CacheWritePerMillion   *float64                    `json:"cache_write_per_million,omitempty"`
	CacheWrite1hPerMillion *float64                    `json:"cache_write_1h_per_million,omitempty"`
	CacheReadPerMillion    *float64                    `json:"cache_read_per_million,omitempty"`
	PerRequestPrice        *float64                    `json:"per_request_price,omitempty"`
	Intervals              []modelPlazaV2QuoteInterval `json:"intervals,omitempty"`
}

type modelPlazaV2DynamicFactor struct {
	Details map[string]string `json:"details,omitempty"`
	Factor  float64           `json:"factor"`
	Source  string            `json:"source"`
	Version string            `json:"version,omitempty"`
}

type modelPlazaV2QuoteInterval struct {
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
