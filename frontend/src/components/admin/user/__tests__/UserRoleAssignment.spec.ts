import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import type { AdminUser } from '@/types'
import UserCreateModal from '../UserCreateModal.vue'
import UserEditModal from '../UserEditModal.vue'

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: {
      create: vi.fn(),
      update: vi.fn(),
    },
    userAttributes: {
      updateUserAttributeValues: vi.fn(),
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError: vi.fn(), showSuccess: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
}

const user: AdminUser = {
  id: 11,
  username: 'admin-target',
  email: 'admin-target@example.com',
  role: 'user',
  balance: 0,
  concurrency: 1,
  status: 'active',
  allowed_groups: [],
  balance_notify_enabled: false,
  balance_notify_threshold: null,
  balance_notify_extra_emails: [],
  created_at: '2026-09-13T00:00:00Z',
  updated_at: '2026-09-13T00:00:00Z',
  notes: '',
}

const stubs = {
  BaseDialog: BaseDialogStub,
  Icon: true,
  TotpStepUpDialog: true,
  UserAttributeForm: true,
}

describe('user role assignment entry points', () => {
  it.each([
    ['create', UserCreateModal, { show: true }],
    ['edit', UserEditModal, { show: true, user }],
  ])('does not expose super_admin to an unauthorised %s form', (_name, component, props) => {
    const wrapper = mount(component, { props, global: { stubs } })

    expect(wrapper.find('option[value="super_admin"]').exists()).toBe(false)
  })

  it.each([
    ['create', UserCreateModal, { show: true }],
    ['edit', UserEditModal, { show: true, user }],
  ])('exposes super_admin only when the existing assignment check allows it for %s', (_name, component, props) => {
    const wrapper = mount(component, {
      props: { ...props, canAssignSuperAdmin: true },
      global: { stubs },
    })

    expect(wrapper.find('option[value="super_admin"]').exists()).toBe(true)
  })
})
