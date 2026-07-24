import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

import type { CheckinStatus } from '@/api/checkin'
import DashboardView from '../DashboardView.vue'

const {
  getCheckinStatus,
  getDashboardStats,
  getDashboardTrend,
  getDashboardModels,
  getByDateRange,
  getMyPlatformQuotas,
  refreshUser,
} = vi.hoisted(() => ({
  getCheckinStatus: vi.fn(),
  getDashboardStats: vi.fn(),
  getDashboardTrend: vi.fn(),
  getDashboardModels: vi.fn(),
  getByDateRange: vi.fn(),
  getMyPlatformQuotas: vi.fn(),
  refreshUser: vi.fn(),
}))

vi.mock('@/api/checkin', () => ({ getCheckinStatus }))
vi.mock('@/api/usage', () => ({
  usageAPI: { getDashboardStats, getDashboardTrend, getDashboardModels, getByDateRange },
}))
vi.mock('@/api/user', () => ({ getMyPlatformQuotas }))
vi.mock('@/stores/auth', () => ({
  useAuthStore: () => ({
    user: { balance: 3.5 },
    isSimpleMode: false,
    refreshUser,
  }),
}))

const baseStatus = (temporaryCredit: string): CheckinStatus => ({
  enabled: true,
  today_checked_in: false,
  current_streak_day: 2,
  next_reward_day: 3,
  next_reward_amount: '1.00000000',
  next_permanent_reward_amount: '0.25000000',
  temporary_credit_available: temporaryCredit,
  temporary_credit_earliest_expires_at: null,
  monthly_reward_total: '2.00000000',
  monthly_permanent_reward_total: '0.50000000',
  reward_tiers: [],
  calendar: [],
})

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((promiseResolve) => {
    resolve = promiseResolve
  })
  return { promise, resolve }
}

function mountDashboard() {
  return mount(DashboardView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        LoadingSpinner: true,
        UserDashboardStats: {
          name: 'UserDashboardStatsStub',
          props: ['temporaryCredit'],
          template: '<div data-test="dashboard-stats-credit">{{ temporaryCredit }}</div>',
        },
        UserDashboardCharts: {
          name: 'UserDashboardChartsStub',
          emits: ['refresh', 'dateRangeChange', 'granularityChange', 'update:startDate', 'update:endDate', 'update:granularity'],
          template: '<div data-test="dashboard-charts" />',
        },
        UserDashboardRecentUsage: true,
        UserDashboardCheckinCard: {
          name: 'UserDashboardCheckinCardStub',
          props: ['status', 'loading'],
          template: '<div data-test="dashboard-checkin-credit">{{ status?.temporary_credit_available }}</div>',
        },
        UserDashboardQuickActions: true,
      },
    },
  })
}

describe('DashboardView daily check-in integration', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-16T04:00:00Z'))
    refreshUser.mockResolvedValue(undefined)
    getDashboardStats.mockResolvedValue({ by_platform: [] })
    getDashboardTrend.mockResolvedValue({ trend: [] })
    getDashboardModels.mockResolvedValue({ models: [] })
    getByDateRange.mockResolvedValue({ items: [] })
    getMyPlatformQuotas.mockResolvedValue({ platform_quotas: [] })
    getCheckinStatus.mockResolvedValue(baseStatus('5.25000000'))
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('loads check-in status once and shares the same response with both dashboard cards', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    expect(getCheckinStatus).toHaveBeenCalledTimes(1)
    expect(getCheckinStatus).toHaveBeenCalledWith('2026-07')
    expect(wrapper.get('[data-test="dashboard-stats-credit"]').text()).toBe('5.25000000')
    expect(wrapper.get('[data-test="dashboard-checkin-credit"]').text()).toBe('5.25000000')
  })

  it('does not let an older dashboard refresh overwrite newer temporary credit', async () => {
    const wrapper = mountDashboard()
    await flushPromises()

    const older = deferred<CheckinStatus>()
    const newer = deferred<CheckinStatus>()
    getCheckinStatus.mockReturnValueOnce(older.promise).mockReturnValueOnce(newer.promise)

    const charts = wrapper.findComponent({ name: 'UserDashboardChartsStub' })
    charts.vm.$emit('refresh')
    charts.vm.$emit('refresh')

    newer.resolve(baseStatus('9.00000000'))
    await flushPromises()
    expect(wrapper.get('[data-test="dashboard-stats-credit"]').text()).toBe('9.00000000')

    older.resolve(baseStatus('8.00000000'))
    await flushPromises()
    expect(wrapper.get('[data-test="dashboard-stats-credit"]').text()).toBe('9.00000000')
  })
})
