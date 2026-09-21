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
  // Read-only enrichment joined from the channel group; never written back.
  group_name?: string
  group_platform?: string
  group_default_rate?: number | null
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
  preview_token: string
  expires_at: string
  policy_enabled: boolean
  policy_version: number
  tier: EntitlementTier
  user_ids: number[]
  affected_user_ids: number[]
  already_at_tier: number[]
  granted_group_ids: number[]
  revoked_group_ids: number[]
  preserved_manual_groups: boolean
  preserved_subscriptions: boolean
}

function arrayOrEmpty<T>(value: T[] | null | undefined): T[] {
  return Array.isArray(value) ? value : []
}

export function normalizeUserEntitlement(data: UserEntitlement): UserEntitlement {
  if (!data) throw new Error('Missing user entitlement response')
  return {
    ...data,
    allowed_groups: arrayOrEmpty(data.allowed_groups),
    tier_groups: arrayOrEmpty(data.tier_groups),
    sources: arrayOrEmpty(data.sources),
    manual_groups: arrayOrEmpty(data.manual_groups),
    subscription_groups: arrayOrEmpty(data.subscription_groups),
    default_rates: data.default_rates ?? {},
  }
}

export function normalizeEntitlementPreview(data: EntitlementChangePreview): EntitlementChangePreview {
  if (!data) throw new Error('Missing entitlement preview response')
  return {
    ...data,
    user_ids: arrayOrEmpty(data.user_ids),
    affected_user_ids: arrayOrEmpty(data.affected_user_ids),
    already_at_tier: arrayOrEmpty(data.already_at_tier),
    granted_group_ids: arrayOrEmpty(data.granted_group_ids),
    revoked_group_ids: arrayOrEmpty(data.revoked_group_ids),
  }
}

export async function getUserEntitlement(userId: number, signal?: AbortSignal): Promise<UserEntitlement> {
  const { data } = await apiClient.get<UserEntitlement>(`/admin/users/${userId}/entitlement`, { signal })
  return normalizeUserEntitlement(data)
}

export async function getEntitlementCatalog(signal?: AbortSignal): Promise<EntitlementCatalog> {
  const { data } = await apiClient.get<EntitlementCatalog>('/admin/entitlements/catalog', { signal })
  if (!data) throw new Error('Missing entitlement catalog response')
  return { ...data, tiers: arrayOrEmpty(data.tiers).map(tier => ({ ...tier, groups: arrayOrEmpty(tier.groups) })) }
}

export async function previewEntitlementChange(userIds: number[], tier: EntitlementTier, signal?: AbortSignal): Promise<EntitlementChangePreview> {
  const { data } = await apiClient.post<EntitlementChangePreview>('/admin/entitlements/preview', { user_ids: userIds, tier }, { signal })
  return normalizeEntitlementPreview(data)
}

export async function applyEntitlementChange(userIds: number[], tier: EntitlementTier, reason: string, requestId: string, previewToken: string): Promise<EntitlementChangeResult> {
  const { data } = await apiClient.post<EntitlementChangeResult>('/admin/entitlements/apply', {
    user_ids: userIds, tier, reason, request_id: requestId, preview_token: previewToken,
  })
  return data
}

export async function updateUserEntitlement(userId: number, tier: EntitlementTier, reason: string, requestId: string, previewToken: string): Promise<EntitlementChangeResult> {
  const { data } = await apiClient.put<EntitlementChangeResult>(`/admin/users/${userId}/entitlement`, {
    tier, reason, request_id: requestId, preview_token: previewToken,
  })
  return data
}

export type EntitlementPolicyUpdate = Omit<EntitlementTierPolicy, 'version'> & {
  expected_version: number
  reason: string
  request_id: string
}

export async function updateEntitlementPolicy(policy: EntitlementPolicyUpdate): Promise<EntitlementTierPolicy> {
  const { data } = await apiClient.put<EntitlementTierPolicy>('/admin/entitlements/catalog', policy)
  return { ...data, groups: arrayOrEmpty(data.groups) }
}
