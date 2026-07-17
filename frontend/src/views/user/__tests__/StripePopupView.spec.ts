import { afterEach, describe, expect, it, vi } from 'vitest'
import { shallowMount, type VueWrapper } from '@vue/test-utils'

const routeState = vi.hoisted(() => ({
  query: {} as Record<string, unknown>,
}))

vi.mock('vue-router', async () => {
  const actual = await vi.importActual<typeof import('vue-router')>('vue-router')
  return {
    ...actual,
    useRoute: () => routeState,
  }
})

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

import StripePopupView from '../StripePopupView.vue'

let wrapper: VueWrapper | null = null

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('StripePopupView amount display', () => {
  it.each([
    ['10', '¥10.00'],
    ['1.2', '¥1.20'],
    ['12345678901234567890.12999999', '¥12345678901234567890.13'],
  ])('formats query amount %s with two decimal places', (amount, expected) => {
    routeState.query = { amount }

    wrapper = shallowMount(StripePopupView)

    expect(wrapper.text()).toContain(expected)
  })
})
