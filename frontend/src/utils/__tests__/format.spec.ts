import { beforeEach, describe, expect, it, vi } from 'vitest'

const getLocaleMock = vi.hoisted(() => vi.fn(() => 'en'))

vi.mock('@/i18n', () => ({
  getLocale: getLocaleMock,
  i18n: { global: { t: (key: string) => key } },
}))

import { formatCurrency, formatDecimalAmount, formatMoneyDisplay } from '../format'

describe('formatCurrency', () => {
  beforeEach(() => {
    getLocaleMock.mockReturnValue('en')
  })

  it('keeps English currency grouping with fixed two-place output', () => {
    expect(formatCurrency(1234567.8, 'USD', 2)).toBe('$1,234,567.80')
  })

  it('uses the locale-aware USD symbol in Chinese', () => {
    getLocaleMock.mockReturnValue('zh')

    expect(formatCurrency(1234567.8, 'USD', 2)).toBe('US$1,234,567.80')
  })
})

describe('formatMoneyDisplay', () => {
  it('rounds decimal strings beyond the Number safe-integer range without losing digits', () => {
    expect(formatMoneyDisplay('90071992547409931234567890.12500000')).toBe(
      '90071992547409931234567890.13',
    )
  })
})

describe('formatDecimalAmount', () => {
  it('rounds decimal strings to two places without converting the amount to Number', () => {
    expect(formatDecimalAmount('1.23499999')).toBe('1.23')
    expect(formatDecimalAmount('1.23500000')).toBe('1.24')
  })

  it('carries rounding into the integer portion', () => {
    expect(formatDecimalAmount('9.99999999')).toBe('10.00')
    expect(formatDecimalAmount('999999999999.99999999')).toBe('1000000000000.00')
  })

  it('rounds negative values and suppresses negative zero', () => {
    expect(formatDecimalAmount('-1.23500000')).toBe('-1.24')
    expect(formatDecimalAmount('-0.00499999')).toBe('0.00')
  })

  it('renders very small values as fixed two-place amounts', () => {
    expect(formatDecimalAmount('0.00000001')).toBe('0.00')
    expect(formatDecimalAmount('0.00500000')).toBe('0.01')
    expect(formatDecimalAmount(1e-8)).toBe('0.00')
  })

  it('returns a stable zero for absent or invalid display values', () => {
    expect(formatDecimalAmount(null)).toBe('0.00')
    expect(formatDecimalAmount('not-an-amount')).toBe('0.00')
    expect(formatDecimalAmount(Number.POSITIVE_INFINITY)).toBe('0.00')
  })
})
