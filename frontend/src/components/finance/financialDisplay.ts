import { currencySymbol, normalizePaymentCurrency } from '@/components/payment/currency'
import type { FinancialAmountDescriptor, LedgerAmount } from '@/types/finance'
import { formatMoneyDisplay } from '@/utils/format'

export type FinanceTranslate = (key: string, params?: Record<string, unknown>) => string

export function financialAmountKey(value: FinancialAmountDescriptor): string {
  const unit = String(value.unit || '').trim().toLowerCase() || 'unknown'
  const currency = String(value.currency || '').trim().toUpperCase()
  return `${unit}:${currency}`
}

export function financialUnitLabel(
  currency: string | null | undefined,
  unit: string | null | undefined,
  t: FinanceTranslate,
): string {
  const normalizedCurrency = String(currency || '').trim().toUpperCase()
  if (unit === 'fiat' || normalizedCurrency) return normalizePaymentCurrency(normalizedCurrency)
  if (unit === 'credit') return t('finance.units.credit')
  return String(unit || '').trim() || t('finance.units.unknown')
}

export function formatFinancialAmount(
  value: LedgerAmount | null | undefined,
  currency: string | null | undefined,
  unit: string | null | undefined,
  t: FinanceTranslate,
): string {
  const amount = formatMoneyDisplay(value ?? 0)
  const normalizedCurrency = String(currency || '').trim().toUpperCase()
  if (unit === 'fiat' || normalizedCurrency) {
    const code = normalizePaymentCurrency(normalizedCurrency)
    const symbol = currencySymbol(code)
    return symbol === code ? `${code} ${amount}` : `${symbol}${amount}`
  }
  return `${amount} ${financialUnitLabel('', unit, t)}`
}

export function sumFinancialAmounts(values: LedgerAmount[]): string {
  const scale = 100000000n
  const total = values.reduce((sum, value) => {
    const normalized = formatMoneyDisplay(value, 8)
    const match = normalized.match(/^(-?)(\d+)\.(\d{8})$/)
    if (!match) return sum
    const scaled = BigInt(match[2]) * scale + BigInt(match[3])
    return sum + (match[1] === '-' ? -scaled : scaled)
  }, 0n)
  const negative = total < 0n
  const absolute = negative ? -total : total
  return `${negative ? '-' : ''}${absolute / scale}.${String(absolute % scale).padStart(8, '0')}`
}
