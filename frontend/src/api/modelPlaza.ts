/**
 * Model Plaza API（公开端点，可匿名访问）
 * 以分组为中心的模型价目：分组信息 + 模型渠道定价 + LiteLLM 官方参考价。
 * 带 token 请求时后端会额外返回专属分组与用户专属倍率。
 */

import { apiClient } from './client'
import type { UserPricingInterval, UserSupportedModelPricing } from './channels'

/** 官方参考价（USD per token，与计费目录同源；字段缺失 = 目录未覆盖）。 */
export interface PlazaOfficialPricing {
  input_price: number | null
  output_price: number | null
  /** 5m 缓存写入（= LiteLLM cache_creation）。 */
  cache_write_price: number | null
  /** 1h 缓存写入（LiteLLM cache_creation_above_1hr），多数模型缺失。 */
  cache_write_1h_price?: number | null
  cache_read_price: number | null
  /** 官方长上下文阶梯（多档模型才有），不受分组开关影响。 */
  intervals?: UserPricingInterval[]
}

/**
 * 多档时的计价基准：
 * - whole_request：整单按所在档单价计价（目录阶梯、渠道区间）；
 * - marginal：仅超出阈值的部分按该档单价计价（平台旧规则）。
 */
export type PlazaLongContextBasis = 'whole_request' | 'marginal'

/** 分时倍率时段：配置时区当天 [start_time, end_time) 内整单实付乘 multiplier。 */
export interface PlazaTimePricingPeriod {
  start_time: string
  end_time: string
  multiplier: number
}

/** 计费会生效的分时倍率（仅倍率 ≠ 1 的时段，已按开始时间升序）。 */
export interface PlazaTimePricing {
  /** IANA 时区名，如 Asia/Shanghai。 */
  timezone: string
  /** true 时时段仅周一至周五生效，周末整天按标准价计费。 */
  weekdays_only?: boolean
  periods: PlazaTimePricingPeriod[]
}

export interface PlazaModel {
  name: string
  platform: string
  /** 实收口径的展示定价：多档时 intervals 为各档绝对单价（已由计费服务折算）；均为标准时段价。 */
  pricing: UserSupportedModelPricing | null
  official_pricing: PlazaOfficialPricing | null
  /** 仅多档模型返回。 */
  long_context_basis?: PlazaLongContextBasis
  /** 仅配置了分时倍率的模型返回。 */
  time_pricing?: PlazaTimePricing
}

export interface ModelPlazaGroup {
  id: number
  name: string
  description: string
  platform: string
  /** 'standard' | 'subscription' */
  subscription_type: string
  rate_multiplier: number
  /** 登录且管理员为该用户配了专属倍率时返回；生效倍率 = user_rate ?? rate_multiplier。 */
  user_rate_multiplier?: number
  peak_rate_enabled: boolean
  peak_start: string
  peak_end: string
  peak_rate_multiplier: number
  is_exclusive: boolean
  /** 生图独立倍率：true 时图片计费模型的实付倍率取 image_rate_multiplier，不取分组/专属倍率。 */
  image_rate_independent: boolean
  image_rate_multiplier: number
  /** 分组是否启用长上下文阶梯计费；false 时实付列只展示最低档，官方阶梯仅供参考。 */
  long_context_pricing_enabled: boolean
  models: PlazaModel[]
}

export interface ModelPlazaResponse {
  /** 管理员配置的全局价格说明（Markdown）。 */
  description: string
  groups: ModelPlazaGroup[]
}

/** 获取模型广场数据。开关未启用时后端返回 404。 */
export async function getModelPlaza(options?: { signal?: AbortSignal }): Promise<ModelPlazaResponse> {
  const { data } = await apiClient.get<ModelPlazaResponse>('/model-plaza', {
    signal: options?.signal
  })
  return data
}

export const modelPlazaAPI = { getModelPlaza, getModelPlazaV2 }

export default modelPlazaAPI

/** W10 v2 independent catalog response. */
export type ModelPlazaAvailabilityState =
  | 'catalog_only'
  | 'eligible'
  | 'temporarily_unavailable'
  | 'not_entitled'
  | 'unknown'

export interface ModelPlazaV2QuoteInterval {
  min_tokens?: number
  max_tokens?: number | null
  tier_label?: string
  input_per_million?: number | null
  output_per_million?: number | null
  cache_write_per_million?: number | null
  cache_write_1h_per_million?: number | null
  cache_read_per_million?: number | null
  per_request_price?: number | null
}

export interface ModelPlazaV2PriceCondition {
  pattern: string
  pricing_unit: string
  billing_mode?: string
  input_per_million?: number | null
  output_per_million?: number | null
  cache_write_per_million?: number | null
  cache_write_1h_per_million?: number | null
  cache_read_per_million?: number | null
  per_request_price?: number | null
  intervals?: ModelPlazaV2QuoteInterval[]
}

export interface ModelPlazaV2PriceQuote {
  peak_rate_multiplier?: number | null
  quote_version: string
  priced_at: string
  pricing_unit: string
  currency: string
  input_per_million?: number | null
  output_per_million?: number | null
  cache_write_per_million?: number | null
  cache_write_1h_per_million?: number | null
  cache_read_per_million?: number | null
  per_request_price?: number | null
  intervals?: ModelPlazaV2QuoteInterval[]
  dynamic_factor_status: 'not_configured' | 'unavailable' | 'unknown' | 'invalid' | 'available'
  dynamic_factor?: { factor: number; source: string; version?: string; details?: Record<string, string> } | null
  effective_rate_multiplier?: number | null
  rate_source?: string
  image_rate_independent?: boolean
  image_rate_multiplier?: number | null
  channel_time_multiplier?: number | null
  price_conditions?: ModelPlazaV2PriceCondition[]
  source?: string
  unknown_fields?: string[]
}

export interface ModelPlazaV2GroupChoice {
  group_id: number
  group_name: string
  platform: string
  route_kind: string
  availability_state: ModelPlazaAvailabilityState
  reason_code?: string
  schedulable: boolean
  price_quote?: ModelPlazaV2PriceQuote | null
  quote_version?: string
  priced_at?: string
}

export interface ModelPlazaV2Model {
  model_id: string
  display_name: string
  platform: string
  capabilities?: string[]
  supported_endpoints?: string[]
  context_window?: number | null
  availability_state: ModelPlazaAvailabilityState
  eligibility: ModelPlazaAvailabilityState
  reason_code?: string
  user_group_choices: ModelPlazaV2GroupChoice[]
  source_versions?: Record<string, string>
  quote_version?: string
  priced_at?: string
}

export interface ModelPlazaV2Response {
  models: ModelPlazaV2Model[]
  generated_at: string
}

/** Fetch the versioned independent catalog. Route registration is owned by the integrator. */
export async function getModelPlazaV2(options?: { signal?: AbortSignal }): Promise<ModelPlazaV2Response> {
  const { data } = await apiClient.get<ModelPlazaV2Response>('/model-plaza/v2', {
    signal: options?.signal
  })
  return data
}
