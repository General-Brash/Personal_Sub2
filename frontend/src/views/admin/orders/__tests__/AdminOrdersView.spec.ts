import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { PaymentOrder } from '@/types/payment'
import AdminOrdersView from '../AdminOrdersView.vue'

const {
  getOrders,
  getOrder,
  cancelOrder,
  retryRecharge,
  refundOrder,
  queryRefund,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getOrders: vi.fn(),
  getOrder: vi.fn(),
  cancelOrder: vi.fn(),
  retryRecharge: vi.fn(),
  refundOrder: vi.fn(),
  queryRefund: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => {
  const paymentAPI = {
    getOrders,
    getOrder,
    cancelOrder,
    retryRecharge,
    refundOrder,
    queryRefund,
  }
  return { adminPaymentAPI: paymentAPI, default: paymentAPI }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) =>
        params?.status ? `${key}:${String(params.status)}` : key,
    }),
  }
})

const OrderTableStub = {
  props: ['orders'],
  template: `
    <div>
      <div v-for="row in orders" :key="row.id" :data-order-id="row.id">
        <slot name="actions" :row="row" />
      </div>
    </div>
  `,
}

const AdminRefundDialogStub = {
  props: ['show', 'order'],
  emits: ['confirm', 'cancel'],
  template: `
    <button
      data-test="refund-confirm"
      @click="$emit('confirm', { amount: 25, reason: 'test refund', deduct_balance: true, force: false })"
    >
      confirm
    </button>
  `,
}

function makeOrder(status: string, id = 1): PaymentOrder {
  return {
    id,
    user_id: 10,
    amount: 100,
    pay_amount: 100,
    currency: 'USD',
    fee_rate: 0,
    payment_type: 'stripe',
    out_trade_no: `order-${id}`,
    status: status as PaymentOrder['status'],
    order_type: 'balance',
    created_at: '2026-08-17T00:00:00Z',
    expires_at: '2026-08-18T00:00:00Z',
    refund_amount: status === 'PARTIALLY_REFUNDED' ? 25 : 0,
  }
}

function mountView(orders: PaymentOrder[]) {
  getOrders.mockResolvedValue({ data: { items: orders, total: orders.length } })
  return mount(AdminOrdersView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        OrderTable: OrderTableStub,
        AdminRefundDialog: AdminRefundDialogStub,
        Pagination: true,
        BaseDialog: true,
        Select: true,
        Icon: true,
        OrderStatusBadge: true,
      },
    },
  })
}

function orderRow(wrapper: VueWrapper, id: number) {
  return wrapper.get(`[data-order-id="${id}"]`)
}

function buttonByText(wrapper: VueWrapper, text: string) {
  return wrapper.findAll('button').find((button) => button.text() === text)
}

describe('AdminOrdersView refund entry', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getOrder.mockResolvedValue({ data: {} })
    refundOrder.mockResolvedValue({ data: { success: true } })
    queryRefund.mockResolvedValue({ data: { success: false, warning: 'pending' } })
  })

  it('allows a completed order to start a refund', async () => {
    const order = makeOrder('COMPLETED')
    const wrapper = mountView([order])
    await flushPromises()

    const refundButton = buttonByText(orderRow(wrapper, order.id), 'payment.admin.refund')
    expect(refundButton).toBeDefined()
    await refundButton!.trigger('click')
    await wrapper.get('[data-test="refund-confirm"]').trigger('click')
    await flushPromises()

    expect(refundOrder).toHaveBeenCalledWith(order.id, {
      amount: 25,
      reason: 'test refund',
      deduct_balance: true,
      force: false,
    })
  })

  it('hides repeat refund for a partially refunded order and blocks an internal submit', async () => {
    const order = makeOrder('PARTIALLY_REFUNDED')
    const wrapper = mountView([order])
    await flushPromises()

    expect(buttonByText(orderRow(wrapper, order.id), 'payment.admin.refund')).toBeUndefined()
    await buttonByText(orderRow(wrapper, order.id), 'common.view')!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="refund-confirm"]').trigger('click')

    expect(refundOrder).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('payment.admin.partialRefundUnsupported:PARTIALLY_REFUNDED')
  })

  it.each(['REFUND_PENDING', 'REFUNDING', 'UNKNOWN'])('blocks ordinary refund submission for %s status', async (status) => {
    const order = makeOrder(status)
    const wrapper = mountView([order])
    await flushPromises()

    expect(buttonByText(orderRow(wrapper, order.id), 'payment.admin.refund')).toBeUndefined()
    await buttonByText(orderRow(wrapper, order.id), 'common.view')!.trigger('click')
    await flushPromises()
    await wrapper.get('[data-test="refund-confirm"]').trigger('click')

    expect(refundOrder).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith(`payment.admin.refundUnavailableForStatus:${status}`)
  })

  it('uses the query endpoint for a pending refund without calling the ordinary refund API', async () => {
    const order = makeOrder('REFUND_PENDING')
    const wrapper = mountView([order])
    await flushPromises()

    const queryButton = buttonByText(orderRow(wrapper, order.id), 'payment.admin.queryRefundStatus')
    expect(queryButton).toBeDefined()
    await queryButton!.trigger('click')
    await flushPromises()

    expect(queryRefund).toHaveBeenCalledWith(order.id)
    expect(refundOrder).not.toHaveBeenCalled()
  })
})
