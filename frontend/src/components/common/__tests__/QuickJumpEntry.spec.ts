import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'

const mocks = vi.hoisted(() => ({
  appStore: {
    cachedPublicSettings: null as null | {
      quick_jump_enabled?: boolean
      quick_jump_items?: Array<{
        id: string
        label: string
        icon_svg?: string
        url: string
        visibility: 'user' | 'admin'
        sort_order: number
      }>
    },
  },
  authStore: { isAdmin: false },
  adminSettingsStore: {
    quickJumpItems: [] as Array<{
      id: string
      label: string
      icon_svg?: string
      url: string
      visibility: 'user' | 'admin'
      sort_order: number
    }>,
  },
  routerPush: vi.fn(),
}))

vi.mock('@/stores/app', () => ({ useAppStore: () => mocks.appStore }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => mocks.authStore }))
vi.mock('@/stores/adminSettings', () => ({ useAdminSettingsStore: () => mocks.adminSettingsStore }))
vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))
vi.mock('vue-router', () => ({ useRouter: () => ({ push: mocks.routerPush }) }))

import QuickJumpEntry from '@/components/common/QuickJumpEntry.vue'

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show" data-test="quick-jump-dialog"><slot /><slot name="footer" /></div>',
})

function mountEntry() {
  return mount(QuickJumpEntry, {
    global: { stubs: { BaseDialog: BaseDialogStub, Icon: true } },
  })
}

function setSettings(
  items: NonNullable<typeof mocks.appStore.cachedPublicSettings>['quick_jump_items'],
  enabled = true,
) {
  mocks.appStore.cachedPublicSettings = {
    quick_jump_enabled: enabled,
    quick_jump_items: items,
  }
}

const userItem = {
  id: 'docs',
  label: 'Documentation',
  url: 'https://docs.example.com',
  visibility: 'user' as const,
  sort_order: 0,
}

describe('QuickJumpEntry', () => {
  beforeEach(() => {
    mocks.appStore.cachedPublicSettings = null
    mocks.authStore.isAdmin = false
    mocks.adminSettingsStore.quickJumpItems = []
    mocks.routerPush.mockReset()
  })

  it('does not render when the opt-in switch is off', () => {
    setSettings([userItem], false)
    expect(mountEntry().find('button[aria-label="quickJump.title"]').exists()).toBe(false)
  })

  it('does not render when there are no visible entries', () => {
    setSettings([])
    expect(mountEntry().find('button[aria-label="quickJump.title"]').exists()).toBe(false)
  })

  it('shows the top-bar trigger on small screens and hides admin-only entries from users', async () => {
    setSettings([
      userItem,
      { ...userItem, id: 'admin', label: 'Admin area', url: 'https://admin.example.com', visibility: 'admin', sort_order: 1 },
    ])
    const wrapper = mountEntry()
    const trigger = wrapper.get('button[aria-label="quickJump.title"]')

    expect(trigger.classes()).toContain('flex')
    expect(trigger.classes()).not.toContain('hidden')
    await trigger.trigger('click')

    const links = wrapper.findAll('a')
    expect(links).toHaveLength(1)
    expect(links[0].text()).toContain('Documentation')
    expect(links[0].attributes('href')).toContain('docs.example.com')
  })

  it('sanitizes javascript URLs and preserves SVG sanitization', async () => {
    setSettings([
      userItem,
      { ...userItem, id: 'unsafe', label: 'Unsafe', url: 'javascript:alert(1)', sort_order: 1 },
      {
        ...userItem,
        id: 'icon',
        label: 'Safe icon',
        icon_svg: '<svg onload="alert(1)"><path d="M0 0" /></svg>',
        sort_order: 2,
      },
    ])
    const wrapper = mountEntry()
    await wrapper.get('button[aria-label="quickJump.title"]').trigger('click')

    const links = wrapper.findAll('a')
    expect(links).toHaveLength(2)
    expect(links.map((link) => link.attributes('href')).join(' ')).not.toContain('javascript:')
    expect(wrapper.text()).not.toContain('Unsafe')
    expect(wrapper.html()).not.toContain('onload=')
  })

  it('shows the settings gear only to administrators', async () => {
    setSettings([userItem])
    const userWrapper = mountEntry()
    await userWrapper.get('button[aria-label="quickJump.title"]').trigger('click')
    expect(userWrapper.find('button.btn-secondary').exists()).toBe(false)

    mocks.authStore.isAdmin = true
    mocks.adminSettingsStore.quickJumpItems = [
      { ...userItem, id: 'admin', label: 'Admin link', visibility: 'admin' },
    ]
    const adminWrapper = mountEntry()
    await adminWrapper.get('button[aria-label="quickJump.title"]').trigger('click')
    expect(adminWrapper.find('button.btn-secondary').text()).toContain('quickJump.manage')
    expect(adminWrapper.findAll('a')).toHaveLength(2)
  })
})
