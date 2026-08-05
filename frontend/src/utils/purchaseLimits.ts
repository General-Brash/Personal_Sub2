import type { PurchaseLimitFields, PurchaseLimitMode, PurchaseLimitUnit } from '@/types/payment'

export type PurchaseLimitScope = 'periodic' | 'total'

export interface PurchaseLimitPolicy {
  unit: PurchaseLimitUnit
  mode: PurchaseLimitMode
  windowSize: number
}

export interface PurchaseLimitItem extends PurchaseLimitPolicy {
  scope: PurchaseLimitScope
  limit: number
  remaining: number
  used: number
  exhausted: boolean
}

export type PurchaseLimitLabelKey =
  | 'calendarDay'
  | 'calendarWeek'
  | 'calendarMonth'
  | 'rollingDay'
  | 'rollingWeek'
  | 'rollingMonth'
  | 'total'

const purchaseLimitUnits: readonly PurchaseLimitUnit[] = ['day', 'week', 'month']
const purchaseLimitModes: readonly PurchaseLimitMode[] = ['calendar', 'rolling']

function normalizeCount(value: number | undefined): number {
  return Number.isFinite(value) && Number.isInteger(value) && Number(value) > 0
    ? Number(value)
    : 0
}

function normalizedUnit(value: PurchaseLimitFields['purchase_limit_unit']): PurchaseLimitUnit {
  return purchaseLimitUnits.includes(value as PurchaseLimitUnit) ? value as PurchaseLimitUnit : 'day'
}

function normalizedMode(value: PurchaseLimitFields['purchase_limit_mode']): PurchaseLimitMode {
  return purchaseLimitModes.includes(value as PurchaseLimitMode) ? value as PurchaseLimitMode : 'calendar'
}

/** Legacy product responses use the original one-natural-day purchase limit. */
export function getPurchaseLimitPolicy(value: PurchaseLimitFields): PurchaseLimitPolicy {
  const unit = normalizedUnit(value.purchase_limit_unit)
  const mode = normalizedMode(value.purchase_limit_mode)
  const requestedWindowSize = normalizeCount(value.purchase_limit_window_size)

  return {
    unit,
    mode,
    windowSize: mode === 'rolling' ? (requestedWindowSize || 1) : 1,
  }
}

function buildLimitItem(
  scope: PurchaseLimitScope,
  limitValue: number | undefined,
  remainingValue: number | undefined,
  policy: PurchaseLimitPolicy,
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
    ...policy,
  }
}

export function getPurchaseLimitItems(value: PurchaseLimitFields): PurchaseLimitItem[] {
  const policy = getPurchaseLimitPolicy(value)
  return [
    buildLimitItem('periodic', value.daily_purchase_limit, value.daily_purchase_remaining, policy),
    buildLimitItem('total', value.total_purchase_limit, value.total_purchase_remaining, policy),
  ].filter((item): item is PurchaseLimitItem => item !== null)
}

export function getPurchaseLimitLabelKey(item: Pick<PurchaseLimitItem, 'scope' | 'mode' | 'unit'>): PurchaseLimitLabelKey {
  if (item.scope === 'total') return 'total'

  const suffix = item.unit.charAt(0).toUpperCase() + item.unit.slice(1) as 'Day' | 'Week' | 'Month'
  return `${item.mode}${suffix}` as PurchaseLimitLabelKey
}

export function isPurchaseLimitExhausted(value: PurchaseLimitFields): boolean {
  return getPurchaseLimitItems(value).some((item) => item.exhausted)
}
