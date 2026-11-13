import { apiClient } from '../client'
import type { DataCleanupConfig, DataCleanupFilter, DataCleanupPreview, DataCleanupExecuteRequest, DataCleanupExecuteResult, DataCleanupAudit } from './ops'

export interface CleanupStorage {
  table: string
  estimated_rows: number
  table_bytes: number
  index_bytes: number
  total_bytes: number
  protected: boolean
}

export interface CleanupOverview {
  measured_at: string
  timezone: string
  database_bytes: number
  tables: CleanupStorage[]
  months: Array<{ month: string; users: number; requests: number; tokens: number; actual_cost: string }>
  years: Array<{ month: string; requests: number; tokens: number; actual_cost: string }>
  last_cleanup_at?: string
  last_cleanup_result: string
  requests: number
  tokens: number
  actual_cost: string
}

const base = '/admin/data-cleanup'
export const dataCleanupAPI = {
  async config(signal?: AbortSignal) { return (await apiClient.get<DataCleanupConfig>(`${base}/config`, { signal })).data },
  async save(config: DataCleanupConfig) { return (await apiClient.put<DataCleanupConfig>(`${base}/config`, config)).data },
  async overview(signal?: AbortSignal) { return (await apiClient.get<CleanupOverview>(`${base}/overview`, { signal })).data },
  async preview(filter: DataCleanupFilter, signal?: AbortSignal) { return (await apiClient.post<DataCleanupPreview>(`${base}/preview`, filter, { signal })).data },
  async execute(request: DataCleanupExecuteRequest) { return (await apiClient.post<DataCleanupExecuteResult>(`${base}/execute`, request)).data },
  async audits(signal?: AbortSignal) { return (await apiClient.get<DataCleanupAudit[]>(`${base}/audits`, { signal })).data },
  async cancel(id: number) { return (await apiClient.post(`${base}/tasks/${id}/cancel`)).data }
}
