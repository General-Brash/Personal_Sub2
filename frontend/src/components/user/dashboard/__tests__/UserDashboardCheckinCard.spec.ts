import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { CheckinStatus } from '@/api/checkin'
import UserDashboardCheckinCard from '../UserDashboardCheckinCard.vue'

const { getCheckinStatus } = vi.hoisted(() => ({
  getCheckinStatus: vi.fn(),
}))

vi.mock('@/api/checkin', () => ({ getCheckinStatus }))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, string | number>) =>
        values?.count != null ? `${key}:${values.count}` : values?.day != null ? `${key}:${values.day}` : key,
    }),
  }
})

const status: CheckinStatus = {
  enabled: true,
  today_checked_in: false,
  current_streak_day: 8,
  next_reward_day: 7,
  next_reward_amount: '2.50000000',
  temporary_credit_available: '5.25000000',
  temporary_credit_earliest_expires_at: '2026-07-14T16:00:00Z',
  monthly_reward_total: '12.50000000',
  calendar: [],
}

describe('UserDashboardCheckinCard', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-14T04:00:00.000Z'))
    getCheckinStatus.mockReset()
    getCheckinStatus.mockResolvedValue(status)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('shows only check-in summary and a details entry point', async () => {
    const wrapper = mount(UserDashboardCheckinCard, {
      global: {
        stubs: {
          Icon: { template: '<i />' },
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        },
      },
    })
    await flushPromises()

    expect(wrapper.get('[data-test="today-status"]').text()).toContain('checkin.checkIn')
    expect(wrapper.get('[data-test="dashboard-streak"]').text()).toContain('8')
    expect(wrapper.get('[data-test="dashboard-reward"]').text()).toContain('$2.50')
    expect(wrapper.get('[data-test="dashboard-temporary-credit"]').text()).toContain('$5.25')
    expect(wrapper.get('[data-test="dashboard-earliest-expiry"]').attributes('data-expires-at')).toBe('2026-07-14T16:00:00Z')
    expect(wrapper.get('[data-test="checkin-details-link"]').attributes('href')).toBe('/check-in')
    expect(wrapper.find('[data-test="check-in-action"]').exists()).toBe(false)
    expect(getCheckinStatus).toHaveBeenCalledWith('2026-07')
  })

  it.each([
    ['0.00000001', '$0.00'],
    ['0.00000101', '$0.00'],
    ['999999999999.99999999', '$1000000000000.00'],
  ])(
    'renders the server summary amount as a precise two-place value: %s',
    async (rewardAmount, expected) => {
      getCheckinStatus.mockResolvedValueOnce({ ...status, next_reward_amount: rewardAmount })
      const wrapper = mount(UserDashboardCheckinCard, {
        global: {
          stubs: {
            Icon: { template: '<i />' },
            RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
          },
        },
      })
      await flushPromises()

      expect(wrapper.get('[data-test="dashboard-reward"]').text()).toContain(expected)
    }
  )

  it('rounds upper-bound amounts and keeps them inside two-column metrics', async () => {
    const upperBoundAmount = '999999999999.99999999'
    getCheckinStatus.mockResolvedValueOnce({
      ...status,
      next_reward_amount: upperBoundAmount,
      temporary_credit_available: upperBoundAmount,
    })
    const wrapper = mount(UserDashboardCheckinCard, {
      global: {
        stubs: {
          Icon: { template: '<i />' },
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        },
      },
    })
    await flushPromises()

    const reward = wrapper.get('[data-test="dashboard-reward"]')
    const temporaryCredit = wrapper.get('[data-test="dashboard-temporary-credit"]')
    expect(reward.text()).toBe('$1000000000000.00')
    expect(temporaryCredit.text()).toBe('$1000000000000.00')

    for (const amount of [reward, temporaryCredit]) {
      expect(amount.classes()).toEqual(expect.arrayContaining([
        'min-w-0',
        'max-w-full',
        'break-all',
        'text-sm',
        'leading-5',
      ]))
      expect(amount.element.parentElement?.classList.contains('min-w-0')).toBe(true)
    }
  })

  it('keeps historical credit information visible without a claim action when disabled', async () => {
    getCheckinStatus.mockResolvedValueOnce({ ...status, enabled: false })
    const wrapper = mount(UserDashboardCheckinCard, {
      global: {
        stubs: {
          Icon: { template: '<i />' },
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        },
      },
    })
    await flushPromises()

    expect(wrapper.get('[data-test="today-status"]').text()).toContain('checkin.disabled')
    expect(wrapper.get('[data-test="dashboard-temporary-credit"]').text()).toContain('$5.25')
    expect(wrapper.find('[data-test="check-in-action"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="checkin-details-link"]').attributes('href')).toBe('/check-in')
  })

  it('uses dashboard-provided status without issuing a duplicate request', async () => {
    const wrapper = mount(UserDashboardCheckinCard, {
      props: { status, loading: false },
      global: {
        stubs: {
          Icon: { template: '<i />' },
          RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        },
      },
    })
    await flushPromises()

    expect(getCheckinStatus).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="dashboard-temporary-credit"]').text()).toContain('$5.25')
  })
})
