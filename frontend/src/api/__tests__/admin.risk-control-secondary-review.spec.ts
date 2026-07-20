import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, post } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, post },
}))

import {
  getSecondaryReviewConfig,
  getSecondaryReviewStatus,
  testSecondaryReview,
  updateSecondaryReviewConfig,
  type SecondaryReviewConfig,
} from '@/api/admin/riskControl'

const config: SecondaryReviewConfig = {
  mode: 'shadow',
  endpoint: 'http://intent-classifier:8080',
  token_configured: true,
  token_masked: 'tok...1234',
  expected_model_version: 'cyber-intent-v20260720.1',
  timeout_ms: 300,
  review_threshold: 0.6,
  block_threshold: 0.9,
  on_error: 'keyword_block',
}

describe('admin secondary review api', () => {
  beforeEach(() => {
    get.mockReset()
    put.mockReset()
    post.mockReset()
  })

  it('loads the write-only token status from the dedicated config endpoint', async () => {
    get.mockResolvedValue({ data: config })

    const result = await getSecondaryReviewConfig()

    expect(get).toHaveBeenCalledWith('/admin/risk-control/secondary-review/config')
    expect(result).toEqual(config)
    expect(result).not.toHaveProperty('token')
  })

  it('updates the complete policy and uses clear_token as an explicit operation', async () => {
    put.mockResolvedValue({ data: { ...config, token_configured: false, token_masked: '' } })
    const payload = {
      mode: 'enforce' as const,
      endpoint: config.endpoint,
      expected_model_version: config.expected_model_version,
      timeout_ms: 500,
      review_threshold: 0.7,
      block_threshold: 0.95,
      on_error: 'allow_and_log' as const,
      clear_token: true,
    }

    await updateSecondaryReviewConfig(payload)

    expect(put).toHaveBeenCalledWith('/admin/risk-control/secondary-review/config', payload)
  })

  it('loads classifier health without exposing an admin endpoint or token', async () => {
    const status = {
      live: true,
      ready: true,
      code: 'ready' as const,
      active_model_version: 'cyber-intent-v20260720.1',
      preprocessing_version: 'text-v3',
      latency_ms: 12,
    }
    get.mockResolvedValue({ data: status })

    const result = await getSecondaryReviewStatus()

    expect(get).toHaveBeenCalledWith('/admin/risk-control/secondary-review/status')
    expect(result).toEqual(status)
    expect(result).not.toHaveProperty('token')
    expect(result).not.toHaveProperty('text')
  })

  it('tests saved configuration with only text and the matched keyword', async () => {
    const response = {
      label: 'actionable_probe',
      score: 0.9621,
      model_version: config.expected_model_version,
      trace_id: 'trace-1',
      latency_ms: 24,
      would_review: true,
      would_block: true,
    }
    post.mockResolvedValue({ data: response })

    const result = await testSecondaryReview({ text: '探测目标网段', matched_keyword: '探测' })

    expect(post).toHaveBeenCalledWith('/admin/risk-control/secondary-review/test', {
      text: '探测目标网段',
      matched_keyword: '探测',
    })
    expect(result).toEqual(response)
  })
})
