import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { paymentAPI } from '@/api/payment'

describe('payment api', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    get.mockResolvedValue({ data: {} })
    post.mockResolvedValue({ data: {} })
  })

  it('keeps legacy public out_trade_no verification for upgrade compatibility', async () => {
    await paymentAPI.verifyOrderPublic('legacy-order-no')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/verify', {
      out_trade_no: 'legacy-order-no',
    })
  })

  it('keeps signed public resume-token resolve endpoint', async () => {
    await paymentAPI.resolveOrderPublicByResumeToken('resume-token-123')

    expect(post).toHaveBeenCalledWith('/payment/public/orders/resolve', {
      resume_token: 'resume-token-123',
    })
  })

  it('loads the read-only mall balance summary', async () => {
    await paymentAPI.getMallBalance()

    expect(get).toHaveBeenCalledWith('/mall/balance')
  })

  it('submits an internal mall purchase with the caller idempotency key', async () => {
    await paymentAPI.purchaseMallProduct({ product_type: 'currency', product_id: 12 }, 'mall-purchase-12')

    expect(post).toHaveBeenCalledWith(
      '/mall/purchases',
      { product_type: 'currency', product_id: 12 },
      { headers: { 'Idempotency-Key': 'mall-purchase-12' } },
    )
  })

  it('loads a fixed-size personal ledger window', async () => {
    await paymentAPI.getLedger({ page: 3, days: 15 })

    expect(get).toHaveBeenCalledWith('/user/ledger', {
      params: { page: 3, page_size: 20, days: 15 },
    })
  })
})
