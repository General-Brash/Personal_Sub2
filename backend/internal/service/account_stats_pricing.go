package service

import (
	"context"
	"strings"
)

// accountStatsUsage 描述一次使用记录在账号统计中的计费单位。
// 它与 BillingService.CalculateCostUnified 使用的 CostInput 保持一一对应，
// 避免账号统计重新实现一套会漏掉倍率或媒体单位的公式。
type accountStatsUsage struct {
	BillingMode  BillingMode
	BillingTier  string
	ServiceTier  string
	RequestCount int
	UsageUnits   float64
	SizeTier     string
}

type accountStatsRuleResult struct {
	cost      *float64
	matched   bool
	supported bool
}

// resolveAccountStatsCost 计算账号统计定价费用。
// 返回 nil 表示不覆盖，使用默认公式（total_cost × account_rate_multiplier）。
//
// 优先级（先命中为准）：
//  1. 自定义规则（始终尝试，不依赖 ApplyPricingToAccountStats 开关）
//  2. ApplyPricingToAccountStats 启用时，直接使用本次请求的客户计费（倍率前的 totalCost）
//  3. 模型定价文件（LiteLLM）中上游模型的默认价格
//  4. nil → 走默认公式（total_cost × account_rate_multiplier）
//
// upstreamModel 是最终发往上游的模型 ID。
// totalCost 是本次请求的客户计费（倍率前），用于优先级 2。
// serviceTier 是最终参与用户计费的 OpenAI 服务层级，用于优先级 3。
//
// 保留该签名供 178-P1 的调用点和历史测试使用；完整媒体计费信息由
// applyAccountStatsCost 通过 UsageLog 构造 accountStatsUsage 后走增强路径。
func resolveAccountStatsCost(
	ctx context.Context,
	channelService *ChannelService,
	billingService *BillingService,
	accountID int64,
	groupID int64,
	upstreamModel string,
	tokens UsageTokens,
	requestCount int,
	totalCost float64,
	serviceTier string,
) *float64 {
	return resolveAccountStatsCostWithUsage(
		ctx, channelService, billingService, accountID, groupID, upstreamModel,
		tokens, totalCost, accountStatsUsage{RequestCount: requestCount, ServiceTier: serviceTier},
	)
}

func resolveAccountStatsCostWithUsage(
	ctx context.Context,
	channelService *ChannelService,
	billingService *BillingService,
	accountID int64,
	groupID int64,
	upstreamModel string,
	tokens UsageTokens,
	totalCost float64,
	usage accountStatsUsage,
) *float64 {
	if channelService == nil || upstreamModel == "" {
		return nil
	}
	channel, err := channelService.GetChannelForGroup(ctx, groupID)
	if err != nil || channel == nil {
		return nil
	}
	if !isSupportedAccountStatsBillingMode(usage.BillingMode) {
		// 不把未知媒体模式静默降级成 token 计费。正常配置会在渠道写入时被拒绝；
		// 这里对历史/脏数据保持 fail-safe，交回调用方的默认账务策略。
		return nil
	}

	platform := channelService.GetGroupPlatform(ctx, groupID)

	// 优先级 1：自定义规则（始终尝试）
	custom := tryCustomRulesWithUsage(
		billingService, channel, accountID, groupID, platform, upstreamModel, tokens, usage,
	)
	if custom.matched {
		if !custom.supported {
			// 定价条目已命中但模式不受支持时，不再落入下方 token/LiteLLM 公式。
			return nil
		}
		if custom.cost != nil {
			return custom.cost
		}
	}

	// 优先级 2：渠道开启"应用模型定价到账号统计"时，直接使用客户计费（倍率前）
	if channel.ApplyPricingToAccountStats {
		cost := totalCost
		if cost <= 0 {
			return nil
		}
		return &cost
	}

	// 优先级 3：模型定价文件仅定义 token 价格。图片/按次/视频请求不能
	// 把 token 数量误当成媒体单位，未配置自定义规则时交回默认账务策略。
	if billingService != nil && isTokenAccountStatsBillingMode(usage.BillingMode) {
		return tryModelFilePricing(billingService, upstreamModel, tokens, usage.ServiceTier)
	}

	return nil
}

func isSupportedAccountStatsBillingMode(mode BillingMode) bool {
	switch mode {
	case "", BillingModeToken, BillingModePerRequest, BillingModeImage, BillingModeVideo:
		return true
	default:
		return false
	}
}

func isTokenAccountStatsBillingMode(mode BillingMode) bool {
	return mode == "" || mode == BillingModeToken
}

// tryModelFilePricing 使用模型定价文件（LiteLLM/fallback）中的价格计算费用。
// 无论是否存在服务层级或长上下文，都复用 BillingService 的统一 token 公式，
// 以覆盖图片输入/输出、缓存细分、Fast/Flex 和长上下文等分支。
func tryModelFilePricing(billingService *BillingService, model string, tokens UsageTokens, serviceTier string) *float64 {
	if billingService == nil {
		return nil
	}
	pricing, err := billingService.GetModelPricing(model)
	if err != nil || pricing == nil {
		return nil
	}
	breakdown, err := billingService.CalculateCostWithServiceTier(
		model, tokens, 1, normalizeBillingServiceTier(serviceTier),
	)
	if err != nil || breakdown == nil || breakdown.TotalCost <= 0 {
		return nil
	}
	return &breakdown.TotalCost
}

// tryCustomRules 遍历自定义规则，按数组顺序先命中为准。
// 保留历史签名；完整请求单位由 tryCustomRulesWithUsage 提供。
func tryCustomRules(
	channel *Channel, accountID, groupID int64,
	platform, model string, tokens UsageTokens, requestCount int,
) *float64 {
	result := tryCustomRulesWithUsage(
		nil, channel, accountID, groupID, platform, model, tokens,
		accountStatsUsage{RequestCount: requestCount},
	)
	return result.cost
}

func tryCustomRulesWithUsage(
	billingService *BillingService,
	channel *Channel,
	accountID, groupID int64,
	platform, model string,
	tokens UsageTokens,
	usage accountStatsUsage,
) accountStatsRuleResult {
	if channel == nil {
		return accountStatsRuleResult{}
	}
	modelLower := strings.ToLower(model)
	for _, rule := range channel.AccountStatsPricingRules {
		if !matchAccountStatsRule(&rule, accountID, groupID) {
			continue
		}
		pricing := findPricingForModel(rule.Pricing, platform, modelLower)
		if pricing == nil {
			continue // 规则匹配但模型不在规则定价中，继续下一条
		}
		cost, supported := calculateStatsCostForUsage(billingService, model, pricing, tokens, usage)
		return accountStatsRuleResult{cost: cost, matched: true, supported: supported}
	}
	return accountStatsRuleResult{}
}

// matchAccountStatsRule 检查规则是否匹配指定的 accountID 和 groupID。
// 匹配条件：accountID ∈ rule.AccountIDs 或 groupID ∈ rule.GroupIDs。
// 如果规则的 AccountIDs 和 GroupIDs 都为空，视为不匹配。
func matchAccountStatsRule(rule *AccountStatsPricingRule, accountID, groupID int64) bool {
	if len(rule.AccountIDs) == 0 && len(rule.GroupIDs) == 0 {
		return false
	}
	for _, id := range rule.AccountIDs {
		if id == accountID {
			return true
		}
	}
	for _, id := range rule.GroupIDs {
		if id == groupID {
			return true
		}
	}
	return false
}

// findPricingForModel 在定价列表中查找匹配的模型定价。
// 先精确匹配，再通配符匹配（按配置顺序，先匹配先使用）。
func findPricingForModel(pricingList []ChannelModelPricing, platform, modelLower string) *ChannelModelPricing {
	// 精确匹配优先
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			if strings.ToLower(m) == modelLower {
				return p
			}
		}
	}
	// 通配符匹配：按配置顺序，先匹配先使用
	for i := range pricingList {
		p := &pricingList[i]
		if !isPlatformMatch(platform, p.Platform) {
			continue
		}
		for _, m := range p.Models {
			ml := strings.ToLower(m)
			if !strings.HasSuffix(ml, "*") {
				continue
			}
			prefix := strings.TrimSuffix(ml, "*")
			if strings.HasPrefix(modelLower, prefix) {
				return p
			}
		}
	}
	return nil
}

// isPlatformMatch 判断平台是否匹配（空平台视为不限平台）。
func isPlatformMatch(queryPlatform, pricingPlatform string) bool {
	if queryPlatform == "" || pricingPlatform == "" {
		return true
	}
	return queryPlatform == pricingPlatform
}

// calculateStatsCost 使用给定的定价计算费用（不含账号倍率，原始费用）。
// 计算委托给 BillingService.CalculateCostUnified，确保账号统计和真实扣费
// 使用完全相同的 token、interval、Fast/Flex、图片和视频语义。
func calculateStatsCost(pricing *ChannelModelPricing, tokens UsageTokens, requestCount int) *float64 {
	cost, _ := calculateStatsCostForUsage(
		nil, "", pricing, tokens, accountStatsUsage{RequestCount: requestCount},
	)
	return cost
}

// calculatePerRequestStatsCost 保留历史内部辅助函数名，实际走统一按次公式。
func calculatePerRequestStatsCost(pricing *ChannelModelPricing, requestCount int) *float64 {
	cost, _ := calculateStatsCostForUsage(
		nil, "", pricing, UsageTokens{}, accountStatsUsage{RequestCount: requestCount},
	)
	return cost
}

// calculateTokenStatsCost 保留历史内部辅助函数名，实际走统一 token 公式。
func calculateTokenStatsCost(pricing *ChannelModelPricing, tokens UsageTokens) *float64 {
	cost, _ := calculateStatsCostForUsage(nil, "", pricing, tokens, accountStatsUsage{})
	return cost
}

func calculateStatsCostForUsage(
	billingService *BillingService,
	model string,
	pricing *ChannelModelPricing,
	tokens UsageTokens,
	usage accountStatsUsage,
) (*float64, bool) {
	if pricing == nil {
		return nil, true
	}
	mode := pricing.BillingMode
	if mode == "" {
		mode = BillingModeToken
	}
	if !isSupportedAccountStatsBillingMode(mode) {
		// 绝不能把未知模式送进 CalculateCostUnified 的 default token 分支。
		return nil, false
	}

	if billingService == nil {
		billingService = &BillingService{}
	}
	resolved := accountStatsResolvedPricing(model, pricing, billingService)
	cost, err := billingService.CalculateCostUnified(CostInput{
		Model:          model,
		Tokens:         tokens,
		RequestCount:   usage.RequestCount,
		UsageUnits:     usage.UsageUnits,
		SizeTier:       usage.SizeTier,
		RateMultiplier: 1,
		ServiceTier:    usage.ServiceTier,
		Resolver:       &ModelPricingResolver{},
		Resolved:       resolved,
	})
	if err != nil || cost == nil || cost.TotalCost <= 0 {
		return nil, true
	}
	value := cost.TotalCost
	return &value, true
}

func accountStatsResolvedPricing(model string, configured *ChannelModelPricing, billingService *BillingService) *ResolvedPricing {
	mode := configured.BillingMode
	if mode == "" {
		mode = BillingModeToken
	}
	resolved := &ResolvedPricing{
		Mode:                      mode,
		Source:                    PricingSourceChannel,
		channelPricing:            configured,
		longContextPricingEnabled: true,
	}
	if mode == BillingModePerRequest || mode == BillingModeImage || mode == BillingModeVideo {
		resolved.RequestTiers = filterValidIntervals(configured.Intervals)
		if configured.PerRequestPrice != nil {
			resolved.DefaultPerRequestPrice = *configured.PerRequestPrice
			resolved.DefaultPerRequestPricePresent = true
		}
		return resolved
	}

	resolved.BasePricing = accountStatsModelPricing(model, configured, billingService)
	resolved.Intervals = filterValidIntervals(configured.Intervals)
	return resolved
}

// accountStatsModelPricing 将账号统计规则的显式字段转换成统一 ModelPricing。
// 未配置的价格保持为 0，延续账号统计规则的既有“显式配置即覆盖、未配置不计”
// 语义；只有已配置的标准价格才借用模型目录的 Fast/Priority 比例。
func accountStatsModelPricing(model string, configured *ChannelModelPricing, billingService *BillingService) *ModelPricing {
	pricing := &ModelPricing{
		FastMultiplier:           configured.FastMultiplier,
		FlexMultiplier:           configured.FlexMultiplier,
		ImageOutputPriceExplicit: true,
	}
	var catalog *ModelPricing
	if billingService != nil {
		catalog, _ = billingService.GetModelPricing(model)
	}
	if configured.InputPrice != nil {
		pricing.InputPricePerToken = *configured.InputPrice
		pricing.InputPricePresent = true
		if catalog != nil {
			pricing.InputPricePerTokenPriority = channelTierOverridePrice(
				catalog.InputPricePerToken, catalog.InputPricePerTokenPriority, *configured.InputPrice,
			)
		}
	}
	if configured.OutputPrice != nil {
		pricing.OutputPricePerToken = *configured.OutputPrice
		pricing.OutputPricePresent = true
		if catalog != nil {
			pricing.OutputPricePerTokenPriority = channelTierOverridePrice(
				catalog.OutputPricePerToken, catalog.OutputPricePerTokenPriority, *configured.OutputPrice,
			)
		}
	}
	if configured.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *configured.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *configured.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		if catalog != nil {
			pricing.CacheCreationPricePerTokenPriority = channelTierOverridePrice(
				catalog.CacheCreationPricePerToken, catalog.CacheCreationPricePerTokenPriority, *configured.CacheWritePrice,
			)
		}
	}
	if configured.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *configured.CacheReadPrice
		if catalog != nil {
			pricing.CacheReadPricePerTokenPriority = channelTierOverridePrice(
				catalog.CacheReadPricePerToken, catalog.CacheReadPricePerTokenPriority, *configured.CacheReadPrice,
			)
		}
	}
	if configured.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *configured.ImageInputPrice
	}
	if configured.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *configured.ImageOutputPrice
	}
	return pricing
}

// applyAccountStatsCost resolves the account stats cost for a usage log entry.
// It resolves the upstream model (falling back to the requested model) and calls
// the 4-level priority chain via resolveAccountStatsCostWithUsage. Media fields
// are intentionally read from UsageLog so both gateway callers retain their
// historical function signature while video/image units remain lossless.
func applyAccountStatsCost(
	ctx context.Context,
	usageLog *UsageLog,
	cs *ChannelService, bs *BillingService,
	accountID int64, groupID int64,
	upstreamModel, requestedModel string,
	tokens UsageTokens,
	totalCost float64,
) {
	if usageLog == nil {
		return
	}
	model := upstreamModel
	if model == "" {
		model = requestedModel
	}
	usage := accountStatsUsageFromLog(usageLog, 1)
	usageLog.AccountStatsCost = resolveAccountStatsCostWithUsage(
		ctx, cs, bs, accountID, groupID, model, tokens, totalCost, usage,
	)
}

func accountStatsUsageFromLog(usageLog *UsageLog, defaultRequestCount int) accountStatsUsage {
	usage := accountStatsUsage{RequestCount: defaultRequestCount}
	if usage.RequestCount <= 0 {
		usage.RequestCount = 1
	}
	if usageLog == nil {
		return usage
	}
	if usageLog.BillingMode != nil {
		usage.BillingMode = BillingMode(strings.ToLower(strings.TrimSpace(*usageLog.BillingMode)))
	}
	if usageLog.BillingTier != nil {
		usage.BillingTier = strings.TrimSpace(*usageLog.BillingTier)
		usage.SizeTier = usage.BillingTier
	}
	if usageLog.ServiceTier != nil {
		usage.ServiceTier = strings.TrimSpace(*usageLog.ServiceTier)
	}
	if usageLog.ImageCount > 0 {
		usage.RequestCount = usageLog.ImageCount
		if usage.BillingMode == "" {
			usage.BillingMode = BillingModeImage
		}
		if usage.SizeTier == "" && usageLog.ImageSize != nil {
			usage.SizeTier = NormalizeImageBillingTierOrDefault(*usageLog.ImageSize)
		}
	}
	if usageLog.VideoCount > 0 || usage.BillingMode == BillingModeVideo {
		videoCount := usageLog.VideoCount
		if videoCount <= 0 {
			videoCount = 1
		}
		durationSeconds := 0
		if usageLog.VideoDurationSeconds != nil {
			durationSeconds = *usageLog.VideoDurationSeconds
		}
		durationSeconds = NormalizeVideoBillingDurationSecondsOrDefault(durationSeconds)
		usage.BillingMode = BillingModeVideo
		usage.RequestCount = videoCount
		usage.UsageUnits = float64(videoCount * durationSeconds)
		if usageLog.VideoResolution != nil {
			usage.SizeTier = NormalizeVideoBillingResolutionOrDefault(*usageLog.VideoResolution)
		}
	}
	return usage
}
