import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

function collectKeys(value: Record<string, any>, prefix = ''): string[] {
  return Object.entries(value).flatMap(([key, nested]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return nested && typeof nested === 'object' ? collectKeys(nested, path) : [path]
  })
}

describe('finance locales', () => {
  it('provides unit-aware analytics and bank-ledger labels in both locales', () => {
    expect((zh as Record<string, any>).finance).toMatchObject({
      bankCost: '银行{operation}消费',
      bankActivity: '银行{operation}',
      units: { credit: '额度' },
      analytics: { revenueUnit: '收入 · {unit}', dailyUnit: '每日趋势 · {unit}' },
    })
    expect((en as Record<string, any>).finance).toMatchObject({
      bankCost: 'Bank {operation} cost',
      bankActivity: 'Bank {operation}',
      units: { credit: 'credit' },
      analytics: { revenueUnit: 'Revenue · {unit}', dailyUnit: 'Daily trend · {unit}' },
    })
  })

  it('keeps the English and Chinese finance key structure aligned', () => {
    expect(collectKeys((en as Record<string, any>).finance).sort()).toEqual(
      collectKeys((zh as Record<string, any>).finance).sort(),
    )
  })
})
