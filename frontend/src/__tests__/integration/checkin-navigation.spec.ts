import { beforeEach, describe, expect, it, vi } from 'vitest'

const authStore = vi.hoisted(() => ({
  checkAuth: vi.fn(),
  isAuthenticated: false,
  isAdmin: false,
  isSimpleMode: false,
  hasPendingAuthSession: false,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authStore,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    siteName: 'Sub2API',
    backendModeEnabled: false,
    cachedPublicSettings: null,
    publicSettingsLoaded: true,
    fetchPublicSettings: vi.fn(),
  }),
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

vi.mock('@/api/setup', () => ({
  getSetupStatus: vi.fn(),
}))

vi.mock('@/router/title', () => ({
  resolveRouteDocumentTitle: () => 'Sub2API',
}))

describe('check-in navigation contract', () => {
  beforeEach(() => {
    authStore.checkAuth.mockReset()
    authStore.isAuthenticated = false
    authStore.isAdmin = false
    authStore.isSimpleMode = false
    vi.stubGlobal('scrollTo', vi.fn())
  })

  it('redirects anonymous users and allows an authenticated regular user to open the page', async () => {
    const { default: router } = await import('@/router')

    await router.push('/check-in')
    expect(router.currentRoute.value.path).toBe('/login')
    expect(router.currentRoute.value.query.redirect).toBe('/check-in')

    authStore.isAuthenticated = true
    await router.push('/check-in')
    expect(router.currentRoute.value.path).toBe('/check-in')
    expect(router.currentRoute.value.meta.requiresAuth).toBe(true)
    expect(router.currentRoute.value.meta.requiresAdmin).toBe(false)
  })
})
