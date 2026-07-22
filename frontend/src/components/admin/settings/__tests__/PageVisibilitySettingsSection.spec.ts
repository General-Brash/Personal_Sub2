import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import PageVisibilitySettingsSection from '../PageVisibilitySettingsSection.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const ToggleStub = defineComponent({
  props: {
    modelValue: {
      type: Boolean,
      required: true,
    },
  },
  emits: ['update:modelValue'],
  setup(props, { attrs, emit }) {
    return () =>
      h('button', {
        ...attrs,
        type: 'button',
        role: 'switch',
        class: 'toggle-stub',
        'aria-checked': String(props.modelValue),
        onClick: () => emit('update:modelValue', !props.modelValue),
      })
  },
})

function mountSection() {
  return mount(PageVisibilitySettingsSection, {
    props: {
      userChannelStatusEnabled: true,
      mallEnabled: true,
      userSubscriptionsEnabled: false,
      adminSubscriptionsEnabled: true,
      adminPromoCodesEnabled: true,
      adminChannelManagementEnabled: false,
    },
    global: {
      stubs: {
        Toggle: ToggleStub,
      },
    },
  })
}

describe('PageVisibilitySettingsSection', () => {
  it('renders two un-nested page groups with six toggles', () => {
    const wrapper = mountSection()

    expect(wrapper.findAll('.card')).toHaveLength(1)
    expect(wrapper.find('.card .card').exists()).toBe(false)
    expect(wrapper.findAll('[role="switch"]')).toHaveLength(6)
    expect(wrapper.text()).toContain('admin.settings.features.pageVisibility.userPages')
    expect(wrapper.text()).toContain('admin.settings.features.pageVisibility.adminPages')
  })

  it('places administrator subscriptions only in the administrator page group', () => {
    const wrapper = mountSection()
    const userGroup = wrapper.get('[data-test="user-pages-group"]')
    const adminGroup = wrapper.get('[data-test="admin-pages-group"]')
    const label = 'admin.settings.features.pageVisibility.adminSubscriptions.label'

    expect(userGroup.text()).not.toContain(label)
    expect(adminGroup.text()).toContain(label)
    expect(adminGroup.find(`[aria-label="${label}"]`).exists()).toBe(true)
  })

  it('emits each named v-model update', async () => {
    const wrapper = mountSection()
    const toggles = wrapper.findAll('[role="switch"]')

    for (const toggle of toggles) {
      await toggle.trigger('click')
    }

    expect(wrapper.emitted('update:userChannelStatusEnabled')).toEqual([[false]])
    expect(wrapper.emitted('update:mallEnabled')).toEqual([[false]])
    expect(wrapper.emitted('update:userSubscriptionsEnabled')).toEqual([[true]])
    expect(wrapper.emitted('update:adminSubscriptionsEnabled')).toEqual([[false]])
    expect(wrapper.emitted('update:adminPromoCodesEnabled')).toEqual([[false]])
    expect(wrapper.emitted('update:adminChannelManagementEnabled')).toEqual([[true]])
  })
})
