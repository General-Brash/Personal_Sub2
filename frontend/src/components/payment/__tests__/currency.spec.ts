import { describe, expect, it } from 'vitest'
import { currencySymbol, formatPaymentAmount } from '../currency'

describe('formatPaymentAmount', () => {
  it('always displays two fraction digits for payment currencies', () => {
    expect(formatPaymentAmount(100, 'USD', 'en-US')).toBe('$100.00')
    expect(formatPaymentAmount(100, 'JPY', 'en-US')).toBe('¥100.00')
    expect(formatPaymentAmount(100, 'KRW', 'en-US')).toBe('₩100.00')
    expect(formatPaymentAmount(1.234, 'BHD', 'en-US').replace(/\s/g, '')).toBe('BHD1.23')
  })
})

describe('currencySymbol', () => {
  it('maps common payment currencies and falls back safely', () => {
    expect(currencySymbol('USD')).toBe('$')
    expect(currencySymbol('cny')).toBe('¥')
    expect(currencySymbol('EUR')).toBe('€')
    expect(currencySymbol('')).toBe('¥')
    expect(currencySymbol('XYZ')).toBe('XYZ')
  })
})
