import { beforeEach, describe, expect, it, vi } from 'vitest'
import { emptyEventFilters, promptCleanupFilter } from '../viewModel'
import { dataCleanupAPI } from '@/api/admin/dataCleanup'
import type { DataCleanupPreview } from '@/api/admin/ops'

const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), delete: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))

import promptAuditAPI from '../api'

describe('Prompt Audit API', () => {
  beforeEach(() => Object.values(client).forEach((mock) => mock.mockReset()))

  it('uses the independent admin route namespace', async () => {
    client.get.mockResolvedValue({ data: { config_version: 1 } })
    await promptAuditAPI.getConfig()
    expect(client.get).toHaveBeenCalledWith('/admin/prompt-audit/config')

    client.get.mockResolvedValue({ data: { process_status: 'running' } })
    await promptAuditAPI.getRuntime()
    expect(client.get).toHaveBeenCalledWith('/admin/prompt-audit/runtime')
  })

  it('sends a temporary probe token only in the request and never invents response credentials', async () => {
    client.post.mockResolvedValue({ data: { ok: true, token_applied: true } })
    const result = await promptAuditAPI.probeEndpoint({
      id: 'guard-1', name: 'Guard', protocol: 'openai_compatible', base_url: 'http://127.0.0.1:8000', model: 'guard',
      token: 'api-canary-secret', clear_token: false, timeout_ms: 1000, input_limit: 1000, enabled: true, has_token: false, token_status: 'missing',
    })
    expect(client.post).toHaveBeenCalledWith('/admin/prompt-audit/endpoints/probe', expect.objectContaining({ endpoint: expect.objectContaining({ token: 'api-canary-secret' }) }))
    expect(JSON.stringify(result)).not.toContain('api-canary-secret')
  })

  it('previews prompt filters centrally and passes the server token, count, confirmation and TOTP to execution', async () => {
    const filters = emptyEventFilters()
    filters.start_at = '2026-07-15T00:00'
    filters.end_at = '2026-07-16T00:00'
    filters.decision = 'critical'
    filters.api_key_id = '2'
    const filter = promptCleanupFilter(filters)
    expect(filter).toMatchObject({
      category: 'prompt_audit', mode: 'range',
      start_time: new Date(filters.start_at).toISOString(), end_time: new Date(filters.end_at).toISOString(),
      prompt_filter: { decision: 'critical', api_key_id: 2 },
    })
    const preview: DataCleanupPreview = {
      category: 'prompt_audit', mode: 'range', matched_rows: 2, blocked_rows: 0,
      confirmation: 'DELETE prompt_audit 2', requires_totp: true, max_range_days: 31, preview_token: 'opaque-token',
    }
    client.post.mockResolvedValueOnce({ data: preview })
    const result = await dataCleanupAPI.preview(filter)
    expect(result).toEqual(preview)
    expect(client.post).toHaveBeenNthCalledWith(1, '/admin/data-cleanup/preview', filter, { signal: undefined })
    const request = {
      ...filter, preview_rows: result.matched_rows, preview_token: result.preview_token,
      confirmation: result.confirmation, totp_code: '123456',
    }
    client.post.mockResolvedValueOnce({ data: { audit_id: 1, status: 'pending', deleted_rows: 0, task_id: 1 } })
    expect(await dataCleanupAPI.execute(request)).toMatchObject({ status: 'pending', task_id: 1 })
    expect(client.post).toHaveBeenNthCalledWith(2, '/admin/data-cleanup/execute', request)
    expect(promptAuditAPI).not.toHaveProperty('deleteEventsByFilter')
  })
})
