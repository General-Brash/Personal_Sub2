import { describe, expect, it } from 'vitest'
import { getPurchaseLimitItems, getPurchaseLimitPolicy, isPurchaseLimitExhausted } from '../purchaseLimits'

describe('purchase limits', () => {
  it('defaults legacy responses to one natural day and preserves total progress', () => {
    expect(getPurchaseLimitItems({
      daily_purchase_limit: 3,
      daily_purchase_remaining: 1,
      total_purchase_limit: 10,
      total_purchase_remaining: 7,
    })).toEqual([
      { scope: 'periodic', limit: 3, remaining: 1, used: 2, exhausted: false, unit: 'day', mode: 'calendar', windowSize: 1 },
      { scope: 'total', limit: 10, remaining: 7, used: 3, exhausted: false, unit: 'day', mode: 'calendar', windowSize: 1 },
    ])
  })

  it.each([
    ['week', 'calendar', 1],
    ['month', 'calendar', 1],
    ['day', 'rolling', 3],
    ['week', 'rolling', 2],
    ['month', 'rolling', 4],
  ] as const)('normalizes %s %s policy with window size %i', (unit, mode, windowSize) => {
    expect(getPurchaseLimitPolicy({
      purchase_limit_unit: unit,
      purchase_limit_mode: mode,
      purchase_limit_window_size: windowSize,
    })).toEqual({ unit, mode, windowSize })
  })

  it('falls back to a one-unit rolling window when an old or malformed response has no positive length', () => {
    expect(getPurchaseLimitPolicy({ purchase_limit_unit: 'week', purchase_limit_mode: 'rolling', purchase_limit_window_size: 0 }))
      .toEqual({ unit: 'week', mode: 'rolling', windowSize: 1 })
  })

  it('treats zero limits as unlimited and any exhausted finite limit as blocked', () => {
    expect(getPurchaseLimitItems({ daily_purchase_limit: 0, total_purchase_limit: 0 })).toEqual([])
    expect(isPurchaseLimitExhausted({ daily_purchase_limit: 1, daily_purchase_remaining: 0 })).toBe(true)
  })

  it('keeps legacy responses without remaining fields purchasable', () => {
    expect(getPurchaseLimitItems({ daily_purchase_limit: 2 })).toEqual([
      { scope: 'periodic', limit: 2, remaining: 2, used: 0, exhausted: false, unit: 'day', mode: 'calendar', windowSize: 1 },
    ])
  })
})
