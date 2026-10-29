import { formatMoneyDisplay } from '@/utils/format'

export const TOKENS_PER_MILLION = 1_000_000
export const USAGE_COST_FRACTION_DIGITS = 4

export function formatUsageCost(amount: string | number | null | undefined): string {
  return formatMoneyDisplay(amount, USAGE_COST_FRACTION_DIGITS)
}

interface TokenPriceFormatOptions {
  fractionDigits?: number
  withCurrencySymbol?: boolean
  emptyValue?: string
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value)
}

export function calculateTokenUnitPrice(
  cost: number | null | undefined,
  tokens: number | null | undefined
): number | null {
  if (!isFiniteNumber(cost) || cost < 0 || !isFiniteNumber(tokens) || tokens <= 0) {
    return null
  }

  const price = cost / tokens
  return Number.isFinite(price) ? price : null
}

export function calculateTokenPricePerMillion(
  cost: number | null | undefined,
  tokens: number | null | undefined
): number | null {
  const unitPrice = calculateTokenUnitPrice(cost, tokens)
  if (unitPrice == null) {
    return null
  }

  const price = unitPrice * TOKENS_PER_MILLION
  return Number.isFinite(price) ? price : null
}

export function formatTokenPricePerMillion(
  cost: number | null | undefined,
  tokens: number | null | undefined,
  options: TokenPriceFormatOptions = {}
): string {
  const pricePerMillion = calculateTokenPricePerMillion(cost, tokens)
  if (pricePerMillion == null) {
    return options.emptyValue ?? '-'
  }

  const fractionDigits = options.fractionDigits ?? USAGE_COST_FRACTION_DIGITS
  const formatted = formatMoneyDisplay(pricePerMillion, fractionDigits)
  return options.withCurrencySymbol == false ? formatted : `$${formatted}`
}
