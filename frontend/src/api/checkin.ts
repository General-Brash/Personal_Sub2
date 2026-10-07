import { apiClient } from './client'

export type UTCDateTimeString = `${string}Z`
export type CheckinMode = 'direct' | 'direct-auto'
export type CheckinStoredMode = CheckinMode | 'normal' | 'super'

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
  mode?: CheckinStoredMode
  auto_fee_bps?: number
  auto_enabled?: boolean
  consent_valid?: boolean
  /** 站点为所有用户强制开启自动签到（手续费恒为 0），用户无需自行开启或同意。 */
  auto_forced_by_admin?: boolean
  policy_version?: string
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
  mode?: CheckinStoredMode
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
  /** 站点强制开启时为 true，前端据此区分「站点强制」与「用户自选」。 */
  auto_forced_by_admin?: boolean
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
  /** 为所有用户强制开启自动签到；开启时手续费必须为 0。 */
  auto_force_all: boolean
  // Deprecated read-only compatibility fields. They are not accepted on writes.
  reviewed?: boolean
  normal?: CheckinAdminRandomPolicy
  super?: CheckinAdminSuperPolicy
  pending_refresh?: CheckinAdminPendingRefresh | null
  next_reset_at?: UTCDateTimeString | null
}

export interface CheckinAdminSettingsUpdate {
  enabled: boolean
  max_reward_day: number
  reward_tiers: CheckinRewardTier[]
  version: string
  refresh_time: string
  auto_fee_bps: number
  auto_force_all: boolean
  expected_version?: string
}

export async function getCheckinSettingsV2(): Promise<CheckinAdminSettings> {
  const { data } = await apiClient.get<CheckinAdminSettings>('/admin/settings/checkin/v2')
  return data
}

export async function updateCheckinSettingsV2(settings: CheckinAdminSettingsUpdate): Promise<CheckinAdminSettings> {
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
