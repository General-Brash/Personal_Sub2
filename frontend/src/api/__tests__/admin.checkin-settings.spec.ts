import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put },
}))

import {
  getCheckinSettings,
  updateCheckinSettings,
  type CheckinSettings,
} from '@/api/admin/settings'

describe('admin check-in settings api', () => {
  const settings: CheckinSettings = {
    enabled: true,
    max_reward_day: 2,
    reward_tiers: [
      { day: 1, amount: '0.00000001', permanent_amount: '0.00000000' },
      { day: 2, amount: '999999999999.99999999', permanent_amount: '0.25000000' },
    ],
  }

  beforeEach(() => {
    get.mockReset()
    put.mockReset()
  })

  it('loads the complete reward tier contract without changing amount strings', async () => {
    get.mockResolvedValue({ data: settings })

    const result = await getCheckinSettings()

    expect(get).toHaveBeenCalledWith('/admin/settings/checkin')
    expect(result).toEqual(settings)
    expect(result.reward_tiers.map((tier) => typeof tier.amount)).toEqual(['string', 'string'])
    expect(result.reward_tiers.map((tier) => typeof tier.permanent_amount)).toEqual(['string', 'string'])
  })

  it('submits complete consecutive tiers with the original decimal strings', async () => {
    put.mockResolvedValue({ data: settings })

    const result = await updateCheckinSettings(settings)

    expect(put).toHaveBeenCalledWith('/admin/settings/checkin', settings)
    expect(result).toEqual(settings)
    expect(put.mock.calls[0]?.[1]?.reward_tiers).toEqual([
      { day: 1, amount: '0.00000001', permanent_amount: '0.00000000' },
      { day: 2, amount: '999999999999.99999999', permanent_amount: '0.25000000' },
    ])
  })
})
