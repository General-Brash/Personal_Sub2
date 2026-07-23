import { describe, expect, it, vi, beforeEach } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import AdminCurrencyProductsView from '../AdminCurrencyProductsView.vue'
import type { CurrencyProduct } from '@/types/payment'

const { getCurrencyProducts, getMallAnalytics, createCurrencyProduct, updateCurrencyProduct, deleteCurrencyProduct, showError, showSuccess } = vi.hoisted(() => ({
  getCurrencyProducts: vi.fn(),
  getMallAnalytics: vi.fn(),
  createCurrencyProduct: vi.fn(),
  updateCurrencyProduct: vi.fn(),
  deleteCurrencyProduct: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => {
  const paymentAPI = {
    getCurrencyProducts,
    getMallAnalytics,
    createCurrencyProduct,
    updateCurrencyProduct,
    deleteCurrencyProduct,
  }
  return { adminPaymentAPI: paymentAPI, default: paymentAPI }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const product: CurrencyProduct = {
  id: 7,
  name: 'Starter credit',
  description: 'A small permanent balance boost',
  payment_price: 9.99,
  payment_credit_type: 'temporary',
  credited_type: 'permanent',
  credited_amount: 12.5,
  sort_order: 2,
  is_active: true,
  for_sale: true,
  daily_purchase_limit: 2,
  total_purchase_limit: 5,
}

const DataTableStub = {
  props: ['columns', 'data', 'loading'],
  template: `
    <div data-test="currency-products-table">
      <div v-if="!loading && data.length === 0">
        <slot name="empty"><p data-test="generic-empty">empty.noData</p></slot>
      </div>
      <div v-for="row in data" v-else :key="row.id" :data-test="'currency-product-row-' + row.id">
        <slot name="cell-name" :row="row" :value="row.name" />
        <slot name="cell-payment_price" :row="row" :value="row.payment_price" />
        <slot name="cell-credited_amount" :row="row" :value="row.credited_amount" />
        <slot name="cell-actions" :row="row" :value="row.actions" />
      </div>
    </div>
  `,
}

const BaseDialogStub = {
  props: ['show', 'title'],
  template: '<div v-if="show" data-test="currency-product-dialog"><slot /><slot name="footer" /></div>',
}

const ConfirmDialogStub = {
  props: ['show'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="delete-confirmation">
      <button data-test="confirm-delete" type="button" @click="$emit('confirm')">confirm</button>
      <button data-test="cancel-delete" type="button" @click="$emit('cancel')">cancel</button>
    </div>
  `,
}

function mountView() {
  return mount(AdminCurrencyProductsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        RouterLink: { template: '<a><slot /></a>' },
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        ConfirmDialog: ConfirmDialogStub,
        ShelfSectionTabs: true,
        DailyRevenueChart: {
          props: ['data', 'title', 'revenueLabel'],
          template: '<div data-test="daily-revenue-chart">{{ title }}|{{ revenueLabel }}|{{ data.length }}</div>',
        },
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: '<select :value="modelValue" v-bind="$attrs" @change="$emit(\'update:modelValue\', $event.target.value)"><option v-for="option in options" :key="option.value" :value="option.value">{{ option.label }}</option></select>',
        },
        Toggle: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template: '<button type="button" data-test="toggle" @click="$emit(\'update:modelValue\', !modelValue)">{{ modelValue }}</button>',
        },
        Icon: true,
      },
    },
  })
}

describe('AdminCurrencyProductsView', () => {
  beforeEach(() => {
    getCurrencyProducts.mockReset().mockResolvedValue({ data: [product] })
    getMallAnalytics.mockReset().mockResolvedValue({
      data: {
        days: 30,
        total_sales: 3,
        total_revenue: '',
        revenue_totals: [
          { currency: 'CNY', unit: 'fiat', revenue: '30.00', sales_count: 2 },
          { currency: '', unit: 'credit', revenue: '4.00', sales_count: 1 },
        ],
        products: [
          { product_type: 'currency', product_id: 7, product_name: 'Starter credit', sales_count: 2, revenue: '30.00', currency: 'CNY', unit: 'fiat' },
          { product_type: 'subscription', product_id: 8, product_name: 'Pro plan', sales_count: 1, revenue: '4.00', currency: '', unit: 'credit' },
        ],
        daily: [
          { date: '2026-07-22', sales_count: 2, revenue: '30.00', currency: 'CNY', unit: 'fiat' },
          { date: '2026-07-22', sales_count: 1, revenue: '4.00', currency: '', unit: 'credit' },
        ],
      },
    })
    createCurrencyProduct.mockReset().mockResolvedValue({ data: product })
    updateCurrencyProduct.mockReset().mockResolvedValue({ data: product })
    deleteCurrencyProduct.mockReset().mockResolvedValue({ data: null })
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('loads and renders the current currency products', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(getCurrencyProducts).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test="currency-product-row-7"]').text()).toContain('Starter credit')
    expect(wrapper.get('[data-test="currency-product-row-7"]').text()).toContain('commerce.creditType.temporary')
    expect(wrapper.get('[data-test="currency-product-row-7"]').text()).toContain('commerce.creditType.permanent')
  })

  it('renders one explicit currency-product empty state without the generic duplicate', async () => {
    getCurrencyProducts.mockResolvedValueOnce({ data: [] })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.findAll('[data-test="currency-products-empty"]')).toHaveLength(1)
    expect(wrapper.get('[data-test="currency-products-empty"]').text()).toBe('commerce.shelf.empty')
    expect(wrapper.find('[data-test="generic-empty"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('empty.noData')
  })

  it('keeps analytics revenue separated by fiat currency and credit unit', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="toggle-shelf-analytics"]').trigger('click')
    await flushPromises()

    const totals = wrapper.findAll('[data-test="analytics-revenue-total"]').map((item) => item.text())
    expect(totals).toEqual([
      'finance.analytics.revenueUnit¥30.00',
      'finance.analytics.revenueUnit4.00 finance.units.credit',
    ])
    expect(wrapper.text()).not.toContain('$34.00')
    expect(wrapper.findAll('[data-test="daily-revenue-chart"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('2 / ¥30.00')
    expect(wrapper.text()).toContain('1 / 4.00 finance.units.credit')
  })

  it('creates a product from the publish form and refreshes the list', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="create-currency-product"]').trigger('click')
    expect(wrapper.get('[data-test="currency-product-name"]').attributes('maxlength')).toBe('100')
    await wrapper.get('[data-test="currency-product-name"]').setValue('New credit')
    await wrapper.get('[data-test="currency-product-description"]').setValue('Published from the shelf')
    await wrapper.get('[data-test="currency-product-price"]').setValue('4.50')
    await wrapper.get('[data-test="currency-product-credit"]').setValue('6.25')
    await wrapper.get('[data-test="currency-product-payment-type"]').setValue('temporary')
    await wrapper.get('[data-test="currency-product-credited-type"]').setValue('temporary')
    await wrapper.get('[data-test="currency-product-sort"]').setValue('3')
    await wrapper.get('[data-test="currency-product-daily-limit"]').setValue('2')
    await wrapper.get('[data-test="currency-product-total-limit"]').setValue('5')
    await wrapper.get('[data-test="save-currency-product"]').trigger('click')
    await flushPromises()

    expect(createCurrencyProduct).toHaveBeenCalledWith({
      name: 'New credit',
      description: 'Published from the shelf',
      payment_price: 4.5,
      payment_credit_type: 'temporary',
      credited_type: 'temporary',
      credited_amount: 6.25,
      sort_order: 3,
      is_active: true,
      for_sale: true,
      daily_purchase_limit: 2,
      total_purchase_limit: 5,
    })
    expect(getCurrencyProducts).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalledWith('commerce.shelf.saved')
  })

  it('edits an existing product with its id and updated values', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="edit-currency-product-7"]').trigger('click')
    expect(wrapper.get('[data-test="currency-product-name"]').element).toHaveProperty('value', 'Starter credit')
    expect(wrapper.get('[data-test="currency-product-daily-limit"]').element).toHaveProperty('value', '2')
    expect(wrapper.get('[data-test="currency-product-total-limit"]').element).toHaveProperty('value', '5')
    expect(wrapper.get('[data-test="currency-product-payment-type"]').element).toHaveProperty('value', 'temporary')
    expect(wrapper.get('[data-test="currency-product-credited-type"]').element).toHaveProperty('value', 'permanent')
    await wrapper.get('[data-test="currency-product-name"]').setValue('Updated credit')
    await wrapper.get('[data-test="currency-product-price"]').setValue('10.25')
    await wrapper.get('[data-test="save-currency-product"]').trigger('click')
    await flushPromises()

    expect(updateCurrencyProduct).toHaveBeenCalledWith(7, expect.objectContaining({
      name: 'Updated credit',
      payment_price: 10.25,
      payment_credit_type: 'temporary',
      credited_type: 'permanent',
      credited_amount: 12.5,
      sort_order: 2,
      is_active: true,
      for_sale: true,
      daily_purchase_limit: 2,
      total_purchase_limit: 5,
    }))
    expect(getCurrencyProducts).toHaveBeenCalledTimes(2)
  })

  it('deletes a product only after confirmation and refreshes the list', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="delete-currency-product-7"]').trigger('click')
    expect(wrapper.get('[data-test="delete-confirmation"]').exists()).toBe(true)
    await wrapper.get('[data-test="confirm-delete"]').trigger('click')
    await flushPromises()

    expect(deleteCurrencyProduct).toHaveBeenCalledWith(7)
    expect(getCurrencyProducts).toHaveBeenCalledTimes(2)
    expect(showSuccess).toHaveBeenCalledWith('commerce.shelf.deleted')
  })
})
