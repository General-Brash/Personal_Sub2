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
  getBankSettings,
  getBankStatus,
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

  it('submits advance and exchange mutations with amount strings and idempotency keys', async () => {
    post
      .mockResolvedValueOnce({ data: { amount: '5.00000000' } })
      .mockResolvedValueOnce({ data: { permanent_spent: '2.00000000' } })

    await requestBankAdvance('5.00000000', 'advance-key')
    await exchangePermanentForTemporary('2.00000000', 'exchange-key')

    expect(post).toHaveBeenNthCalledWith(1, '/bank/advance', { amount: '5.00000000' }, {
      headers: { 'Idempotency-Key': 'advance-key' },
    })
    expect(post).toHaveBeenNthCalledWith(2, '/bank/exchange', { amount: '2.00000000' }, {
      headers: { 'Idempotency-Key': 'exchange-key' },
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
})
