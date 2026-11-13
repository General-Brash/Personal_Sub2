export interface UsageHistoryMetadata {
  includes_history?: boolean
  approximate?: boolean
  history_granularity?: string
  history_range_policy?: 'whole_intersecting_days'
  history_timezone?: string
  history_start?: string
  history_end?: string
}
