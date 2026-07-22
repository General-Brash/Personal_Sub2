import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('bank locales', () => {
  it('exposes English navigation metadata', () => {
    expect((en as Record<string, any>).bank).toMatchObject({
      title: 'Bank',
      description: 'Manage temporary credit advances, permanent credit exchanges, and account debt.',
      operations: {
        advance: 'Temporary credit advance',
        permanentSettlement: 'Permanent credit settlement',
      },
    })
  })

  it('exposes Chinese navigation metadata', () => {
    expect((zh as Record<string, any>).bank).toMatchObject({
      title: '银行',
      description: '管理临时额度预支、永久额度兑换和账户负债。',
      operations: {
        advance: '临时额度预支',
        permanentSettlement: '永久额度结算',
      },
    })
  })

  it('keeps the English and Chinese bank key structure aligned', () => {
    const collectKeys = (value: Record<string, any>, prefix = ''): string[] =>
      Object.entries(value).flatMap(([key, nested]) => {
        const path = prefix ? `${prefix}.${key}` : key
        return nested && typeof nested === 'object' ? collectKeys(nested, path) : [path]
      })

    expect(collectKeys((en as Record<string, any>).bank).sort()).toEqual(
      collectKeys((zh as Record<string, any>).bank).sort(),
    )
  })

  it('keeps commerce purchase and shelf-limit copy aligned', () => {
    const enCommerce = (en as Record<string, any>).commerce
    const zhCommerce = (zh as Record<string, any>).commerce

    expect(enCommerce.paymentDisabled.title).toBeTruthy()
    expect(zhCommerce.paymentDisabled.title).toBeTruthy()
    expect(enCommerce.shelf.dailyPurchaseLimitHint).toContain('00:00 Beijing time')
    expect(zhCommerce.shelf.dailyPurchaseLimitHint).toContain('北京时间每日 0 点')
  })
})
