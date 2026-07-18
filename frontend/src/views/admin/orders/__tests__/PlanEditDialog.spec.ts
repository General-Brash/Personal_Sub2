import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { AdminGroup } from '@/types'
import PlanEditDialog from '../PlanEditDialog.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (key === 'payment.admin.subscriptionCnyPayPreview') return `preview ${params?.amount}`
        if (key === 'payment.admin.subscriptionCnyPayPreviewWithFee') return `fee ${params?.feeRate} ${params?.total}`
        return key
      },
    }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan: vi.fn(),
    updatePlan: vi.fn(),
  },
}))

function mountDialog(
  paymentConfig: Record<string, unknown> | null,
  groups: AdminGroup[] = [],
) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan: null,
      groups,
      paymentConfig,
    },
    global: {
      stubs: {
        BaseDialog: {
          props: ['show'],
          template: '<div v-if="show"><slot /><slot name="footer" /></div>',
        },
        Select: true,
        Icon: true,
        GroupBadge: true,
      },
    },
  })
}

describe('PlanEditDialog subscription CNY payment preview', () => {
  it('shows CNY channel charge using the configured subscription rate and fee', async () => {
    const wrapper = mountDialog({
      subscription_usd_to_cny_rate: 7.15,
      recharge_fee_rate: 2.5,
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview')
    expect(wrapper.text()).toContain('¥71.43')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).toContain('¥73.22')
  })

  it('hides the preview when the subscription rate is not configured', async () => {
    const wrapper = mountDialog({
      subscription_usd_to_cny_rate: 0,
      recharge_fee_rate: 2.5,
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).not.toContain('preview')
    expect(wrapper.text()).not.toContain('¥71.43')
  })
})

describe('PlanEditDialog group limit preview', () => {
  it('rounds an eight-place USD limit to two display decimals', async () => {
    const group = {
      id: 7,
      name: 'Precision group',
      platform: 'anthropic',
      rate_multiplier: 1,
      subscription_type: 'subscription',
      daily_limit_usd: 1.23500000,
      weekly_limit_usd: null,
      monthly_limit_usd: null,
    } as AdminGroup
    const wrapper = mountDialog(null, [group])

    wrapper.findAllComponents({ name: 'Select' })[0].vm.$emit('update:modelValue', group.id)
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('$1.24')
    expect(wrapper.text()).not.toContain('$1.23500000')
  })
})
