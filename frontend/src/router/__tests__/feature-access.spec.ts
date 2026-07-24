import { beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

type NavigationGuard = (
  to: Record<string, any>,
  from: Record<string, any>,
  next: ReturnType<typeof vi.fn>
) => Promise<void>

const routerHarness = vi.hoisted(() => ({
  guard: null as NavigationGuard | null,
  routes: [] as Array<Record<string, any>>,
}))

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: true,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  publicSettingsLoaded: false,
  cachedPublicSettings: null as null | {
    payment_enabled?: boolean
    mall_enabled?: boolean
    risk_control_enabled?: boolean
    channel_monitor_enabled?: boolean
    user_channel_status_enabled?: boolean
    user_subscriptions_enabled?: boolean
    admin_subscriptions_enabled?: boolean
    admin_promo_codes_enabled?: boolean
    admin_channel_management_enabled?: boolean
    admin_finance_enabled?: boolean
    admin_bank_transactions_enabled?: boolean
    admin_audit_logs_enabled?: boolean
    admin_ops_enabled?: boolean
    custom_menu_items?: []
  },
  fetchPublicSettings: vi.fn(),
  showWarning: vi.fn(),
}))

vi.mock('vue-router', () => ({
  createWebHistory: vi.fn(() => ({})),
  createRouter: vi.fn((options: { routes: Array<Record<string, any>> }) => {
    routerHarness.routes = options.routes
    return {
      beforeEach: vi.fn((guard: NavigationGuard) => {
        routerHarness.guard = guard
      }),
      afterEach: vi.fn(),
      onError: vi.fn(),
    }
  }),
}))

vi.mock('@/i18n', () => ({
  i18n: {
    global: {
      t: (key: string) => key,
    },
  },
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

vi.mock('@/stores/adminSettings', () => ({
  useAdminSettingsStore: () => ({ customMenuItems: [] }),
}))

vi.mock('@/stores/adminCompliance', () => ({
  useAdminComplianceStore: () => ({
    initialized: true,
    fetchStatus: vi.fn(),
    requireAcknowledgement: vi.fn(),
  }),
}))

vi.mock('@/composables/useNavigationLoading', () => ({
  useNavigationLoadingState: () => ({
    startNavigation: vi.fn(),
    endNavigation: vi.fn(),
    isLoading: { value: false },
  }),
}))

vi.mock('@/composables/useRoutePrefetch', () => ({
  useRoutePrefetch: () => ({
    triggerPrefetch: vi.fn(),
    cancelPendingPrefetch: vi.fn(),
    resetPrefetchState: vi.fn(),
  }),
}))

function createDeferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function runGuard(meta: Record<string, unknown>, path: string) {
  if (!routerHarness.guard) {
    throw new Error('router guard was not registered')
  }

  const next = vi.fn()
  const navigation = routerHarness.guard(
    {
      path,
      fullPath: path,
      name: 'FeatureRoute',
      params: {},
      meta: { requiresAuth: true, ...meta },
    },
    {},
    next
  )
  return { navigation, next }
}

const featureFlagValues = [true, false, undefined] as const
const combinedFeatureFlagCases = featureFlagValues.flatMap((pageFlag) =>
  featureFlagValues.map((channelMonitorFlag) => ({
    pageFlag,
    channelMonitorFlag,
    blocked: pageFlag === false || channelMonitorFlag === false,
  })),
)

describe('feature route guard', () => {
  beforeAll(async () => {
    await import('@/router')
  })

  beforeEach(() => {
    authStore.isAuthenticated = true
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    appStore.publicSettingsLoaded = false
    appStore.cachedPublicSettings = null
    appStore.fetchPublicSettings.mockReset()
    appStore.showWarning.mockReset()
  })

  it.each([
    ['/mall', 'mall_enabled'],
    ['/monitor', 'user_channel_status_enabled'],
    ['/subscriptions', 'user_subscriptions_enabled'],
    ['/admin/subscriptions', 'admin_subscriptions_enabled'],
    ['/admin/promo-codes', 'admin_promo_codes_enabled'],
    ['/admin/channels', 'admin_channel_management_enabled'],
    ['/admin/channels/pricing', 'admin_channel_management_enabled'],
    ['/admin/channels/monitor', 'admin_channel_management_enabled'],
    ['/admin/finance', 'admin_finance_enabled'],
    ['/admin/bank/transactions', 'admin_bank_transactions_enabled'],
    ['/admin/audit-logs', 'admin_audit_logs_enabled'],
    ['/admin/ops', 'admin_ops_enabled'],
  ])('binds %s to the %s route meta flag', (path, key) => {
    const route = routerHarness.routes.find((item) => item.path === path)

    expect(route?.meta?.requiredFeatureFlag?.key).toBe(key)
  })

  it.each([
    ['/admin/finance', 'admin_finance_enabled'],
    ['/admin/bank/transactions', 'admin_bank_transactions_enabled'],
    ['/admin/audit-logs', 'admin_audit_logs_enabled'],
    ['/admin/ops', 'admin_ops_enabled'],
  ])('blocks direct access to disabled administrator page %s', async (path, key) => {
    const route = routerHarness.routes.find((item) => item.path === path)
    authStore.isAdmin = true
    appStore.cachedPublicSettings = { [key]: false }
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(route?.meta ?? {}, path)
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith('/admin/dashboard')
    expect(appStore.showWarning).toHaveBeenCalledWith('common.pageDisabledByAdmin')
  })

  it.each(['/monitor', '/admin/channels/monitor'])(
    'also binds %s to the shared channel monitor flag',
    (path) => {
      const route = routerHarness.routes.find((item) => item.path === path)

      expect(route?.meta?.requiredFeatureFlags?.map((flag: { key: string }) => flag.key)).toEqual([
        'channel_monitor_enabled',
      ])
    },
  )

  it('waits for the first public-settings request before deciding payment access', async () => {
    const deferred = createDeferred<{ payment_enabled: boolean }>()
    appStore.fetchPublicSettings.mockImplementation(async () => {
      const settings = await deferred.promise
      appStore.cachedPublicSettings = settings
      appStore.publicSettingsLoaded = true
      return settings
    })

    const { navigation, next } = runGuard({ requiresPayment: true }, '/purchase')

    await vi.waitFor(() => expect(appStore.fetchPublicSettings).toHaveBeenCalledTimes(1))
    expect(next).not.toHaveBeenCalled()

    deferred.resolve({ payment_enabled: true })
    await navigation
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it('waits for public settings before rejecting a cold disabled /mall visit', async () => {
    const route = routerHarness.routes.find((item) => item.path === '/mall')
    const deferred = createDeferred<{ mall_enabled: boolean }>()
    appStore.fetchPublicSettings.mockImplementation(async () => {
      const settings = await deferred.promise
      appStore.cachedPublicSettings = settings
      appStore.publicSettingsLoaded = true
      return settings
    })

    const { navigation, next } = runGuard(route?.meta ?? {}, '/mall')

    await vi.waitFor(() => expect(appStore.fetchPublicSettings).toHaveBeenCalledTimes(1))
    expect(next).not.toHaveBeenCalled()

    deferred.resolve({ mall_enabled: false })
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith('/dashboard')
    expect(appStore.showWarning).toHaveBeenCalledWith('common.pageDisabledByAdmin')
  })

  it('allows /mall browsing when the mall is enabled but payment is disabled', async () => {
    const route = routerHarness.routes.find((item) => item.path === '/mall')
    appStore.cachedPublicSettings = { mall_enabled: true, payment_enabled: false }
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(route?.meta ?? {}, '/mall')
    await navigation

    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
    expect(route?.meta?.requiresPayment).toBeUndefined()
    expect(next).toHaveBeenCalledWith()
  })

  it('keeps every /purchase query and hash value in the /mall compatibility redirect', () => {
    const route = routerHarness.routes.find((item) => item.path === '/purchase')
    const redirect = route?.redirect as (to: Record<string, unknown>) => Record<string, unknown>

    expect(redirect({ query: { tab: 'subscription', plan_id: '7' }, hash: '#resume' })).toEqual({
      path: '/mall',
      query: { tab: 'subscription', plan_id: '7' },
      hash: '#resume',
    })
  })

  it.each([
    ['payment', { requiresPayment: true }, '/purchase'],
    ['risk control', { requiresRiskControl: true }, '/admin/risk-control'],
    [
      'page visibility',
      {
        requiredFeatureFlag: {
          key: 'user_subscriptions_enabled',
          mode: 'opt-out',
          label: 'User Subscriptions',
        },
      },
      '/subscriptions',
    ],
    [
      'page visibility list',
      {
        requiredFeatureFlags: [
          {
            key: 'user_channel_status_enabled',
            mode: 'opt-out',
            label: 'User Channel Status',
          },
          {
            key: 'channel_monitor_enabled',
            mode: 'opt-out',
            label: 'Channel Monitor',
          },
        ],
      },
      '/monitor',
    ],
  ])('does not treat a failed %s settings load as explicitly disabled', async (_name, meta, path) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.fetchPublicSettings.mockResolvedValue(null)

    const { navigation, next } = runGuard(meta, path)
    await navigation

    expect(appStore.publicSettingsLoaded).toBe(false)
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith()
  })

  it.each([
    ['payment', { requiresPayment: true }, { payment_enabled: false }, '/dashboard'],
    [
      'risk control',
      { requiresRiskControl: true },
      { risk_control_enabled: false },
      '/admin/settings',
    ],
  ])('redirects when loaded settings explicitly disable %s', async (_name, meta, settings, target) => {
    authStore.isAdmin = meta.requiresRiskControl === true
    appStore.cachedPublicSettings = settings
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(meta, '/feature')
    await navigation

    expect(appStore.fetchPublicSettings).not.toHaveBeenCalled()
    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith(target)
  })

  it.each([
    [
      'user page',
      false,
      '/subscriptions',
      'user_subscriptions_enabled',
      '/dashboard',
    ],
    [
      'administrator page',
      true,
      '/admin/promo-codes',
      'admin_promo_codes_enabled',
      '/admin/dashboard',
    ],
    [
      'administrator subscriptions page',
      true,
      '/admin/subscriptions',
      'admin_subscriptions_enabled',
      '/admin/dashboard',
    ],
  ])('redirects a disabled %s and shows a warning', async (_name, isAdmin, path, key, target) => {
    authStore.isAdmin = isAdmin
    appStore.cachedPublicSettings = { [key]: false }
    appStore.publicSettingsLoaded = true

    const { navigation, next } = runGuard(
      {
        requiresAdmin: isAdmin,
        requiredFeatureFlag: { key, mode: 'opt-out', label: key },
      },
      path,
    )
    await navigation

    expect(next).toHaveBeenCalledOnce()
    expect(next).toHaveBeenCalledWith(target)
    expect(appStore.showWarning).toHaveBeenCalledWith('common.pageDisabledByAdmin')
  })

  it.each(combinedFeatureFlagCases)(
    'applies the user monitor flag matrix: page=$pageFlag monitor=$channelMonitorFlag',
    async ({ pageFlag, channelMonitorFlag, blocked }) => {
      const route = routerHarness.routes.find((item) => item.path === '/monitor')
      appStore.cachedPublicSettings = {
        user_channel_status_enabled: pageFlag,
        channel_monitor_enabled: channelMonitorFlag,
      }
      appStore.publicSettingsLoaded = true

      const { navigation, next } = runGuard(route?.meta ?? {}, '/monitor')
      await navigation

      expect(next).toHaveBeenCalledOnce()
      expect(next).toHaveBeenCalledWith(...(blocked ? ['/dashboard'] : []))
      expect(appStore.showWarning).toHaveBeenCalledTimes(blocked ? 1 : 0)
    },
  )

  it.each(combinedFeatureFlagCases)(
    'applies the administrator monitor flag matrix: page=$pageFlag monitor=$channelMonitorFlag',
    async ({ pageFlag, channelMonitorFlag, blocked }) => {
      const route = routerHarness.routes.find((item) => item.path === '/admin/channels/monitor')
      authStore.isAdmin = true
      appStore.cachedPublicSettings = {
        admin_channel_management_enabled: pageFlag,
        channel_monitor_enabled: channelMonitorFlag,
      }
      appStore.publicSettingsLoaded = true

      const { navigation, next } = runGuard(route?.meta ?? {}, '/admin/channels/monitor')
      await navigation

      expect(next).toHaveBeenCalledOnce()
      expect(next).toHaveBeenCalledWith(...(blocked ? ['/admin/dashboard'] : []))
      expect(appStore.showWarning).toHaveBeenCalledTimes(blocked ? 1 : 0)
    },
  )
})
