import type { FinanceTranslate } from './financialDisplay'

const bankOperationKeys: Record<string, string> = {
  advance: 'bank.operations.advance',
  exchange: 'bank.operations.exchange',
  debt_offset: 'bank.operations.debtOffset',
  permanent_settlement: 'bank.operations.permanentSettlement',
  unused_advance_repayment: 'bank.operations.unusedAdvanceRepayment',
  early_repay_temporary: 'bank.operations.earlyRepayTemporary',
  early_repay_permanent: 'bank.operations.earlyRepayPermanent',
}

export function bankOperationLabel(operation: string | null | undefined, t: FinanceTranslate): string {
  const normalized = String(operation || '').trim()
  const key = bankOperationKeys[normalized]
  if (key) return t(key)
  return normalized || t('finance.unknownOperation')
}

export function bankLedgerLabel(
  operation: string | null | undefined,
  category: string,
  costAmount: string | number,
  t: FinanceTranslate,
): string {
  const operationLabel = bankOperationLabel(operation, t)
  const isCost = category !== 'settlement' && Number(costAmount) > 0
  return t(isCost ? 'finance.bankCost' : 'finance.bankActivity', { operation: operationLabel })
}
