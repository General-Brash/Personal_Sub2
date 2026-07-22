import { describe, expect, it } from 'vitest'
import { getPurchaseLimitItems, isPurchaseLimitExhausted } from '../purchaseLimits'

describe('purchase limits', () => {
  it('derives used and remaining counts for daily and total limits', () => {
    expect(getPurchaseLimitItems({
      daily_purchase_limit: 3,
      daily_purchase_remaining: 1,
      total_purchase_limit: 10,
      total_purchase_remaining: 7,
    })).toEqual([
      { scope: 'daily', limit: 3, remaining: 1, used: 2, exhausted: false },
      { scope: 'total', limit: 10, remaining: 7, used: 3, exhausted: false },
    ])
  })

  it('treats zero limits as unlimited and any exhausted finite limit as blocked', () => {
    expect(getPurchaseLimitItems({ daily_purchase_limit: 0, total_purchase_limit: 0 })).toEqual([])
    expect(isPurchaseLimitExhausted({ daily_purchase_limit: 1, daily_purchase_remaining: 0 })).toBe(true)
  })

  it('keeps legacy responses without remaining fields purchasable', () => {
    expect(getPurchaseLimitItems({ daily_purchase_limit: 2 })).toEqual([
      { scope: 'daily', limit: 2, remaining: 2, used: 0, exhausted: false },
    ])
  })
})
