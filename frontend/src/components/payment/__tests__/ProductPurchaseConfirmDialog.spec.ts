import { mount } from '@vue/test-utils'
import { nextTick } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProductPurchaseConfirmDialog from '../ProductPurchaseConfirmDialog.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const BaseDialogStub = {
  props: ['show', 'title'],
  emits: ['close'],
  template: '<section v-if="show" role="dialog" :aria-label="title"><slot /><footer><slot name="footer" /></footer></section>',
}

function mountDialog(overrides: Record<string, unknown> = {}) {
  return mount(ProductPurchaseConfirmDialog, {
    props: {
      show: true,
      productName: 'Starter credit',
      description: 'Permanent-credit bundle',
      paymentMethod: 'WeChat Pay',
      expectedSpend: '¥19.90',
      expectedReceive: '$25.00',
      limits: {
        daily_purchase_limit: 2,
        daily_purchase_remaining: 1,
        total_purchase_limit: 5,
        total_purchase_remaining: 3,
      },
      ...overrides,
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        PurchaseLimitBadge: true,
      },
    },
  })
}

describe('ProductPurchaseConfirmDialog', () => {
  afterEach(() => {
    document.body.innerHTML = ''
    document.body.classList.remove('modal-open')
  })

  it('shows transaction details and confirms only from the explicit action', async () => {
    const wrapper = mountDialog()

    expect(wrapper.get('[role="dialog"]').attributes('aria-label')).toBe('payment.purchaseConfirm.title')
    expect(wrapper.text()).toContain('Starter credit')
    expect(wrapper.text()).toContain('WeChat Pay')
    expect(wrapper.get('[data-test="purchase-confirm-spend"]').text()).toBe('¥19.90')
    expect(wrapper.get('[data-test="purchase-confirm-receive"]').text()).toBe('$25.00')
    expect(wrapper.get('[data-test="purchase-confirm-remaining-daily"]').text()).toBe('1')
    expect(wrapper.get('[data-test="purchase-confirm-remaining-total"]').text()).toBe('3')
    expect(wrapper.emitted('confirm')).toBeUndefined()

    await wrapper.get('[data-test="purchase-confirm-submit"]').trigger('click')
    expect(wrapper.emitted('confirm')).toHaveLength(1)
  })

  it('disables confirmation when a finite limit is exhausted', () => {
    const wrapper = mountDialog({
      limits: { daily_purchase_limit: 1, daily_purchase_remaining: 0 },
    })

    expect(wrapper.get('[data-test="purchase-confirm-submit"]').attributes('disabled')).toBeDefined()
  })

  it('keeps the dialog and long transaction labels shrinkable at a 390px viewport', async () => {
    Object.defineProperty(window, 'innerWidth', { configurable: true, value: 390 })
    const wrapper = mount(ProductPurchaseConfirmDialog, {
      attachTo: document.body,
      props: {
        show: true,
        productName: '每日穷鬼套餐',
        paymentMethod: '永久额度支付',
        expectedSpend: '$0.10 永久额度支付',
        expectedReceive: '$5.00 永久额度获得',
        limits: {
          daily_purchase_limit: 2,
          daily_purchase_remaining: 2,
          total_purchase_limit: 3,
          total_purchase_remaining: 3,
        },
      },
      global: {
        stubs: { PurchaseLimitBadge: false },
      },
    })
    await nextTick()

    const panel = document.body.querySelector('.modal-content')
    expect(panel?.classList.contains('min-w-0')).toBe(true)
    expect(panel?.classList.contains('max-w-[calc(100vw-1rem)]')).toBe(true)
    expect(document.body.querySelector('[data-test="purchase-confirm-dialog"]')?.classList.contains('min-w-0')).toBe(true)
    expect(document.body.querySelector('[data-test="purchase-confirm-spend"]')?.classList.contains('break-words')).toBe(true)
    expect(document.body.querySelector('[data-test="purchase-confirm-receive"]')?.classList.contains('min-w-0')).toBe(true)

    wrapper.unmount()
  })
})
