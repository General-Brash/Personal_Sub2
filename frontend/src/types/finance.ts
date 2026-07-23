export type LedgerAmount = string | number

export type FinancialAmountUnit = 'credit' | 'fiat' | string

export interface FinancialAmountDescriptor {
  currency: string
  unit: FinancialAmountUnit
}

export interface LedgerAmountTotal extends FinancialAmountDescriptor {
  amount: LedgerAmount
  count: number
}

export type LedgerWindowKey = 'today' | 'seven_days' | 'fifteen_days'

export interface LedgerWindowSummary {
  total_amount: LedgerAmount
  count: number
  categories?: LedgerCategorySummary[]
  totals: LedgerAmountTotal[]
}

export interface LedgerCategorySummary extends FinancialAmountDescriptor {
  category: string
  label: string
  amount: LedgerAmount
  count: number
}

export interface LedgerItem {
  id: number | string
  row_id: string
  user_id?: number
  username?: string
  email?: string
  source: string
  category: string
  label: string
  amount: LedgerAmount
  cost_amount: LedgerAmount
  currency: string
  unit: FinancialAmountUnit
  product_type?: string | null
  product_id?: number | null
  operation?: string | null
  model?: string | null
  permanent_delta: LedgerAmount | null
  temporary_delta: LedgerAmount | null
  debt_delta: LedgerAmount | null
  permanent_balance_before: LedgerAmount | null
  permanent_balance_after: LedgerAmount | null
  temporary_balance_before: LedgerAmount | null
  temporary_balance_after: LedgerAmount | null
  debt_before: LedgerAmount | null
  debt_after: LedgerAmount | null
  count?: number
  created_at: string
}

export interface LedgerResponse {
  user_id?: number
  username?: string
  email?: string
  timezone: string
  windows: Record<LedgerWindowKey, LedgerWindowSummary>
  summary: LedgerCategorySummary[]
  items: LedgerItem[]
  total: number
  page: number
  page_size: number
  pages: number
  days?: number
}

export interface MallTransactionItem {
  id: number
  row_id: string
  source: string
  user_id: number
  username: string
  email?: string
  product_type: 'currency' | 'subscription' | string
  product_id: number
  product_name: string
  payment_credit_type: 'permanent' | 'temporary' | string
  price: LedgerAmount
  currency: string
  unit: FinancialAmountUnit
  permanent_credited_amount: LedgerAmount
  temporary_credited_amount: LedgerAmount
  permanent_balance_before: LedgerAmount | null
  permanent_balance_after: LedgerAmount | null
  temporary_balance_before: LedgerAmount | null
  temporary_balance_after: LedgerAmount | null
  subscription_expires_at?: string | null
  status: string
  created_at: string
}

export interface BankTransactionItem {
  id: number
  row_id: string
  source: string
  user_id: number
  username: string
  email?: string
  operation: string
  transaction_amount: LedgerAmount
  currency: string
  unit: FinancialAmountUnit
  permanent_delta: LedgerAmount
  temporary_delta: LedgerAmount
  debt_delta: LedgerAmount
  permanent_balance_before: LedgerAmount | null
  permanent_balance_after: LedgerAmount | null
  temporary_balance_before: LedgerAmount | null
  temporary_balance_after: LedgerAmount | null
  debt_before: LedgerAmount | null
  debt_after: LedgerAmount | null
  metadata?: Record<string, unknown> | null
  created_at: string
}

export interface FixedPageResponse<T> {
  items: T[]
  total: number
  page: number
  page_size: number
  pages: number
}

export interface MallAnalyticsProduct extends FinancialAmountDescriptor {
  product_type: string
  product_id: number
  product_name: string
  sales_count: number
  revenue: LedgerAmount
}

export interface MallAnalyticsDailyPoint extends FinancialAmountDescriptor {
  date: string
  sales_count: number
  revenue: LedgerAmount
}

export interface MallAnalyticsRevenueTotal extends FinancialAmountDescriptor {
  revenue: LedgerAmount
  sales_count: number
}

export interface MallAnalyticsResponse {
  days: number
  total_sales: number
  total_revenue?: LedgerAmount
  revenue_totals: MallAnalyticsRevenueTotal[]
  products: MallAnalyticsProduct[]
  daily: MallAnalyticsDailyPoint[]
  currency_sales?: number
  subscription_sales?: number
  currency_revenue?: LedgerAmount
  subscription_revenue?: LedgerAmount
}
