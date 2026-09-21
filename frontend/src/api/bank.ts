import { apiClient } from './client'
import type { FinancialAmountUnit, FixedPageResponse, BankTransactionItem } from '@/types/finance'

/**
 * Bank amounts are deliberately represented as decimal strings. The backend
 * ledger contract is fixed to eight fractional digits and converting these
 * values to JavaScript numbers at the API boundary would lose precision.
 */
export type BankAmount = string

export interface BankExchangeTier {
  up_to: BankAmount | null
  rate: BankAmount
}

export interface BankExchangeProgress {
  date: string
  permanent_exchanged_today: BankAmount
  current_tier_index: number
  current_tier_rate: BankAmount
  current_tier_up_to?: BankAmount | null
  next_tier_rate?: BankAmount | null
  amount_until_next_tier?: BankAmount | null
}

export interface BankPolicy {
  advance_min_amount: BankAmount
  advance_max_amount: BankAmount
  debt_grace_days: number
  debt_conversion_ratio: BankAmount
  exchange_rate: BankAmount
  unused_advance_debt_reduction_ratio: BankAmount
  early_repay_temporary_ratio: BankAmount
  early_repay_permanent_ratio: BankAmount
  exchange_tiers?: BankExchangeTier[]
}

export interface BankAdvance {
  id: number
  principal: BankAmount
  debt_remaining: BankAmount
  status: string
  granted_at: string
  grant_expires_at: string
  settlement_due_at: string
}

export interface BankLedgerItem {
  id: number
  row_id: string
  source: string
  currency: string
  unit: FinancialAmountUnit
  operation: string
  loan_id?: number | null
  grant_id?: number | null
  permanent_delta: BankAmount
  temporary_delta: BankAmount
  debt_delta: BankAmount
  debt_before: BankAmount
  debt_after: BankAmount
  created_at: string
}

export interface BankLedgerPage {
  items: BankLedgerItem[]
  total: number
  page: number
  page_size: 5
  pages: number
}

export interface BankStatus {
  permanent_balance: BankAmount
  temporary_credit_available: BankAmount
  temporary_credit_earliest_expires_at: string | null
  temporary_debt: BankAmount
  temporary_debt_due_at: string | null
  active_advance: BankAdvance | null
  policy: BankPolicy
  ledger: BankLedgerItem[]
  exchange_progress?: BankExchangeProgress | null
}

export interface BankAdvanceResult {
  advance_id: number
  temporary_credit_grant_id: number
  amount: BankAmount
  temporary_debt: BankAmount
  expires_at: string
  settlement_due_at: string
}

export interface BankExchangeResult {
  permanent_spent: BankAmount
  temporary_granted: BankAmount
  temporary_available: BankAmount
  permanent_balance: BankAmount
  temporary_debt: BankAmount
  expires_at: string
  daily_permanent_exchanged?: BankAmount
  exchange_progress?: BankExchangeProgress | null
  tier_allocations?: Array<{ tier_index: number; permanent_amount: BankAmount; rate: BankAmount; temporary_amount: BankAmount }>
}

export type BankRepaySource = 'temporary' | 'permanent'

export interface BankRepayResult {
  source: BankRepaySource
  credit_spent: BankAmount
  debt_reduced: BankAmount
  temporary_debt: BankAmount
  temporary_credit_available: BankAmount
  permanent_balance: BankAmount
}

export async function getBankStatus(): Promise<BankStatus> {
  const response = await apiClient.get<BankStatus>('/bank/status')
  return response.data
}

/** Load one fixed-size page of the authenticated user's bank ledger. */
export async function getBankLedger(page = 1): Promise<BankLedgerPage> {
  const response = await apiClient.get<BankLedgerPage>('/bank/ledger', {
    params: { page },
  })
  return response.data
}

export async function requestBankAdvance(
  amount: BankAmount,
  idempotencyKey: string,
): Promise<BankAdvanceResult> {
  const response = await apiClient.post<BankAdvanceResult>(
    '/bank/advance',
    { amount },
    { headers: { 'Idempotency-Key': idempotencyKey } },
  )
  return response.data
}

export async function exchangePermanentForTemporary(
  permanentAmount: BankAmount,
  idempotencyKey: string,
): Promise<BankExchangeResult> {
  const response = await apiClient.post<BankExchangeResult>(
    '/bank/exchange',
    { amount: permanentAmount },
    { headers: { 'Idempotency-Key': idempotencyKey } },
  )
  return response.data
}

export async function repayBankDebt(
  source: BankRepaySource,
  amount: BankAmount,
  idempotencyKey: string,
): Promise<BankRepayResult> {
  const response = await apiClient.post<BankRepayResult>(
    '/bank/repay',
    { source, amount },
    { headers: { 'Idempotency-Key': idempotencyKey } },
  )
  return response.data
}

export async function getBankSettings(): Promise<BankPolicy> {
  const response = await apiClient.get<BankPolicy>('/admin/settings/bank')
  return response.data
}

export async function updateBankSettings(
  policy: BankPolicy,
  idempotencyKey: string,
): Promise<BankPolicy> {
  const response = await apiClient.put<BankPolicy>(
    '/admin/settings/bank',
    policy,
    { headers: { 'Idempotency-Key': idempotencyKey } },
  )
  return response.data
}

/** Load admin bank transactions with an explicit transaction amount and balance snapshots. */
export async function getBankTransactions(params?: { page?: number; user_id?: number }): Promise<FixedPageResponse<BankTransactionItem>> {
  const response = await apiClient.get<FixedPageResponse<BankTransactionItem>>(
    '/admin/settings/bank/transactions',
    { params: { page: params?.page ?? 1, page_size: 20, ...params } },
  )
  return response.data
}

export const bankAPI = {
  getStatus: getBankStatus,
  getLedger: getBankLedger,
  advance: requestBankAdvance,
  exchange: exchangePermanentForTemporary,
  repay: repayBankDebt,
  getSettings: getBankSettings,
  updateSettings: updateBankSettings,
  getTransactions: getBankTransactions,
}
