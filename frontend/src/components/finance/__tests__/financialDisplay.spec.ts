import { describe, expect, it } from 'vitest'

import { bankLedgerLabel, bankOperationLabel } from '../bankOperation'
import { financialAmountKey, formatFinancialAmount, sumFinancialAmounts, type FinanceTranslate } from '../financialDisplay'

const t: FinanceTranslate = (key, params) => params?.operation === undefined
  ? key
  : `${key}:${params.operation}`

describe('financial display helpers', () => {
  it('formats fiat currencies and internal credits without assuming USD', () => {
    expect(formatFinancialAmount('12.5', 'CNY', 'fiat', t)).toBe('¥12.50')
    expect(formatFinancialAmount('12.5', 'USD', 'fiat', t)).toBe('$12.50')
    expect(formatFinancialAmount('12.5', 'CHF', 'fiat', t)).toBe('CHF 12.50')
    expect(formatFinancialAmount('12.5', '', 'credit', t)).toBe('12.50 finance.units.credit')
  })

  it('keeps grouping keys unit-aware and sums decimal strings exactly', () => {
    expect(financialAmountKey({ currency: 'cny', unit: 'fiat' })).toBe('fiat:CNY')
    expect(financialAmountKey({ currency: '', unit: 'credit' })).toBe('credit:')
    expect(sumFinancialAmounts(['9007199254740993.00000001', '0.00000009'])).toBe('9007199254740993.00000010')
  })

  it('localizes known bank operations and safely preserves unknown operation names', () => {
    expect(bankOperationLabel('exchange', t)).toBe('bank.operations.exchange')
    expect(bankOperationLabel('future_operation', t)).toBe('future_operation')
    expect(bankLedgerLabel('exchange', 'bank', '2.00', t)).toBe('finance.bankCost:bank.operations.exchange')
    expect(bankLedgerLabel('permanent_settlement', 'settlement', '2.00', t)).toBe('finance.bankActivity:bank.operations.permanentSettlement')
  })
})
