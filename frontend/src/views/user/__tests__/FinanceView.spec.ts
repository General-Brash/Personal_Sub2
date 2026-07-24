import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import FinanceView from '../FinanceView.vue'

const { appState, authState } = vi.hoisted(() => ({
  appState: { cachedPublicSettings: null as null | Record<string, boolean> },
  authState: { isAdmin: false },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appState,
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountView() {
  return mount(FinanceView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: true,
        LedgerWorkspace: true,
        RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })
}

describe('FinanceView administrator shortcut visibility', () => {
  beforeEach(() => {
    authState.isAdmin = false
    appState.cachedPublicSettings = null
  })

  it('shows the shortcut to administrators by default', () => {
    authState.isAdmin = true

    expect(mountView().find('[data-test="admin-finance-button"]').exists()).toBe(true)
  })

  it('hides the shortcut when the page is disabled', () => {
    authState.isAdmin = true
    appState.cachedPublicSettings = { admin_finance_enabled: false }

    expect(mountView().find('[data-test="admin-finance-button"]').exists()).toBe(false)
  })
})
