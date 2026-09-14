import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'

import CheckinConsentDialog from '../CheckinConsentDialog.vue'
import CheckinModeDialog from '../CheckinModeDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const BaseDialogStub = {
  props: ['show'],
  template: '<div v-if="show"><slot /></div>',
}

describe('check-in display units', () => {
  it('renders multiplier bps as an x multiplier rather than a raw bps value', () => {
    const wrapper = mount(CheckinModeDialog, {
      props: {
        show: true,
        baseReward: '1.00000000',
        permanentReward: '0.00000000',
        permanentBalance: '1.00000000',
        normal: { enabled: true, min_bps: 10000, max_bps: 15000 },
        superMode: { enabled: true, min_bps: 10000, max_bps: 15000, cost: '1.00000000' },
        canAffordSuper: true,
      },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })

    expect(wrapper.text()).toContain('1.00x–1.50x')
    expect(wrapper.text()).not.toContain('×')
  })

  it('renders check-in fee bps as a percentage while preserving the numeric input contract', () => {
    const wrapper = mount(CheckinConsentDialog, {
      props: { show: true, feeBps: 500 },
      global: { stubs: { BaseDialog: BaseDialogStub } },
    })

    expect(wrapper.text()).toContain('5.00%')
  })
})
