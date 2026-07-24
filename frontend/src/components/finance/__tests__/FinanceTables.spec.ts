import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminTransactionLogTable from '../AdminTransactionLogTable.vue'
import LedgerPieChart from '../LedgerPieChart.vue'
import LedgerWorkspace from '../LedgerWorkspace.vue'

vi.mock('vue-chartjs', () => ({
  Doughnut: { template: '<div data-test="ledger-chart" />' },
}))

const {
  appState,
  getAdminLedger,
  getLedger,
  getMallTransactions,
  getBankTransactions,
  showError,
} = vi.hoisted(() => ({
  appState: { cachedPublicSettings: null as null | Record<string, boolean> },
  getAdminLedger: vi.fn(),
  getLedger: vi.fn(),
  getMallTransactions: vi.fn(),
  getBankTransactions: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: { getLedger },
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    getLedger: getAdminLedger,
    getMallTransactions,
  },
}))

vi.mock('@/api/bank', () => ({ getBankTransactions }))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    get cachedPublicSettings() {
      return appState.cachedPublicSettings
    },
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => {
        if (params?.count !== undefined) return `${key}:${params.count}`
        if (params?.operation !== undefined) return `${key}:${params.operation}`
        if (params?.unit !== undefined) return `${key}:${params.unit}`
        return key
      },
    }),
  }
})

const DataTableStub = {
  props: ['columns', 'data', 'rowKey'],
  methods: {
    resolveRowKey(row: Record<string, unknown>) {
      return typeof this.rowKey === 'function'
        ? this.rowKey(row)
        : row[String(this.rowKey || 'id')]
    },
  },
  template: `
    <div>
      <div v-for="row in data" :key="resolveRowKey(row)" :data-row-key="resolveRowKey(row)" data-test="finance-row">
        <template v-for="column in columns" :key="column.key">
          <slot :name="'cell-' + column.key" :row="row" :value="row[column.key]" />
        </template>
      </div>
    </div>
  `,
}

const SelectStub = {
  props: ['options'],
  template: `
    <div>
      <span v-for="option in options" :key="option.value" data-test="select-option-value">{{ option.value }}</span>
    </div>
  `,
}

const commonStubs = {
  DataTable: DataTableStub,
  Pagination: true,
  Select: SelectStub,
  Icon: true,
  LedgerPieChart: true,
  RouterLink: true,
}

describe('finance tables', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    appState.cachedPublicSettings = null
    showError.mockReset()
    getAdminLedger.mockReset().mockResolvedValue({
      data: {
        timezone: 'Asia/Shanghai',
        windows: {
          today: { total_amount: '6.00', count: 3, totals: [{ currency: '', unit: 'credit', amount: '6.00', count: 3 }] },
          seven_days: { total_amount: '6.00', count: 3, totals: [{ currency: '', unit: 'credit', amount: '6.00', count: 3 }] },
          fifteen_days: { total_amount: '6.00', count: 3, totals: [{ currency: '', unit: 'credit', amount: '6.00', count: 3 }] },
        },
        summary: [
          { category: 'model', label: 'gpt fee', amount: '1.00', count: 1, currency: '', unit: 'credit' },
          { category: 'model', label: 'claude fee', amount: '2.00', count: 1, currency: '', unit: 'credit' },
          { category: 'mall', label: 'mall purchase', amount: '3.00', count: 1, currency: '', unit: 'credit' },
        ],
        items: [],
        total: 0,
        page: 1,
        page_size: 20,
        pages: 0,
      },
    })
    getLedger.mockReset().mockResolvedValue({
      data: {
        timezone: 'Asia/Shanghai',
        windows: {
          today: { total_amount: '1.00', count: 1, totals: [{ currency: '', unit: 'credit', amount: '1.00', count: 1 }] },
          seven_days: { total_amount: '1.00', count: 1, totals: [{ currency: '', unit: 'credit', amount: '1.00', count: 1 }] },
          fifteen_days: { total_amount: '1.00', count: 1, totals: [{ currency: '', unit: 'credit', amount: '1.00', count: 1 }] },
        },
        summary: [],
        items: [
          {
            id: 1,
            row_id: 'usage:1',
            source: 'model',
            category: 'model',
            label: 'gpt model fee',
            amount: '1.00',
            cost_amount: '1.00',
            currency: '',
            unit: 'credit',
            permanent_delta: null,
            temporary_delta: null,
            debt_delta: null,
            permanent_balance_before: null,
            permanent_balance_after: null,
            temporary_balance_before: null,
            temporary_balance_after: null,
            debt_before: null,
            debt_after: null,
            count: 12,
            created_at: '2026-07-23T00:00:00Z',
          },
          {
            id: 2,
            row_id: 'mall:2',
            source: 'mall',
            category: 'mall',
            label: 'Cash pack purchase',
            amount: '2.00',
            cost_amount: '2.00',
            currency: '',
            unit: 'credit',
            permanent_delta: null,
            temporary_delta: null,
            debt_delta: null,
            permanent_balance_before: null,
            permanent_balance_after: '8.00',
            temporary_balance_before: null,
            temporary_balance_after: null,
            debt_before: null,
            debt_after: null,
            count: 99,
            created_at: '2026-07-23T00:01:00Z',
          },
          {
            id: 2,
            row_id: 'bank:2',
            source: 'bank',
            category: 'bank',
            label: 'exchange',
            amount: '3.00',
            cost_amount: '3.00',
            currency: '',
            unit: 'credit',
            operation: 'exchange',
            permanent_delta: '-3.00',
            temporary_delta: '6.00',
            debt_delta: '0.00',
            permanent_balance_before: '8.00',
            permanent_balance_after: '5.00',
            temporary_balance_before: '0.00',
            temporary_balance_after: '6.00',
            debt_before: '0.00',
            debt_after: '0.00',
            count: 1,
            created_at: '2026-07-23T00:02:00Z',
          },
          {
            id: 3,
            row_id: 'bank:3',
            source: 'bank',
            category: 'bank',
            label: 'future_operation',
            amount: '1.00',
            cost_amount: '1.00',
            currency: '',
            unit: 'credit',
            operation: 'future_operation',
            permanent_delta: '-1.00',
            temporary_delta: '0.00',
            debt_delta: '0.00',
            permanent_balance_before: '5.00',
            permanent_balance_after: '4.00',
            temporary_balance_before: '6.00',
            temporary_balance_after: '6.00',
            debt_before: '0.00',
            debt_after: '0.00',
            count: 1,
            created_at: '2026-07-23T00:03:00Z',
          },
        ],
        total: 4,
        page: 1,
        page_size: 20,
        pages: 1,
      },
    })
    getMallTransactions.mockReset().mockResolvedValue({
      data: {
        items: [{
          id: 2,
          row_id: 'mall_purchase:2',
          source: 'mall_purchase',
          user_id: 9,
          username: 'alice',
          product_type: 'currency',
          product_id: 3,
          product_name: 'Cash pack',
          payment_credit_type: 'permanent',
          price: '10.00',
          currency: '',
          unit: 'credit',
          permanent_credited_amount: '12.00',
          temporary_credited_amount: '0.00',
          permanent_balance_before: '20.00',
          permanent_balance_after: '10.00',
          temporary_balance_before: null,
          temporary_balance_after: null,
          status: 'completed',
          created_at: '2026-07-23T00:00:00Z',
        }, {
          id: 2,
          row_id: 'payment_order:2',
          source: 'payment_order',
          user_id: 10,
          username: 'bob',
          product_type: 'subscription',
          product_id: 4,
          product_name: 'Pro plan',
          payment_credit_type: 'external',
          price: '25.00',
          currency: 'CNY',
          unit: 'fiat',
          permanent_credited_amount: '0.00',
          temporary_credited_amount: '0.00',
          permanent_balance_before: null,
          permanent_balance_after: null,
          temporary_balance_before: null,
          temporary_balance_after: null,
          status: 'completed',
          created_at: '2026-07-23T00:01:00Z',
        }],
        total: 2,
        page: 1,
        page_size: 20,
        pages: 1,
      },
    })
    getBankTransactions.mockReset().mockResolvedValue({
      items: [{
        id: 3,
        row_id: 'bank:3',
        source: 'bank',
        user_id: 9,
        username: 'alice',
        operation: 'exchange',
        transaction_amount: '5.00',
        currency: '',
        unit: 'credit',
        permanent_delta: '-5.00',
        temporary_delta: '3.00',
        debt_delta: '-2.00',
        permanent_balance_before: '10.00',
        permanent_balance_after: '5.00',
        temporary_balance_before: null,
        temporary_balance_after: '3.00',
        debt_before: '4.00',
        debt_after: '2.00',
        metadata: null,
        created_at: '2026-07-23T00:02:00Z',
      }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
  })

  it('formats personal-ledger timestamps in the backend Asia/Shanghai timezone', async () => {
    const localeString = vi.spyOn(Date.prototype, 'toLocaleString').mockReturnValue('SHANGHAI_TIME')
    const wrapper = mount(LedgerWorkspace, { global: { stubs: commonStubs } })

    await flushPromises()

    expect(wrapper.text()).toContain('SHANGHAI_TIME')
    expect(wrapper.get('[data-test="model-call-count"]').text()).toBe('finance.modelCallCount:12')
    expect(wrapper.findAll('[data-test="model-call-count"]')).toHaveLength(1)
    expect(wrapper.findAll('[data-test="ledger-label"]').map((item) => item.text())).toContain('finance.bankCost:bank.operations.exchange')
    expect(wrapper.findAll('[data-test="ledger-label"]').map((item) => item.text())).toContain('finance.bankCost:future_operation')
    expect(wrapper.findAll('[data-test="ledger-amount"]').every((item) => item.text().includes('finance.units.credit'))).toBe(true)
    expect(wrapper.findAll('[data-test="finance-row"]').map((item) => item.attributes('data-row-key'))).toEqual([
      'usage:1',
      'mall:2',
      'bank:2',
      'bank:3',
    ])
    expect(localeString).toHaveBeenCalledWith(undefined, { timeZone: 'Asia/Shanghai' })
  })

  it('deduplicates admin ledger filters by category', async () => {
    const wrapper = mount(LedgerWorkspace, {
      props: { admin: true },
      global: { stubs: commonStubs },
    })

    await flushPromises()

    const categoryValues = wrapper.findAll('[data-test="select-option-value"]')
      .map((option) => option.text())
      .filter(Boolean)
    expect(categoryValues).toEqual(['model', 'mall'])
  })

  it('renders separate ledger charts for credit and fiat totals', () => {
    const wrapper = mount(LedgerPieChart, {
      props: {
        summary: [
          { category: 'model', label: 'Model', amount: '2.00', count: 1, currency: '', unit: 'credit' },
          { category: 'mall', label: 'Mall CNY', amount: '30.00', count: 1, currency: 'CNY', unit: 'fiat' },
        ],
      },
      global: { stubs: {} },
    })

    expect(wrapper.findAll('[data-test="ledger-chart"]')).toHaveLength(2)
    expect(wrapper.text()).toContain('2.00 finance.units.credit')
    expect(wrapper.text()).toContain('¥30.00')
  })

  it('renders the mall product branch once and uses the same fixed timezone', async () => {
    const localeString = vi.spyOn(Date.prototype, 'toLocaleString').mockReturnValue('SHANGHAI_TIME')
    const wrapper = mount(AdminTransactionLogTable, {
      props: { kind: 'mall' },
      global: { stubs: commonStubs },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('Cash pack')
    expect(wrapper.text()).toContain('finance.transactions.currency')
    expect(wrapper.text()).toContain('SHANGHAI_TIME')
    expect(wrapper.findAll('[data-test="transaction-amount"]').map((item) => item.text())).toEqual([
      '10.00 finance.units.credit',
      '¥25.00',
    ])
    expect(wrapper.findAll('[data-test="finance-row"]').map((item) => item.attributes('data-row-key'))).toEqual([
      'mall_purchase:2',
      'payment_order:2',
    ])
    expect(wrapper.get('[data-test="credited-change"]').text()).toContain('P +12.00 / T +0.00')
    expect(wrapper.get('[data-test="permanent-balance-change"]').text()).toContain('20.00 → 10.00')
    expect(wrapper.get('[data-test="temporary-balance-change"]').text()).toContain('finance.historyUnavailable → finance.historyUnavailable')
    expect(localeString).toHaveBeenCalledWith(undefined, { timeZone: 'Asia/Shanghai' })
  })

  it('uses the explicit bank transaction amount and renders all before-to-after balances once', async () => {
    const wrapper = mount(AdminTransactionLogTable, {
      props: { kind: 'bank' },
      global: { stubs: commonStubs },
    })

    await flushPromises()

    expect(wrapper.get('[data-test="transaction-amount"]').text()).toBe('5.00 finance.units.credit')
    expect(wrapper.findAll('[data-test="transaction-amount"]')).toHaveLength(1)
    expect(wrapper.get('[data-test="bank-operation"]').text()).toBe('bank.operations.exchange')
    expect(wrapper.find('[data-test="credited-change"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="permanent-balance-change"]').text()).toContain('10.00 → 5.00')
    expect(wrapper.get('[data-test="temporary-balance-change"]').text()).toContain('finance.historyUnavailable → 3.00')
    expect(wrapper.get('[data-test="debt-balance-change"]').text()).toContain('4.00 → 2.00')
  })

  it.each([
    ['mall', 2],
    ['bank', 1],
  ] as const)('keeps administrator finance user links for %s logs before settings load', async (kind, expectedLinks) => {
    const wrapper = mount(AdminTransactionLogTable, {
      props: { kind },
      global: { stubs: commonStubs },
    })

    await flushPromises()

    expect(wrapper.findAll('[data-test="admin-finance-user-link"]')).toHaveLength(expectedLinks)
    expect(wrapper.find('[data-test="admin-finance-user-text"]').exists()).toBe(false)
  })

  it.each([
    ['mall', 2],
    ['bank', 1],
  ] as const)('renders plain user text for %s logs when administrator finance is disabled', async (kind, expectedRows) => {
    appState.cachedPublicSettings = { admin_finance_enabled: false }
    const wrapper = mount(AdminTransactionLogTable, {
      props: { kind },
      global: { stubs: commonStubs },
    })

    await flushPromises()

    expect(wrapper.find('[data-test="admin-finance-user-link"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-test="admin-finance-user-text"]')).toHaveLength(expectedRows)
  })
})
