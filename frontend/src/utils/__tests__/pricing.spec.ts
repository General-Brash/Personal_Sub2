import { describe, expect, it } from 'vitest'

import { formatScaled } from '@/utils/pricing'
import {
  calculateTokenPricePerMillion,
  formatTokenPricePerMillion
} from '@/utils/usagePricing'

describe('price display formatting', () => {
  it('formats scaled prices with two decimal places', () => {
    expect(formatScaled(0.000003456789, 1_000_000)).toBe('$3.46')
    expect(formatScaled(0.5, 1)).toBe('$0.50')
    expect(formatScaled(null, 1_000_000)).toBe('-')
  })

  it('keeps per-million calculations precise and rounds only the display', () => {
    const calculated = calculateTokenPricePerMillion(1.23456789, 1_000_000)

    expect(calculated).toBeCloseTo(1.23456789, 8)
    expect(formatTokenPricePerMillion(1.23456789, 1_000_000)).toBe('$1.23')
    expect(
      formatTokenPricePerMillion(1.23456789, 1_000_000, { withCurrencySymbol: false })
    ).toBe('1.23')
  })
})
