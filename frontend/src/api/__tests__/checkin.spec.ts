import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
    put,
  },
}))

import { checkIn, getCheckinStatus } from '@/api/checkin'
import { getCheckinSettings, updateCheckinSettings } from '@/api/admin/settings'

describe('check-in api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    put.mockReset()
  })

  it('gets the selected month status from the user check-in endpoint', async () => {
    const response = {
      enabled: true,
      today_checked_in: false,
      current_streak_day: 3,
      next_reward_day: 4,
      next_reward_amount: '1.00000000',
      next_permanent_reward_amount: '0.25000000',
      temporary_credit_available: '2.00000000',
      temporary_credit_earliest_expires_at: '2026-07-13T16:00:00Z',
      monthly_reward_total: '3.00000000',
      monthly_permanent_reward_total: '0.50000000',
      reward_tiers: [
        { day: 1, amount: '1.00000000', permanent_amount: '0.00000000' },
        { day: 2, amount: '1.50000000', permanent_amount: '0.25000000' },
      ],
      calendar: [
        {
          checkin_date: '2026-07-12',
          streak_day: 3,
          reward_day: 3,
          reward_amount: '1.00000000',
          permanent_reward_amount: '0.25000000',
        },
      ],
    }
    get.mockResolvedValue({ data: response })

    const result = await getCheckinStatus('2026-07')

    expect(get).toHaveBeenCalledWith('/user/check-in', { params: { month: '2026-07' } })
    expect(result).toEqual(response)
  })

  it('posts check-in with the caller supplied idempotency key', async () => {
    const response = {
      already_checked_in: false,
      checkin_date: '2026-07-13',
      streak_day: 4,
      reward_day: 4,
      reward_amount: '1.00000000',
      permanent_reward_amount: '0.25000000',
      temporary_credit_grant_id: 42,
      expires_at: '2026-07-13T16:00:00Z',
    }
    post.mockResolvedValue({ data: response })

    const result = await checkIn('check-in-2026-07-13')

    expect(post).toHaveBeenCalledWith('/user/check-in', undefined, {
      headers: { 'Idempotency-Key': 'check-in-2026-07-13' },
    })
    expect(result).toEqual(response)
    expect(Object.keys(result).sort()).toEqual([
      'already_checked_in',
      'checkin_date',
      'expires_at',
      'permanent_reward_amount',
      'reward_amount',
      'reward_day',
      'streak_day',
      'temporary_credit_grant_id',
    ])
  })
})

describe('admin check-in settings api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    put.mockReset()
  })

  it('gets the dedicated check-in settings resource', async () => {
    const response = {
      enabled: true,
      max_reward_day: 3,
      reward_tiers: [
		{ day: 1, amount: '1.00000000', permanent_amount: '0.00000000' },
		{ day: 2, amount: '2.00000000', permanent_amount: '0.25000000' },
		{ day: 3, amount: '3.00000000', permanent_amount: '0.50000000' },
      ],
    }
    get.mockResolvedValue({ data: response })

    const result = await getCheckinSettings()

    expect(get).toHaveBeenCalledWith('/admin/settings/checkin')
    expect(result).toEqual(response)
  })

  it('updates all check-in settings atomically through the dedicated resource', async () => {
    const request = {
      enabled: false,
      max_reward_day: 2,
      reward_tiers: [
		{ day: 1, amount: '1.00000000', permanent_amount: '0.00000000' },
		{ day: 2, amount: '2.00000000', permanent_amount: '0.25000000' },
      ],
    }
    put.mockResolvedValue({ data: request })

    const result = await updateCheckinSettings(request)

    expect(put).toHaveBeenCalledWith('/admin/settings/checkin', request)
    expect(result).toEqual(request)
  })
})
