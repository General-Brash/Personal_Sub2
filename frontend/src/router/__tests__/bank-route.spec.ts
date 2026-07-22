import { describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
}))

const appStore = vi.hoisted(() => ({
  siteName: 'Sub2API',
  backendModeEnabled: false,
  cachedPublicSettings: null as null | Record<string, unknown>,
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

describe('router bank route', () => {
  it('registers bank as an authenticated non-admin route', async () => {
    const { default: router } = await import('@/router')
    const route = router.getRoutes().find((record) => record.name === 'Bank')

    expect(route?.path).toBe('/bank')
    expect(route?.meta.requiresAuth).toBe(true)
    expect(route?.meta.requiresAdmin).toBe(false)
    expect(route?.meta.titleKey).toBe('bank.title')
  })

  it('keeps bank and purchase deep links while adding the commerce entry route', async () => {
    const { default: router } = await import('@/router')
    const mall = router.getRoutes().find((record) => record.name === 'Commerce')
    const purchase = router.getRoutes().find((record) => record.name === 'PurchaseSubscription')
    expect(mall?.path).toBe('/mall')
    expect(mall?.meta.requiredFeatureFlag?.key).toBe('mall_enabled')
    expect(mall?.meta.requiresPayment).toBeUndefined()
    expect(router.getRoutes().find((record) => record.name === 'Bank')?.path).toBe('/bank')
    expect(purchase?.path).toBe('/purchase')
    expect(typeof purchase?.redirect).toBe('function')
  })

  it('does not gate administrator shelf management behind the payment switch', async () => {
    const { default: router } = await import('@/router')
    expect(router.getRoutes().find((record) => record.path === '/admin/orders/shelves')?.meta.requiresPayment).toBeUndefined()
    expect(router.getRoutes().find((record) => record.path === '/admin/orders/plans')?.meta.requiresPayment).toBeUndefined()
  })
})
