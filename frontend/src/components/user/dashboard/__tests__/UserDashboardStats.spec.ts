import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import UserDashboardStats from '../UserDashboardStats.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

function mountStats(temporaryCredit: string) {
  return mount(UserDashboardStats, {
    props: {
      stats: { by_platform: [] } as any,
      balance: 12.345,
      temporaryCredit,
      isSimple: false,
      platformQuotas: [],
    },
    global: { stubs: { Icon: true } },
  })
}

describe('UserDashboardStats balance card', () => {
  it('labels permanent balance separately and shows temporary credit below it', () => {
    const wrapper = mountStats('5.25500000')

    expect(wrapper.get('[data-test="dashboard-permanent-balance"]').text()).toBe('$12.35')
    expect(wrapper.get('[data-test="dashboard-temporary-credit"]').text()).toContain(
      'dashboard.temporaryCredit: $5.26',
    )
    expect(wrapper.text()).toContain('dashboard.permanentBalance')
  })

  it('formats the temporary credit from its original decimal string', () => {
    const wrapper = mountStats('999999999999.99999999')

    expect(wrapper.get('[data-test="dashboard-temporary-credit"]').text()).toContain(
      '$1000000000000.00',
    )
  })
})
