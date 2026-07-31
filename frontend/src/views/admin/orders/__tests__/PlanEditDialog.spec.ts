import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

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

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: Boolean,
    title: String,
    width: String,
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: [String, Number],
    options: {
      type: Array,
      default: () => [],
    },
    placeholder: String,
  },
  emits: ['update:modelValue'],
  setup(_props, { emit }) {
    const onChange = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value
      emit('update:modelValue', value === '' ? null : Number(value))
    }
    return { onChange }
  },
  template: `
    <select
      :value="modelValue ?? ''"
      @change="onChange"
    >
      <option value="">{{ placeholder }}</option>
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :data-platform="option.platform"
      >
        {{ option.label }}
      </option>
    </select>
  `,
})

const groupFixture = (overrides: Partial<AdminGroup>): AdminGroup => ({
  id: 1,
  name: 'OpenAI',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'subscription',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: false,
  sort_order: 0,
  ...overrides,
})

function mountDialog({
  groups = [],
  paymentConfig = null,
  plan = null,
}: {
  groups?: AdminGroup[]
  paymentConfig?: Record<string, unknown> | null
  plan?: SubscriptionPlan | null
} = {}) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan,
      groups,
      paymentConfig,
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        GroupBadge: true,
      },
    },
  })
}

describe('PlanEditDialog payment preview', () => {
  it('shows CNY channel charge using the configured subscription rate and fee', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 7.15,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.get('[data-test="plan-price"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview')
    expect(wrapper.text()).toContain('¥71.43')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).toContain('¥73.22')
  })

  it('hides the preview when the subscription rate is not configured', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 0,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.get('[data-test="plan-price"]').setValue('9.99')

    expect(wrapper.text()).not.toContain('preview')
    expect(wrapper.text()).not.toContain('¥71.43')
  })
})

describe('PlanEditDialog mall benefit fields', () => {
  it('creates a daily temporary-credit plan without requiring a Sub2 group', async () => {
    createPlan.mockReset().mockResolvedValue({ data: {} })
    const wrapper = mountDialog()
    const selects = wrapper.findAllComponents({ name: 'SelectStub' })

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
    const wrapper = mountDialog({ plan })
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
    const wrapper = mountDialog({ plan })
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(wrapper.findAllComponents({ name: 'SelectStub' })[3].props('modelValue')).toBe(submittedUnit)
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
    const wrapper = mountDialog({ groups: [group] })

    wrapper.findAllComponents({ name: 'SelectStub' })[2].vm.$emit('update:modelValue', group.id)
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
    const wrapper = mountDialog({ groups: [group] })

    wrapper.findAllComponents({ name: 'SelectStub' })[2].vm.$emit('update:modelValue', group.id)
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

describe('PlanEditDialog group options', () => {
  it('allows composite subscription groups for payment plans', () => {
    const wrapper = mountDialog({
      groups: [
        groupFixture({
          id: 10,
          name: 'OpenAI + Claude + Gemini + Grok',
          platform: 'composite',
          rate_multiplier: 1.2,
          subscription_type: 'subscription',
        }),
        groupFixture({
          id: 11,
          name: 'Standard OpenAI',
          platform: 'openai',
          subscription_type: 'standard',
        }),
      ],
    })

    const options = wrapper.findAll('option').map(option => option.text())

    expect(options).toContain('OpenAI + Claude + Gemini + Grok — composite (1.2x)')
    expect(options).not.toContain('Standard OpenAI — openai (1x)')
  })
})
