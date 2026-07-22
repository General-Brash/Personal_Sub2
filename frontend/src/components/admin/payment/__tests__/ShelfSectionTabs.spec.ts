import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import ShelfSectionTabs from '../ShelfSectionTabs.vue'

const push = vi.hoisted(() => vi.fn())

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return { ...actual, useRouter: () => ({ push }) }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const RouterLinkStub = {
  inheritAttrs: false,
  props: ['to'],
  template: '<a v-bind="$attrs" :href="to"><slot /></a>',
}

describe('ShelfSectionTabs', () => {
  beforeEach(() => {
    push.mockReset().mockResolvedValue(undefined)
  })

  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('links each route tab to its panel and keeps a single tab in the tab order', () => {
    const wrapper = mount(ShelfSectionTabs, {
      props: { activeSection: 'currency' },
      global: { stubs: { RouterLink: RouterLinkStub } },
    })

    const currencyTab = wrapper.get('#admin-shelf-tab-currency')
    const subscriptionTab = wrapper.get('#admin-shelf-tab-subscription')
    expect(currencyTab.attributes('aria-controls')).toBe('admin-shelf-panel-currency')
    expect(subscriptionTab.attributes('aria-controls')).toBe('admin-shelf-panel-subscription')
    expect(currencyTab.attributes('aria-selected')).toBe('true')
    expect(currencyTab.attributes('tabindex')).toBe('0')
    expect(subscriptionTab.attributes('tabindex')).toBe('-1')
  })

  it('navigates and focuses the destination tab with arrow keys', async () => {
    const wrapper = mount(ShelfSectionTabs, {
      attachTo: document.body,
      props: { activeSection: 'currency' },
      global: { stubs: { RouterLink: RouterLinkStub } },
    })
    const currencyTab = wrapper.get('#admin-shelf-tab-currency')
    const subscriptionTab = wrapper.get('#admin-shelf-tab-subscription')

    await currencyTab.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()

    expect(push).toHaveBeenCalledWith('/admin/orders/plans')
    expect(document.activeElement).toBe(subscriptionTab.element)

    await wrapper.setProps({ activeSection: 'subscription' })
    await subscriptionTab.trigger('keydown', { key: 'Home' })
    await flushPromises()

    expect(push).toHaveBeenLastCalledWith('/admin/orders/shelves')
    expect(document.activeElement).toBe(currencyTab.element)
    wrapper.unmount()
  })
})
