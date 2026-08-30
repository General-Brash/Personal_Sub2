import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'

import CnBaseUrlPresets from '../CnBaseUrlPresets.vue'

describe('CnBaseUrlPresets', () => {
  it('requires mode and URL to match when the current URL is present', () => {
    const wrapper = mount(CnBaseUrlPresets, {
      props: {
        platform: 'zhipu',
        mode: 'payg',
        protocol: 'anthropic',
        currentUrl: 'https://open.bigmodel.cn/api/anthropic',
      },
    })

    const buttons = wrapper.findAll('[data-testid="cn-base-url-preset"]')

    expect(buttons).toHaveLength(2)
    expect(buttons[0].classes()).toContain('bg-primary-100')
    expect(buttons[1].classes()).not.toContain('bg-primary-100')
  })

  it('falls back to mode-only matching when the current URL is empty', () => {
    const wrapper = mount(CnBaseUrlPresets, {
      props: {
        platform: 'zhipu',
        mode: 'coding',
        protocol: 'anthropic',
        currentUrl: '',
      },
    })

    const buttons = wrapper.findAll('[data-testid="cn-base-url-preset"]')

    expect(buttons[0].classes()).not.toContain('bg-primary-100')
    expect(buttons[1].classes()).toContain('bg-primary-100')
  })
})
