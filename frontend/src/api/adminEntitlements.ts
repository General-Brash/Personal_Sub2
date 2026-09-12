import { apiClient } from './client'
import type { AdminCapabilities } from './adminPermissions'

export type EntitlementTier = 'standard' | 'premium'

export interface EntitlementSource {
  source: string
  tier: EntitlementTier
  group_id?: number
  rate?: number
  expires_at?: string | null
  version: number
  explain: string
}

export interface UserEntitlement {
  user_id: number
  tier: EntitlementTier
  tier_enabled: boolean
  version: number
  allowed_groups: number[]
  tier_groups: number[]
  default_rates: Record<string, number>
  sources: EntitlementSource[]
  manual_groups: number[]
  subscription_groups: number[]
  capabilities?: AdminCapabilities
}

export interface EntitlementTierGroupPolicy {
  group_id: number
  rate_multiplier?: number | null
  source: string
}

export interface EntitlementTierPolicy {
  tier: EntitlementTier
  display_name: string
  enabled: boolean
  version: number
  groups: EntitlementTierGroupPolicy[]
}

export interface EntitlementCatalog {
  tiers: EntitlementTierPolicy[]
  capabilities: AdminCapabilities
}

export interface EntitlementChangeResult {
  tier: EntitlementTier
  requested: number
  changed: number
  unchanged: number
  version: number
  idempotent: boolean
}

export interface EntitlementChangePreview {
  tier: EntitlementTier
  user_ids: number[]
  affected_user_ids: number[]
  already_at_tier: number[]
  granted_group_ids?: number[]
  revoked_group_ids?: number[]
  preserved_manual_groups: boolean
  preserved_subscriptions: boolean
}

export async function getUserEntitlement(userId: number): Promise<UserEntitlement> {
  const { data } = await apiClient.get<UserEntitlement>(`/admin/users/${userId}/entitlement`)
  return data
}

export async function getEntitlementCatalog(): Promise<EntitlementCatalog> {
  const { data } = await apiClient.get<EntitlementCatalog>('/admin/entitlements/catalog')
  return data
}

export async function previewEntitlementChange(userIds: number[], tier: EntitlementTier): Promise<EntitlementChangePreview> {
  const { data } = await apiClient.post<EntitlementChangePreview>('/admin/entitlements/preview', { user_ids: userIds, tier })
  return data
}

export async function applyEntitlementChange(userIds: number[], tier: EntitlementTier, reason: string, requestId: string = crypto.randomUUID()): Promise<EntitlementChangeResult> {
  const { data } = await apiClient.post<EntitlementChangeResult>('/admin/entitlements/apply', { user_ids: userIds, tier, reason, request_id: requestId })
  return data
}

export async function updateUserEntitlement(userId: number, tier: EntitlementTier, reason: string, requestId: string = crypto.randomUUID()): Promise<EntitlementChangeResult> {
  const { data } = await apiClient.put<EntitlementChangeResult>(`/admin/users/${userId}/entitlement`, { tier, reason, request_id: requestId })
  return data
}

export async function updateEntitlementPolicy(policy: Omit<EntitlementTierPolicy, 'version'> & { expected_version: number; reason: string; request_id?: string }): Promise<EntitlementTierPolicy> {
  const { data } = await apiClient.put<EntitlementTierPolicy>('/admin/entitlements/catalog', {
    ...policy,
    request_id: policy.request_id || crypto.randomUUID(),
  })
  return data
}
