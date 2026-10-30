import { apiClient } from '../client'

export type EmptyRetryStrategy = 'same_account_only' | 'same_then_switch' | 'prefer_other_account'
export type RateLimitRetryStrategy = 'same_account_only' | 'prefer_other_account'

export interface ServerAutoRetryConfig {
  schema_version: 1
  enabled: boolean
  scope: {
    platform: 'openai'
    group_ids: number[]
    account_ids: number[]
    models: string[]
  }
  max_additional_attempts: number
  max_total_retry_wait_ms: number
  azure400: { enabled: boolean; max_retries: number }
  empty_response: { enabled: boolean; max_retries: number }
  empty_retry_strategy: EmptyRetryStrategy
  same_account_retries: number
  rate_limit_retry_strategy: RateLimitRetryStrategy
  empty_backoff_base_ms: number
  rate_limit_backoff_base_ms: number
  backoff_cap_ms: number
  jitter_ms: number
  respect_retry_after: boolean
}

// Older or incomplete log projections may omit any evidence field.
export interface ServerAutoRetryEvent {
  id?: number | string | null
  timestamp?: string | null
  request_id?: string | null
  reason?: string | null
  raw_http_status?: number | null
  semantic_status?: number | string | null
  account_id?: number | null
  dispatch_count?: number | null
  retry_count?: number | null
  switch_count?: number | null
  wait_ms?: number | null
  response_committed?: boolean | null
  has_semantic_output?: boolean | null
  usage_evidence?: string | null
  stop_reason?: string | null
  outcome?: string | null
  upstream_request_id?: string | null
}

export interface ServerAutoRetryEventsResponse {
  items: ServerAutoRetryEvent[]
  total: number
  page: number
  page_size: number
}

export function recommendedServerAutoRetryConfig(): ServerAutoRetryConfig {
  return {
    schema_version: 1,
    enabled: true,
    scope: { platform: 'openai', group_ids: [], account_ids: [], models: [] },
    max_additional_attempts: 3,
    max_total_retry_wait_ms: 30000,
    azure400: { enabled: true, max_retries: 1 },
    empty_response: { enabled: true, max_retries: 2 },
    empty_retry_strategy: 'same_then_switch',
    same_account_retries: 1,
    rate_limit_retry_strategy: 'prefer_other_account',
    empty_backoff_base_ms: 500,
    rate_limit_backoff_base_ms: 2000,
    backoff_cap_ms: 10000,
    jitter_ms: 250,
    respect_retry_after: true
  }
}

export async function getConfig(signal?: AbortSignal): Promise<ServerAutoRetryConfig> {
  const { data } = await apiClient.get<ServerAutoRetryConfig>('/admin/server-auto-retry/config', { signal })
  return data
}

export async function updateConfig(config: ServerAutoRetryConfig): Promise<ServerAutoRetryConfig> {
  const { data } = await apiClient.put<ServerAutoRetryConfig>('/admin/server-auto-retry/config', config)
  return data
}

export async function listEvents(
  params: { page: number; page_size: number; request_id?: string },
  signal?: AbortSignal
): Promise<ServerAutoRetryEventsResponse> {
  const { data } = await apiClient.get<ServerAutoRetryEventsResponse>('/admin/server-auto-retry/events', {
    params,
    signal
  })
  return data
}

export const serverAutoRetryAPI = { getConfig, updateConfig, listEvents }
export default serverAutoRetryAPI
