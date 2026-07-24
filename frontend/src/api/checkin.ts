import { apiClient } from './client'

export type UTCDateTimeString = `${string}Z`

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
}

export async function getCheckinStatus(month: string): Promise<CheckinStatus> {
  const { data } = await apiClient.get<CheckinStatus>('/user/check-in', {
    params: { month },
  })
  return data
}

export async function checkIn(idempotencyKey: string): Promise<CheckinResult> {
  const { data } = await apiClient.post<CheckinResult>('/user/check-in', undefined, {
    headers: { 'Idempotency-Key': idempotencyKey },
  })
  return data
}
