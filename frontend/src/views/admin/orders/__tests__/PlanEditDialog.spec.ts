import { describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { AdminGroup } from '@/types'
import type { SubscriptionPlan } from '@/types/payment'
import PlanEditDialog from '../PlanEditDialog.vue'

const { createPlan, updatePlan, showError, showSuccess } = vi.hoisted(() => ({
  createPlan: vi.fn(),
  updatePlan: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

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
    showError,
    showSuccess,
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan,
    updatePlan,
  },
}))

function mountDialog(
  paymentConfig: Record<string, unknown> | null,
  groups: AdminGroup[] = [],
  plan: SubscriptionPlan | null = null,
) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan,
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

describe('PlanEditDialog mall benefit fields', () => {
  it('creates a daily temporary-credit plan without requiring a Sub2 group', async () => {
    createPlan.mockReset().mockResolvedValue({ data: {} })
    const wrapper = mountDialog(null)
    const selects = wrapper.findAllComponents({ name: 'Select' })

    selects[0].vm.$emit('update:modelValue', 'daily_temporary_credit')
    selects[1].vm.$emit('update:modelValue', 'temporary')
    await wrapper.vm.$nextTick()
    await wrapper.get('[data-test="plan-name"]').setValue('Three day credit')
    await wrapper.get('[data-test="plan-description"]').setValue('Daily temporary credit')
    await wrapper.get('[data-test="plan-price"]').setValue('2.50')
    await wrapper.get('[data-test="plan-daily-temporary-credit"]').setValue('10')
    await wrapper.get('[data-test="plan-validity-days"]').setValue('3')
    await wrapper.get('#plan-form').trigger('submit')
    await flushPromises()

    expect(createPlan).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 0,
      benefit_type: 'daily_temporary_credit',
      payment_credit_type: 'temporary',
      daily_temporary_credit_amount: 10,
      price: 2.5,
      validity_days: 3,
      validity_unit: 'day',
    }))
  })

  it('updates an existing daily temporary-credit plan with the singular day unit', async () => {
    updatePlan.mockReset().mockResolvedValue({ data: {} })
    const plan = {
      id: 42,
      group_id: 0,
      name: 'Daily credit',
      description: 'Daily temporary credit',
      price: 2.5,
      original_price: 0,
      validity_days: 3,
      validity_unit: 'days',
      features: [],
      for_sale: true,
      benefit_type: 'daily_temporary_credit',
      payment_credit_type: 'temporary',
      daily_temporary_credit_amount: 10,
    } as SubscriptionPlan
    const wrapper = mountDialog(null, [], plan)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()

    await wrapper.get('#plan-form').trigger('submit')
    await flushPromises()

    expect(updatePlan).toHaveBeenCalledWith(42, expect.objectContaining({
      group_id: 0,
      benefit_type: 'daily_temporary_credit',
      validity_days: 3,
      validity_unit: 'day',
    }))
  })

  it.each([
    ['day', 'days'],
    ['week', 'weeks'],
    ['month', 'months'],
  ])('normalizes legacy Sub2 %s validity for editing', async (storedUnit, submittedUnit) => {
    updatePlan.mockReset().mockResolvedValue({ data: {} })
    const plan = {
      id: 51,
      group_id: 7,
      name: 'Legacy plan',
      description: 'Legacy Sub2 plan',
      price: 2.5,
      original_price: 0,
      validity_days: 2,
      validity_unit: storedUnit,
      features: [],
      for_sale: true,
      benefit_type: 'sub2',
      payment_credit_type: 'permanent',
    } as SubscriptionPlan
    const wrapper = mountDialog(null, [], plan)
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.findAllComponents({ name: 'Select' })[3].props('modelValue')).toBe(submittedUnit)
    await wrapper.get('#plan-form').trigger('submit')
    await flushPromises()

    expect(updatePlan).toHaveBeenCalledWith(51, expect.objectContaining({
      benefit_type: 'sub2',
      validity_unit: submittedUnit,
    }))
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

    wrapper.findAllComponents({ name: 'Select' })[2].vm.$emit('update:modelValue', group.id)
    await wrapper.vm.$nextTick()

    expect(wrapper.text()).toContain('$1.24')
    expect(wrapper.text()).not.toContain('$1.23500000')
  })
})

describe('PlanEditDialog purchase limits', () => {
  it('submits daily and total limits from advanced settings', async () => {
    createPlan.mockReset().mockResolvedValue({ data: {} })
    const group = {
      id: 9,
      name: 'Paid group',
      platform: 'openai',
      rate_multiplier: 1,
      subscription_type: 'subscription',
    } as AdminGroup
    const wrapper = mountDialog(null, [group])

    wrapper.findAllComponents({ name: 'Select' })[2].vm.$emit('update:modelValue', group.id)
    await wrapper.get('[data-test="plan-name"]').setValue('Limited plan')
    await wrapper.get('[data-test="plan-description"]').setValue('Plan description')
    await wrapper.get('[data-test="plan-price"]').setValue('12.50')
    await wrapper.get('[data-test="plan-daily-purchase-limit"]').setValue('1')
    await wrapper.get('[data-test="plan-total-purchase-limit"]').setValue('3')
    await wrapper.get('#plan-form').trigger('submit')
    await flushPromises()

    expect(createPlan).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 9,
      benefit_type: 'sub2',
      payment_credit_type: 'permanent',
      daily_purchase_limit: 1,
      total_purchase_limit: 3,
    }))
  })
})
