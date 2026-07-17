import { apiClient } from './client'

export type UTCDateTimeString = `${string}Z`

export interface CheckinCalendarEntry {
  checkin_date: string
  streak_day: number
  reward_day: number
  reward_amount: string
}

export interface CheckinStatus {
  enabled: boolean
  today_checked_in: boolean
  current_streak_day: number
  next_reward_day: number
  next_reward_amount: string
  temporary_credit_available: string
  temporary_credit_earliest_expires_at: UTCDateTimeString | null
  monthly_reward_total: string
  calendar: CheckinCalendarEntry[]
}

export interface CheckinResult {
  already_checked_in: boolean
  checkin_date: string
  streak_day: number
  reward_day: number
  reward_amount: string
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
