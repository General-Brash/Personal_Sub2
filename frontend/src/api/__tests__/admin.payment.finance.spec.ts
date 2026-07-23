import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get } = vi.hoisted(() => ({ get: vi.fn() }))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn(),
  },
}))

import { adminPaymentAPI } from '@/api/admin/payment'

describe('admin payment finance api', () => {
  beforeEach(() => {
    get.mockReset().mockResolvedValue({ data: {} })
  })

  it('loads a fixed-size filtered all-site ledger', async () => {
    await adminPaymentAPI.getLedger({ page: 2, user_id: 9, category: 'mall', days: 7 })

    expect(get).toHaveBeenCalledWith('/admin/payment/ledger', {
      params: { page: 2, page_size: 20, user_id: 9, category: 'mall', days: 7 },
    })
  })

  it('loads fixed-size mall transactions and sales analytics', async () => {
    await adminPaymentAPI.getMallTransactions({ page: 4, user_id: 9, product_type: 'subscription' })
    await adminPaymentAPI.getMallAnalytics(15)

    expect(get).toHaveBeenNthCalledWith(1, '/admin/payment/mall/transactions', {
      params: { page: 4, page_size: 20, user_id: 9, product_type: 'subscription' },
    })
    expect(get).toHaveBeenNthCalledWith(2, '/admin/payment/mall/analytics', {
      params: { days: 15 },
    })
  })

  it('preserves unit-aware analytics fields returned by the API', async () => {
    get.mockResolvedValueOnce({
      data: {
        days: 30,
        total_sales: 2,
        total_revenue: '',
        revenue_totals: [
          { currency: 'CNY', unit: 'fiat', revenue: '20.00', sales_count: 1 },
          { currency: '', unit: 'credit', revenue: '3.00', sales_count: 1 },
        ],
        products: [],
        daily: [],
      },
    })

    const response = await adminPaymentAPI.getMallAnalytics(30)

    expect(response.data.revenue_totals).toEqual([
      { currency: 'CNY', unit: 'fiat', revenue: '20.00', sales_count: 1 },
      { currency: '', unit: 'credit', revenue: '3.00', sales_count: 1 },
    ])
  })
})
