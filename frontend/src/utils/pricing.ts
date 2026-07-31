/**
 * formatScaled formats a per-token (or per-request) USD price scaled by `scale`.
 *
 *   formatScaled(0.000003, 1_000_000) → "$3.00"     // per 1M tokens
 *   formatScaled(0.5,        1)        → "$0.50"     // per request
 *   formatScaled(null,       1_000_000) → "-"
 *   formatScaled(1.25e-8, 1_000_000, 2) → "$0.0125" // preserve useful precision
 *
 * Calls without `minFractionDigits` keep the legacy two-decimal display.
 * Passing it preserves significant decimals and pads to the requested minimum.
 */
import { formatMoneyDisplay } from '@/utils/format'

export function formatScaled(
  value: number | null,
  scale: number,
  minFractionDigits?: number,
): string {
  if (value == null) return '-'
  if (minFractionDigits == null) return `$${formatMoneyDisplay(value * scale)}`

  let s = (value * scale).toPrecision(10).replace(/\.?0+$/, '')
  if (minFractionDigits > 0 && !s.includes('e')) {
    const dot = s.indexOf('.')
    const digits = dot === -1 ? 0 : s.length - dot - 1
    if (digits < minFractionDigits) {
      s = (dot === -1 ? `${s}.` : s) + '0'.repeat(minFractionDigits - digits)
    }
  }
  return `$${s}`
}
