import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post, put } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, post, put },
}))

import {
  exchangePermanentForTemporary,
  getBankLedger,
  getBankSettings,
  getBankStatus,
  getBankTransactions,
  repayBankDebt,
  requestBankAdvance,
  updateBankSettings,
  type BankPolicy,
} from '@/api/bank'

describe('bank api', () => {
  const policy: BankPolicy = {
    advance_min_amount: '5.00000000',
    advance_max_amount: '20.00000000',
    debt_grace_days: 3,
    debt_conversion_ratio: '1.25000000',
    exchange_rate: '2.00000000',
    unused_advance_debt_reduction_ratio: '0.75000000',
    early_repay_temporary_ratio: '1.00000000',
    early_repay_permanent_ratio: '2.00000000',
  }

  beforeEach(() => {
    get.mockReset()
    post.mockReset()
    put.mockReset()
  })

  it('loads bank status without changing decimal strings', async () => {
    const response = {
      permanent_balance: '12.34567890',
      temporary_credit_available: '3.00000000',
      temporary_credit_earliest_expires_at: null,
      temporary_debt: '0.00000000',
      temporary_debt_due_at: null,
      active_advance: null,
      policy,
      ledger: [],
    }
    get.mockResolvedValue({ data: response })

    const result = await getBankStatus()

    expect(get).toHaveBeenCalledWith('/bank/status')
    expect(result).toEqual(response)
    expect(result.permanent_balance).toBe('12.34567890')
  })

  it('loads a fixed five-row user ledger page without sending a page size', async () => {
    const response = {
      items: [{ id: 6, permanent_delta: '-1.00000000' }],
      total: 11,
      page: 2,
      page_size: 5 as const,
      pages: 3,
    }
    get.mockResolvedValue({ data: response })

    const result = await getBankLedger(2)

    expect(get).toHaveBeenCalledWith('/bank/ledger', { params: { page: 2 } })
    expect(result).toEqual(response)
  })

  it('submits bank mutations with amount strings and idempotency keys', async () => {
    post
      .mockResolvedValueOnce({ data: { amount: '5.00000000' } })
      .mockResolvedValueOnce({ data: { permanent_spent: '2.00000000' } })
      .mockResolvedValueOnce({ data: { credit_spent: '1.00000000' } })

    await requestBankAdvance('5.00000000', 'advance-key')
    await exchangePermanentForTemporary('2.00000000', 'exchange-key')
    await repayBankDebt('temporary', '1.00000000', 'repay-key')

    expect(post).toHaveBeenNthCalledWith(1, '/bank/advance', { amount: '5.00000000' }, {
      headers: { 'Idempotency-Key': 'advance-key' },
    })
    expect(post).toHaveBeenNthCalledWith(2, '/bank/exchange', { amount: '2.00000000' }, {
      headers: { 'Idempotency-Key': 'exchange-key' },
    })
    expect(post).toHaveBeenNthCalledWith(3, '/bank/repay', {
      source: 'temporary',
      amount: '1.00000000',
    }, {
      headers: { 'Idempotency-Key': 'repay-key' },
    })
  })

  it('loads and atomically updates the dedicated admin bank policy', async () => {
    get.mockResolvedValue({ data: policy })
    put.mockResolvedValue({ data: policy })

    expect(await getBankSettings()).toEqual(policy)
    expect(get).toHaveBeenCalledWith('/admin/settings/bank')

    expect(await updateBankSettings(policy, 'settings-key')).toEqual(policy)
    expect(put).toHaveBeenCalledWith('/admin/settings/bank', policy, {
      headers: { 'Idempotency-Key': 'settings-key' },
    })
  })

  it('loads the fixed-size admin bank transaction log', async () => {
    const response = {
      items: [{ transaction_amount: '5.00000000' }],
      total: 1,
      page: 2,
      page_size: 20,
      pages: 1,
    }
    get.mockResolvedValue({ data: response })

    const result = await getBankTransactions({ page: 2, user_id: 9 })

    expect(get).toHaveBeenCalledWith('/admin/settings/bank/transactions', {
      params: { page: 2, page_size: 20, user_id: 9 },
    })
    expect(result).toEqual(response)
    expect(result.items[0]?.transaction_amount).toBe('5.00000000')
  })
})
