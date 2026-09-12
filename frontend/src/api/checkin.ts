import { apiClient } from './client'

export type UTCDateTimeString = `${string}Z`
export type CheckinMode = 'direct' | 'direct-auto' | 'normal' | 'super'

export interface CheckinCalendarEntry {
  checkin_date: string
  streak_day: number
  reward_day: number
  reward_amount: string
  permanent_reward_amount?: string
}

export interface CheckinRewardTier {
  day: number
  amount: string
  permanent_amount: string
}

export interface CheckinStatus {
  enabled: boolean
  today_checked_in: boolean
  current_streak_day: number
  next_reward_day: number
  next_reward_amount: string
  next_permanent_reward_amount?: string
  temporary_credit_available: string
  temporary_credit_earliest_expires_at: UTCDateTimeString | null
  monthly_reward_total: string
  monthly_permanent_reward_total?: string
  reward_tiers: CheckinRewardTier[]
  calendar: CheckinCalendarEntry[]
  business_period_id?: string
  period_start_at?: UTCDateTimeString
  next_reset_at?: UTCDateTimeString
  mode?: CheckinMode
  auto_fee_bps?: number
  auto_enabled?: boolean
  consent_valid?: boolean
  policy_version?: string
  normal_enabled?: boolean
  normal_min_bps?: number
  normal_max_bps?: number
  super_enabled?: boolean
  super_min_bps?: number
  super_max_bps?: number
  super_cost?: string
  permanent_balance?: string
  can_afford_super?: boolean
}

export interface CheckinResult {
  already_checked_in: boolean
  checkin_date: string
  streak_day: number
  reward_day: number
  reward_amount: string
  permanent_reward_amount?: string
  temporary_credit_grant_id: number
  expires_at: UTCDateTimeString
  mode?: CheckinMode
  business_period_id?: string
  period_start_at?: UTCDateTimeString
  next_reset_at?: UTCDateTimeString
  base_reward_amount?: string
  multiplier_bps?: number
  super_cost?: string
  auto_fee_bps?: number
  policy_version?: string
  random_rule_version?: string
}

export interface CheckinPreference {
  auto_enabled?: boolean
  consent_policy_version?: string
  consent_fee_bps: number
  consented_at?: UTCDateTimeString
  consent_valid?: boolean
  current_policy_version: string
  current_fee_bps: number
}

export interface CheckinAdminRandomPolicy {
  enabled: boolean
  min_bps: number
  max_bps: number
}

export interface CheckinAdminSuperPolicy extends CheckinAdminRandomPolicy {
  cost: string
}

export interface CheckinAdminPendingRefresh {
  refresh_time: string
  effective_at: UTCDateTimeString
}

export interface CheckinAdminSettings {
  enabled: boolean
  max_reward_day: number
  reward_tiers: CheckinRewardTier[]
  version: string
  refresh_time: string
  auto_fee_bps: number
  reviewed: boolean
  normal: CheckinAdminRandomPolicy
  super: CheckinAdminSuperPolicy
  expected_version?: string
  pending_refresh?: CheckinAdminPendingRefresh | null
  next_reset_at?: UTCDateTimeString | null
}

export async function getCheckinSettingsV2(): Promise<CheckinAdminSettings> {
  const { data } = await apiClient.get<CheckinAdminSettings>('/admin/settings/checkin/v2')
  return data
}

export async function updateCheckinSettingsV2(settings: CheckinAdminSettings): Promise<CheckinAdminSettings> {
  const { data } = await apiClient.put<CheckinAdminSettings>('/admin/settings/checkin/v2', settings)
  return data
}

export async function getCheckinStatus(month?: string): Promise<CheckinStatus> {
  const { data } = await apiClient.get<CheckinStatus>('/user/check-in', {
    params: { month },
  })
  return data
}

export async function checkIn(idempotencyKey: string): Promise<CheckinResult> {
  const { data } = await apiClient.post<CheckinResult>('/user/check-in', undefined, { headers: { 'Idempotency-Key': idempotencyKey } })
  return data
}

export async function checkInMode(idempotencyKey: string, mode: Exclude<CheckinMode, 'direct-auto'>, policyVersion?: string): Promise<CheckinResult> {
  const { data } = await apiClient.post<CheckinResult>('/user/check-in', { mode, ...(policyVersion ? { policy_version: policyVersion } : {}) }, {
    headers: { 'Idempotency-Key': idempotencyKey },
  })
  return data
}

export async function autoCheckIn(idempotencyKey: string): Promise<CheckinResult> {
  const { data } = await apiClient.post<CheckinResult>('/user/check-in/auto', undefined, {
    headers: { 'Idempotency-Key': idempotencyKey },
  })
  return data
}

export async function getCheckinPreference(): Promise<CheckinPreference> {
  const { data } = await apiClient.get<CheckinPreference>('/user/check-in/preference')
  return data
}

export async function updateCheckinPreference(autoEnabled: boolean, accepted: boolean, policyVersion?: string, feeBps?: number): Promise<CheckinPreference> {
  const { data } = await apiClient.put<CheckinPreference>('/user/check-in/preference', {
    auto_enabled: autoEnabled,
    accepted,
    policy_version: policyVersion,
    fee_bps: feeBps,
  })
  return data
}
