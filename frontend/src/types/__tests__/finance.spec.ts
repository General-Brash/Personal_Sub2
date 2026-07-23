import { describe, expect, it } from 'vitest'

import type { LedgerItem, MallAnalyticsResponse, MallTransactionItem } from '../finance'

describe('finance DTO types', () => {
  it('require stable row ids and explicit amount descriptors', () => {
    const ledgerItem = {
      id: 7,
      row_id: 'bank:7',
      source: 'bank',
      category: 'bank',
      label: 'exchange',
      amount: '1.00',
      cost_amount: '1.00',
      currency: '',
      unit: 'credit',
      permanent_delta: '-1.00',
      temporary_delta: '2.00',
      debt_delta: '0.00',
      permanent_balance_before: '3.00',
      permanent_balance_after: '2.00',
      temporary_balance_before: '0.00',
      temporary_balance_after: '2.00',
      debt_before: '0.00',
      debt_after: '0.00',
      created_at: '2026-07-23T00:00:00Z',
    } satisfies LedgerItem
    const mallTransaction = {
      id: -4,
      row_id: 'payment_order:4',
      source: 'payment_order',
      user_id: 1,
      username: 'alice',
      product_type: 'subscription',
      product_id: 2,
      product_name: 'Pro',
      payment_credit_type: 'external',
      price: '20.00',
      currency: 'CNY',
      unit: 'fiat',
      permanent_credited_amount: '0.00',
      temporary_credited_amount: '0.00',
      permanent_balance_before: null,
      permanent_balance_after: null,
      temporary_balance_before: null,
      temporary_balance_after: null,
      status: 'completed',
      created_at: '2026-07-23T00:00:00Z',
    } satisfies MallTransactionItem

    expect(ledgerItem.row_id).toBe('bank:7')
    expect(mallTransaction.currency).toBe('CNY')
  })

  it('models analytics revenue as separate currency and unit totals', () => {
    const analytics = {
      days: 30,
      total_sales: 2,
      total_revenue: '',
      revenue_totals: [
        { currency: 'CNY', unit: 'fiat', revenue: '20.00', sales_count: 1 },
        { currency: '', unit: 'credit', revenue: '2.00', sales_count: 1 },
      ],
      products: [],
      daily: [],
    } satisfies MallAnalyticsResponse

    expect(analytics.revenue_totals).toHaveLength(2)
  })
})
