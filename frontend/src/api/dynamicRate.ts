import { apiClient } from './client'

export type DynamicRateMetric = 'tokens_m' | 'wallet_spend'
export type DynamicRateMode = 'text' | 'image' | 'batch_image'

export interface DynamicRateTier {
  id: string
  threshold: number | string
  factor: number
}

export interface DynamicRatePolicy {
  group_id: number
  enabled: boolean
  metric: DynamicRateMetric
  timezone: string
  reset_time: string
  tiers: DynamicRateTier[]
  included_modes: DynamicRateMode[]
  cache_token_policy: 'normalized'
  policy_version: number
  current_policy_version?: number
  effective_at: string
  expected_policy_version?: number
}

export interface DynamicRateUsageStatus {
  group_id: number
  user_id: number
  metric: DynamicRateMetric
  window_id: string
  current: number | string
  tier_id: string
  factor: number
  next_threshold?: number | string
  next_factor?: number
  reset_at: string
  policy_version: number
}

export interface DynamicRatePreview {
  policy: DynamicRatePolicy
  status?: DynamicRateUsageStatus
  window_start?: string
  window_end?: string
  dynamic_factor: number
  final_factor: number
}

export interface DynamicRatePreviewRequest extends Omit<DynamicRatePolicy, 'group_id' | 'policy_version' | 'effective_at'> {
  user_id: number
  static_factor?: number
  peak_factor?: number
  subscription_group?: boolean
}

export async function getDynamicRatePolicy(groupId: number): Promise<DynamicRatePolicy> {
  const { data } = await apiClient.get<DynamicRatePolicy>(`/admin/groups/${groupId}/dynamic-rate`)
  return data
}

export async function saveDynamicRatePolicy(groupId: number, policy: DynamicRatePolicy): Promise<DynamicRatePolicy> {
  const { data } = await apiClient.put<DynamicRatePolicy>(`/admin/groups/${groupId}/dynamic-rate`, {
    ...policy,
    expected_policy_version: policy.current_policy_version ?? 0
  })
  return data
}

export async function previewDynamicRate(groupId: number, request: DynamicRatePreviewRequest): Promise<DynamicRatePreview> {
  const { data } = await apiClient.post<DynamicRatePreview>(`/admin/groups/${groupId}/dynamic-rate/preview`, request)
  return data
}

export async function getDynamicRateStatus(groupId: number, userId: number, at?: string): Promise<{ status: DynamicRateUsageStatus; policy: DynamicRatePolicy }> {
  const { data } = await apiClient.get<{ status: DynamicRateUsageStatus; policy: DynamicRatePolicy }>(`/admin/groups/${groupId}/dynamic-rate/status`, { params: { user_id: userId, at } })
  return data
}
