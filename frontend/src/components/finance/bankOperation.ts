import type { FinanceTranslate } from './financialDisplay'

const bankOperationKeys: Record<string, string> = {
  advance: 'bank.operations.advance',
  exchange: 'bank.operations.exchange',
  auto_exchange_deposit_freeze: 'bank.operations.autoExchangeDepositFreeze',
  auto_exchange_deposit_release: 'bank.operations.autoExchangeDepositRelease',
  debt_offset: 'bank.operations.debtOffset',
  permanent_settlement: 'bank.operations.permanentSettlement',
  unused_advance_repayment: 'bank.operations.unusedAdvanceRepayment',
  early_repay_temporary: 'bank.operations.earlyRepayTemporary',
  early_repay_permanent: 'bank.operations.earlyRepayPermanent',
}

export function bankOperationLabel(
  operation: string | null | undefined,
  t: FinanceTranslate,
  metadata?: Record<string, unknown> | null,
): string {
  const normalized = String(operation || '').trim()
  if (normalized === 'exchange' && (metadata?.auto_exchange === true || metadata?.notes === '\u81ea\u52a8\u5151\u6362')) {
    return t('bank.operations.autoExchange')
  }
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
  const normalizedOperation = String(operation || '').trim()
  const operationLabel = bankOperationLabel(normalizedOperation, t)
  const isDepositActivity = normalizedOperation === 'auto_exchange_deposit_freeze'
    || normalizedOperation === 'auto_exchange_deposit_release'
  const isCost = !isDepositActivity && category !== 'settlement' && Number(costAmount) > 0
  return t(isCost ? 'finance.bankCost' : 'finance.bankActivity', { operation: operationLabel })
}
