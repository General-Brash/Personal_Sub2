import type { PurchaseLimitFields } from '@/types/payment'

export type PurchaseLimitScope = 'daily' | 'total'

export interface PurchaseLimitItem {
  scope: PurchaseLimitScope
  limit: number
  remaining: number
  used: number
  exhausted: boolean
}

function normalizeCount(value: number | undefined): number {
  return Number.isFinite(value) && Number.isInteger(value) && Number(value) > 0
    ? Number(value)
    : 0
}

function buildLimitItem(
  scope: PurchaseLimitScope,
  limitValue: number | undefined,
  remainingValue: number | undefined,
): PurchaseLimitItem | null {
  const limit = normalizeCount(limitValue)
  if (limit === 0) return null

  const rawRemaining = Number.isFinite(remainingValue) && Number.isInteger(remainingValue)
    ? Number(remainingValue)
    : limit
  const remaining = Math.min(limit, Math.max(0, rawRemaining))
  return {
    scope,
    limit,
    remaining,
    used: limit - remaining,
    exhausted: remaining === 0,
  }
}

export function getPurchaseLimitItems(value: PurchaseLimitFields): PurchaseLimitItem[] {
  return [
    buildLimitItem('daily', value.daily_purchase_limit, value.daily_purchase_remaining),
    buildLimitItem('total', value.total_purchase_limit, value.total_purchase_remaining),
  ].filter((item): item is PurchaseLimitItem => item !== null)
}

export function isPurchaseLimitExhausted(value: PurchaseLimitFields): boolean {
  return getPurchaseLimitItems(value).some((item) => item.exhausted)
}
