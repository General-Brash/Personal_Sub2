/**
 * formatScaled formats a per-token (or per-request) USD price scaled by `scale`.
 *
 *   formatScaled(0.000003, 1_000_000) → "$3.00"     // per 1M tokens
 *   formatScaled(0.5,        1)        → "$0.50"     // per request
 *   formatScaled(null,       1_000_000) → "-"
 *
 * Keeps calculations untouched and rounds only at the display boundary.
 */
import { formatMoneyDisplay } from '@/utils/format'

export function formatScaled(value: number | null, scale: number): string {
  if (value == null) return '-'
  return `$${formatMoneyDisplay(value * scale)}`
}
