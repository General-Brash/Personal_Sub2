/**
 * Admin Model Plaza API endpoints
 * 广场展示管理：逐模型隐藏 / 置顶 / 排序 + “无账号支持”收敛开关，版本乐观锁写入。
 */

import { apiClient } from '../client'
import type { ModelPlazaAvailabilityState } from '@/api/modelPlaza'

/** 管理面板视角的单个模型（含被隐藏项，便于管理员查看自己隐藏了什么）。 */
export interface ModelPlazaAdminModel {
  /** 归一化键 "platform:model_id"，PUT 时原样回传。 */
  key: string
  model_id: string
  display_name: string
  platform: string
  availability_state: ModelPlazaAvailabilityState
  hidden: boolean
  pinned: boolean
  sort_order: number
  has_exact_pricing_standard?: boolean
}

/** 逐模型展示覆盖。 */
export interface ModelPlazaAdminOverride {
  hidden: boolean
  pinned: boolean
  sort_order: number
}

export interface ModelPlazaAdminSettings {
  models: ModelPlazaAdminModel[]
  /** 默认 true：收敛掉“无任何账号支持”的幽灵模型；关闭则回退展示全部播种。 */
  hide_no_account: boolean
  /** 内容寻址版本，写入需回传以做乐观锁。 */
  version: string
}

export interface UpdateModelPlazaAdminRequest {
  overrides: Record<string, ModelPlazaAdminOverride>
  hide_no_account: boolean
  version: string
}

/** 读取广场展示管理设置（含全部模型）。 */
export async function getModelPlazaAdmin(options?: { signal?: AbortSignal }): Promise<ModelPlazaAdminSettings> {
  const { data } = await apiClient.get<ModelPlazaAdminSettings>('/admin/model-plaza', { signal: options?.signal })
  return data
}

/** 以版本乐观锁写入广场展示管理设置，返回写入后的最新状态。 */
export async function updateModelPlazaAdmin(request: UpdateModelPlazaAdminRequest): Promise<ModelPlazaAdminSettings> {
  const { data } = await apiClient.put<ModelPlazaAdminSettings>('/admin/model-plaza', request)
  return data
}

export const modelPlazaAdminAPI = { getModelPlazaAdmin, updateModelPlazaAdmin }
export default modelPlazaAdminAPI

export type DefaultPricingMode = 'token' | 'per_request' | 'image' | 'video'
export type DefaultPricingMoneyField =
  | 'input_price' | 'output_price' | 'cache_write_price' | 'cache_write_1h_price'
  | 'cache_read_price' | 'image_input_price' | 'image_output_price' | 'per_request_price'
  | 'image_price_1k' | 'image_price_2k' | 'image_price_4k'
  | 'video_price_480p' | 'video_price_720p' | 'video_price_1080p'
export type DefaultPricingFields = Partial<Record<DefaultPricingMoneyField, number>> & { billing_mode?: DefaultPricingMode }
export type DefaultPricingPatch = Partial<Record<DefaultPricingMoneyField, number | null>> & { billing_mode?: DefaultPricingMode | null }
export interface DefaultPricingStatus {
  version: string
  loaded_version: string
  refresh_interval_seconds: number
  refresh_error?: string
}
export interface ModelDefaultPricingDetail extends DefaultPricingStatus {
  cache_fallback_to_input?: boolean
  requested_model_id: string
  pricing_key: string
  matched_model_id: string
  match_type: string
  source: string
  currency: 'USD'
  pricing_unit: string
  has_admin_override: boolean
  has_exact_system_standard: boolean
  effective_pricing_available: boolean
  system_baseline: {
    prices: DefaultPricingFields
    source: string
    matched_model_id: string
    match_type: string
    has_exact_standard: boolean
  }
  admin_override: DefaultPricingFields
  effective_pricing: DefaultPricingFields
  editable_fields: string[]
  supported_modes: DefaultPricingMode[]
}
export interface DefaultPricingOverrideList extends DefaultPricingStatus {
  items: { model_id: string; override: DefaultPricingFields }[]
  total: number
  page: number
  page_size: number
}

export async function getModelDefaultPricing(modelId: string, options?: { signal?: AbortSignal }): Promise<ModelDefaultPricingDetail> {
  const { data } = await apiClient.get<ModelDefaultPricingDetail>('/admin/model-plaza/pricing', { params: { model_id: modelId }, signal: options?.signal })
  return data
}
export async function listModelDefaultPricing(search = '', page = 1): Promise<DefaultPricingOverrideList> {
  const { data } = await apiClient.get<DefaultPricingOverrideList>('/admin/model-plaza/pricing/overrides', { params: { search, page, page_size: 20 } })
  return data
}
export async function saveModelDefaultPricing(modelId: string, version: string, patch: DefaultPricingPatch): Promise<ModelDefaultPricingDetail> {
  const { data } = await apiClient.put<ModelDefaultPricingDetail>('/admin/model-plaza/pricing', { model_id: modelId, version, patch })
  return data
}
export async function resetModelDefaultPricing(modelId: string, version: string): Promise<ModelDefaultPricingDetail> {
  const { data } = await apiClient.post<ModelDefaultPricingDetail>('/admin/model-plaza/pricing/reset', { model_id: modelId, version })
  return data
}