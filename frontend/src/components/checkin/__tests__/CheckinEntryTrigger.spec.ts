import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import CheckinEntryTrigger from '../CheckinEntryTrigger.vue'
import { autoCheckIn, getCheckinPreference, getCheckinStatus, type CheckinStatus } from '@/api/checkin'

vi.mock('vue-router', () => ({ RouterLink: { template: '<a><slot /></a>' } }))

vi.mock('@/api/checkin', () => ({
  autoCheckIn: vi.fn(), getCheckinPreference: vi.fn(), getCheckinStatus: vi.fn(),
}))

function status(overrides: Partial<CheckinStatus> = {}): CheckinStatus {
  return {
    enabled: true, today_checked_in: false, current_streak_day: 0,
    next_reward_day: 1, next_reward_amount: '1.00000000',
    temporary_credit_available: '0.00000000', temporary_credit_earliest_expires_at: null,
    monthly_reward_total: '0.00000000', reward_tiers: [], calendar: [],
    business_period_id: 'server-window-20260912', ...overrides,
  }
}

beforeEach(() => {
  vi.resetAllMocks()
  sessionStorage.clear()
  vi.mocked(getCheckinStatus).mockResolvedValue(status())
  vi.mocked(getCheckinPreference).mockResolvedValue({
    auto_enabled: true, consent_valid: true, consent_fee_bps: 500,
    current_policy_version: 'policy-v1', current_fee_bps: 500,
  })
  vi.mocked(autoCheckIn).mockResolvedValue({
    already_checked_in: false, checkin_date: '2026-09-12', streak_day: 1, reward_day: 1,
    reward_amount: '0.95000000', temporary_credit_grant_id: 9,
    expires_at: '2026-09-12T16:00:00Z',
  })
})

describe('CheckinEntryTrigger', () => {
  it('does not claim a disabled or already-claimed server period', async () => {
    vi.mocked(getCheckinStatus).mockResolvedValue(status({ today_checked_in: true }))
    const wrapper = mount(CheckinEntryTrigger, { props: { userId: 11 } })
    await flushPromises()
    expect(getCheckinStatus).toHaveBeenCalledWith()
    expect(getCheckinPreference).not.toHaveBeenCalled()
    expect(autoCheckIn).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('requires an active, current consent before any automatic award request', async () => {
    vi.mocked(getCheckinPreference).mockResolvedValue({
      auto_enabled: true, consent_valid: false, consent_fee_bps: 500,
      current_policy_version: 'new-policy', current_fee_bps: 700,
    })
    const wrapper = mount(CheckinEntryTrigger, { props: { userId: 11 } })
    await flushPromises()
    expect(autoCheckIn).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('reuses a retry identity for the same user and period, without sharing it across users', async () => {
    const first = mount(CheckinEntryTrigger, { props: { userId: 11 } })
    await flushPromises()
    const key = vi.mocked(autoCheckIn).mock.calls[0][0]
    expect(first.emitted('completed')).toHaveLength(1)
    first.unmount()
    const retry = mount(CheckinEntryTrigger, { props: { userId: 11 } })
    await flushPromises()
    expect(vi.mocked(autoCheckIn).mock.calls[1][0]).toBe(key)
    retry.unmount()
    const anotherUser = mount(CheckinEntryTrigger, { props: { userId: 22 } })
    await flushPromises()
    expect(vi.mocked(autoCheckIn).mock.calls[2][0]).not.toBe(key)
    anotherUser.unmount()
  })

  it('does not submit a claim after the authenticated component has unmounted', async () => {
    let release!: (value: CheckinStatus) => void
    vi.mocked(getCheckinStatus).mockReturnValue(new Promise((resolve) => { release = resolve }))
    const wrapper = mount(CheckinEntryTrigger, { props: { userId: 11 } })
    wrapper.unmount()
    release(status())
    await flushPromises()
    expect(autoCheckIn).not.toHaveBeenCalled()
  })
})
