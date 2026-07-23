import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { CheckinResult, CheckinStatus } from '@/api/checkin'
import CheckInView from '../CheckInView.vue'

const { checkIn, getCheckinStatus, refreshUser, showError, showSuccess } = vi.hoisted(() => ({
  checkIn: vi.fn(),
  getCheckinStatus: vi.fn(),
  refreshUser: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/checkin', () => ({
  checkIn,
  getCheckinStatus,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({ refreshUser }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, values?: Record<string, string | number>) =>
        values?.count != null ? `${key}:${values.count}` : values?.day != null ? `Day ${values.day}` : key,
    }),
  }
})

const baseStatus = (overrides: Partial<CheckinStatus> = {}): CheckinStatus => ({
  enabled: true,
  today_checked_in: false,
  current_streak_day: 8,
  next_reward_day: 7,
  next_reward_amount: '2.50000000',
  next_permanent_reward_amount: '0.25000000',
  temporary_credit_available: '5.25000000',
  temporary_credit_earliest_expires_at: '2026-07-14T16:00:00Z',
  monthly_reward_total: '12.50000000',
  monthly_permanent_reward_total: '0.75000000',
  calendar: [
    {
      checkin_date: '2026-07-03',
      streak_day: 3,
      reward_day: 3,
      reward_amount: '1.25000000',
      permanent_reward_amount: '0.12500000',
    },
  ],
  ...overrides,
})

const checkinResult: CheckinResult = {
  already_checked_in: false,
  checkin_date: '2026-07-14',
  streak_day: 8,
  reward_day: 7,
  reward_amount: '2.50000000',
  permanent_reward_amount: '0.25000000',
  temporary_credit_grant_id: 42,
  expires_at: '2026-07-14T16:00:00Z',
}

const mountView = async () => {
  const wrapper = mount(CheckInView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: { template: '<i />' },
      },
    },
  })
  await flushPromises()
  return wrapper
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('CheckInView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-14T04:00:00.000Z'))
    localStorage.clear()
    checkIn.mockReset()
    getCheckinStatus.mockReset()
    refreshUser.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    getCheckinStatus.mockResolvedValue(baseStatus())
    refreshUser.mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('renders a fixed 42-cell calendar using server checkin dates and rewards', async () => {
    const wrapper = await mountView()

    expect(wrapper.findAll('[data-test="calendar-cell"]')).toHaveLength(42)
    expect(wrapper.get('[data-test="current-streak"]').text()).toContain('8')
    expect(wrapper.get('[data-test="next-reward"]').text()).not.toContain('Day 7')
    expect(wrapper.get('[data-test="next-reward"]').text()).not.toContain('/')
    expect(wrapper.get('[data-test="next-temporary-reward"]').text()).toBe('$2.50')
    expect(wrapper.get('[data-test="next-permanent-reward"]').text()).toBe('$0.25')
    expect(wrapper.get('[data-test="temporary-credit"]').text()).toContain('$5.25')
    expect(wrapper.get('[data-test="monthly-temporary-reward-total"]').text()).toBe('$12.50')
    expect(wrapper.get('[data-test="monthly-permanent-reward-total"]').text()).toBe('$0.75')

    const checkedDay = wrapper.get('[data-test="calendar-cell"][data-date="2026-07-03"]')
    expect(checkedDay.text()).toContain('$1.25')
    expect(checkedDay.text()).toContain('$0.13')
  })

  it('keeps the five summary cards equal-height and gives reward types distinct accessible colors', async () => {
    const wrapper = await mountView()
    const cards = wrapper.findAll('[data-test="checkin-stat-card"]')

    expect(cards).toHaveLength(5)
    for (const card of cards) {
      expect(card.classes()).toEqual(expect.arrayContaining(['min-w-0', 'flex']))
    }
    for (const selector of [
      '[data-test="next-temporary-reward"]',
      '[data-test="monthly-temporary-reward-total"]',
    ]) {
      expect(wrapper.get(selector).classes()).toEqual(expect.arrayContaining([
        'text-lg',
        'text-emerald-600',
        'dark:text-emerald-400',
      ]))
    }
    for (const selector of [
      '[data-test="next-permanent-reward"]',
      '[data-test="monthly-permanent-reward-total"]',
    ]) {
      expect(wrapper.get(selector).classes()).toEqual(expect.arrayContaining([
        'text-lg',
        'text-indigo-600',
        'dark:text-indigo-400',
      ]))
    }
    expect(wrapper.get('[data-test="next-reward"]').text()).toContain('checkin.temporaryReward:')
    expect(wrapper.get('[data-test="next-reward"]').text()).toContain('checkin.permanentReward:')
  })

  it('keeps long reward amounts complete and allows them to wrap inside their cards', async () => {
    const upperBoundReward = '999999999999.99999999'
    getCheckinStatus.mockResolvedValueOnce(baseStatus({
      next_reward_amount: upperBoundReward,
      next_permanent_reward_amount: upperBoundReward,
      monthly_reward_total: upperBoundReward,
      monthly_permanent_reward_total: upperBoundReward,
      temporary_credit_available: upperBoundReward,
    }))
    const wrapper = await mountView()
    const expectedAmount = '$1000000000000.00'
    const rewardValues = [
      wrapper.get('[data-test="next-temporary-reward"]'),
      wrapper.get('[data-test="next-permanent-reward"]'),
      wrapper.get('[data-test="monthly-temporary-reward-total"]'),
      wrapper.get('[data-test="monthly-permanent-reward-total"]'),
    ]

    for (const value of rewardValues) {
      expect(value.text()).toBe(expectedAmount)
      expect(value.classes()).toEqual(expect.arrayContaining(['min-w-0', 'max-w-full', 'break-all', 'text-lg']))
      for (const forbiddenClass of ['truncate', 'overflow-hidden', 'text-ellipsis']) {
        expect(value.classes()).not.toContain(forbiddenClass)
      }
    }

    expect(wrapper.get('[data-test="next-reward"]').classes()).toContain('flex-wrap')
    expect(wrapper.get('[data-test="monthly-reward-total"]').classes()).toContain('flex-wrap')
    expect(wrapper.get('[data-test="temporary-credit"]').text()).toBe(expectedAmount)
  })

  it('rounds calendar rewards to two places and keeps them constrained inside narrow cells', async () => {
    const upperBoundReward = '999999999999.99999999'
    getCheckinStatus.mockResolvedValueOnce(baseStatus({
      calendar: [
        ...baseStatus().calendar,
        {
          checkin_date: '2026-07-04',
          streak_day: 4,
          reward_day: 4,
          reward_amount: upperBoundReward,
          permanent_reward_amount: '0.00000000',
        },
      ],
    }))
    const wrapper = await mountView()

    const regularCell = wrapper.get('[data-test="calendar-cell"][data-date="2026-07-03"]')
    const upperBoundCell = wrapper.get('[data-test="calendar-cell"][data-date="2026-07-04"]')
    expect(regularCell.get('[data-test="calendar-reward"]').text()).toContain('$1.25')
    expect(upperBoundCell.get('[data-test="calendar-reward"]').text()).toContain('$1000000000000.00')

    for (const cell of [regularCell, upperBoundCell]) {
      const reward = cell.get('[data-test="calendar-reward"]')
      expect(cell.classes()).toContain('min-w-0')
      expect(reward.classes()).toEqual(expect.arrayContaining([
        'block',
        'w-full',
        'min-w-0',
        'max-w-full',
        'break-all',
        'text-[10px]',
        'leading-tight',
      ]))
    }
  })

  it('disables check-in when the server reports today is already checked in or the policy is disabled', async () => {
    getCheckinStatus.mockResolvedValueOnce(baseStatus({ today_checked_in: true }))
    const checkedInWrapper = await mountView()
    expect(checkedInWrapper.get('[data-test="check-in-button"]').attributes('disabled')).toBeDefined()

    getCheckinStatus.mockResolvedValueOnce(baseStatus({ enabled: false }))
    const disabledWrapper = await mountView()
    expect(disabledWrapper.find('[data-test="check-in-button"]').exists()).toBe(false)
  })

  it.each([
    ['0.00000001', '$0.00'],
    ['0.00000101', '$0.00'],
    ['999999999999.99999999', '$1000000000000.00'],
  ])(
    'renders the server reward amount as a precise two-place value: %s',
    async (rewardAmount, expected) => {
      getCheckinStatus.mockResolvedValueOnce(baseStatus({ next_reward_amount: rewardAmount }))
      const wrapper = await mountView()

      expect(wrapper.get('[data-test="next-temporary-reward"]').text()).toBe(expected)
    }
  )

  it('loads previous and next months across a year boundary and keeps 42 cells', async () => {
    vi.setSystemTime(new Date('2026-01-15T04:00:00.000Z'))
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-month"]').text()).toBe('2026-01')
    await wrapper.get('[data-test="previous-month-button"]').trigger('click')
    await flushPromises()
    expect(getCheckinStatus).toHaveBeenLastCalledWith('2025-12')
    expect(wrapper.get('[data-test="current-month"]').text()).toBe('2025-12')
    expect(wrapper.findAll('[data-test="calendar-cell"]')).toHaveLength(42)

    await wrapper.get('[data-test="next-month-button"]').trigger('click')
    await flushPromises()
    expect(getCheckinStatus).toHaveBeenLastCalledWith('2026-01')
    expect(wrapper.get('[data-test="current-month"]').text()).toBe('2026-01')
    expect(wrapper.findAll('[data-test="calendar-cell"]')).toHaveLength(42)
  })

  it('ignores an older month response that resolves after the latest selection', async () => {
    const august = deferred<CheckinStatus>()
    const september = deferred<CheckinStatus>()
    const wrapper = await mountView()
    getCheckinStatus
      .mockImplementationOnce(() => august.promise)
      .mockImplementationOnce(() => september.promise)

    await wrapper.get('[data-test="next-month-button"]').trigger('click')
    await wrapper.get('[data-test="next-month-button"]').trigger('click')
    expect(getCheckinStatus).toHaveBeenNthCalledWith(2, '2026-08')
    expect(getCheckinStatus).toHaveBeenNthCalledWith(3, '2026-09')

    september.resolve(baseStatus({
      monthly_reward_total: '9.00000000',
      calendar: [{
        checkin_date: '2026-09-09',
        streak_day: 9,
        reward_day: 7,
        reward_amount: '9.00000000',
        permanent_reward_amount: '0.00000000',
      }],
    }))
    await flushPromises()
    expect(wrapper.get('[data-test="current-month"]').text()).toBe('2026-09')
    expect(wrapper.get('[data-test="calendar-cell"][data-date="2026-09-09"]').text()).toContain('$9.00')

    august.resolve(baseStatus({
      monthly_reward_total: '8.00000000',
      calendar: [{
        checkin_date: '2026-08-08',
        streak_day: 8,
        reward_day: 7,
        reward_amount: '8.00000000',
        permanent_reward_amount: '0.00000000',
      }],
    }))
    await flushPromises()
    expect(wrapper.get('[data-test="current-month"]').text()).toBe('2026-09')
    expect(wrapper.find('[data-test="calendar-cell"][data-date="2026-08-08"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="calendar-cell"][data-date="2026-09-09"]').text()).toContain('$9.00')
  })

  it('reuses an idempotency key and refreshes from server data after a successful check-in', async () => {
    const refreshed = baseStatus({
      today_checked_in: true,
      current_streak_day: checkinResult.streak_day,
      temporary_credit_available: '7.75000000',
      monthly_reward_total: '15.00000000',
      calendar: [
        ...baseStatus().calendar,
        {
          checkin_date: checkinResult.checkin_date,
          streak_day: checkinResult.streak_day,
          reward_day: checkinResult.reward_day,
          reward_amount: checkinResult.reward_amount,
          permanent_reward_amount: checkinResult.permanent_reward_amount,
        },
      ],
    })
    const refresh = deferred<CheckinStatus>()
    getCheckinStatus.mockResolvedValueOnce(baseStatus()).mockImplementationOnce(() => refresh.promise)
    checkIn.mockResolvedValue(checkinResult)

    const wrapper = await mountView()
    await wrapper.get('[data-test="check-in-button"]').trigger('click')
    await flushPromises()

    expect(checkIn).toHaveBeenCalledWith(expect.stringMatching(/^check-in-/))
    const firstKey = checkIn.mock.calls[0][0]
    expect(localStorage.getItem('daily-checkin-idempotency-key')).toBe(firstKey)
    expect(getCheckinStatus).toHaveBeenCalledTimes(2)
    expect(getCheckinStatus).toHaveBeenNthCalledWith(2, '2026-07')
    expect(wrapper.get('[data-test="check-in-button"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="calendar-cell"][data-date="2026-07-14"]').text()).toContain('$2.50')
    expect(wrapper.get('[data-test="checkin-result-temporary"]').text()).toContain('$2.50')
    expect(wrapper.get('[data-test="checkin-result-permanent"]').text()).toContain('$0.25')
    expect(wrapper.get('[data-test="temporary-credit"]').text()).toContain('$5.25')
    expect(refreshUser).toHaveBeenCalledTimes(1)

    refresh.resolve(refreshed)
    await flushPromises()
    expect(wrapper.get('[data-test="temporary-credit"]').text()).toContain('$7.75')
    expect(showSuccess).toHaveBeenCalled()
  })

  it('keeps the successful result when refreshing the header user fails', async () => {
    checkIn.mockResolvedValue(checkinResult)
    refreshUser.mockRejectedValueOnce(new Error('profile unavailable'))
    const wrapper = await mountView()

    await wrapper.get('[data-test="check-in-button"]').trigger('click')
    await flushPromises()

    expect(wrapper.get('[data-test="checkin-result-temporary"]').text()).toContain('$2.50')
    expect(wrapper.get('[data-test="checkin-result-permanent"]').text()).toContain('$0.25')
    expect(showSuccess).toHaveBeenCalled()
    expect(showError).not.toHaveBeenCalled()
  })
})
