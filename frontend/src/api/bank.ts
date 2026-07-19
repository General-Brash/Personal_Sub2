import { apiClient } from './client'

/**
 * Bank amounts are deliberately represented as decimal strings. The backend
 * ledger contract is fixed to eight fractional digits and converting these
 * values to JavaScript numbers at the API boundary would lose precision.
 */
export type BankAmount = string

export interface BankPolicy {
  advance_min_amount: BankAmount
  advance_max_amount: BankAmount
  debt_grace_days: number
  debt_conversion_ratio: BankAmount
  exchange_rate: BankAmount
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

export interface BankStatus {
  permanent_balance: BankAmount
  temporary_credit_available: BankAmount
  temporary_credit_earliest_expires_at: string | null
  temporary_debt: BankAmount
  temporary_debt_due_at: string | null
  active_advance: BankAdvance | null
  policy: BankPolicy
  ledger: BankLedgerItem[]
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
}

export async function getBankStatus(): Promise<BankStatus> {
  const response = await apiClient.get<BankStatus>('/bank/status')
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

export const bankAPI = {
  getStatus: getBankStatus,
  advance: requestBankAdvance,
  exchange: exchangePermanentForTemporary,
  getSettings: getBankSettings,
  updateSettings: updateBankSettings,
}
