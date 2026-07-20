import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import type { VueWrapper } from '@vue/test-utils'

import SecondaryReviewView from '../SecondaryReviewView.vue'
import type {
  ContentModerationConfig,
  SecondaryReviewConfig,
  SecondaryReviewStatus,
  SecondaryReviewTestResult,
  UpdateSecondaryReviewConfig,
} from '@/api/admin/riskControl'

const {
  getSecondaryReviewConfig,
  getSecondaryReviewStatus,
  updateSecondaryReviewConfig,
  testSecondaryReview,
  getConfig,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getSecondaryReviewConfig: vi.fn(),
  getSecondaryReviewStatus: vi.fn(),
  updateSecondaryReviewConfig: vi.fn(),
  testSecondaryReview: vi.fn(),
  getConfig: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    riskControl: {
      getSecondaryReviewConfig,
      getSecondaryReviewStatus,
      updateSecondaryReviewConfig,
      testSecondaryReview,
      getConfig,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorCode: (error: unknown) => (
    error && typeof error === 'object' && 'reason' in error
      ? String((error as { reason: unknown }).reason)
      : undefined
  ),
  extractApiErrorMessage: (_error: unknown, fallback: string) => fallback,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.secondaryReview.tokenMasked') return `${key}:${params?.token ?? ''}`
        if (key === 'admin.secondaryReview.serviceStatus.reasons.modelVersionMismatch') {
          return `${key}:${params?.active ?? ''}:${params?.expected ?? ''}`
        }
        return key.replace(/\{(\w+)\}/g, (_, token) => String(params?.[token] ?? `{${token}}`))
      },
    }),
  }
})

const secondaryConfig = (): SecondaryReviewConfig => ({
  mode: 'shadow',
  endpoint: 'http://intent-classifier:8080',
  token_configured: true,
  token_masked: 'tok...1234',
  expected_model_version: 'cyber-intent-v20260720.1',
  timeout_ms: 300,
  review_threshold: 0.6,
  block_threshold: 0.9,
  on_error: 'keyword_block',
})

const readyStatus = (): SecondaryReviewStatus => ({
  live: true,
  ready: true,
  code: 'ready',
  active_model_version: 'cyber-intent-v20260720.1',
  preprocessing_version: 'text-v3',
  latency_ms: 12,
})

const moderationConfig = (overrides: Partial<ContentModerationConfig> = {}): ContentModerationConfig => ({
  enabled: true,
  mode: 'pre_block',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  api_key_configured: false,
  api_key_masked: '',
  api_key_count: 0,
  api_key_masks: [],
  api_key_statuses: [],
  timeout_ms: 3000,
  sample_rate: 100,
  all_groups: true,
  group_ids: [],
  record_non_hits: false,
  thresholds: {},
  worker_count: 4,
  queue_size: 32768,
  block_status: 403,
  block_message: 'blocked',
  email_on_hit: false,
  auto_ban_enabled: false,
  ban_threshold: 10,
  violation_window_hours: 720,
  retry_count: 2,
  hit_retention_days: 180,
  non_hit_retention_days: 3,
  pre_hash_check_enabled: false,
  blocked_keywords: ['探测'],
  keyword_blocking_mode: 'keyword_only',
  model_filter: { type: 'all', models: [] },
  cyber_policy_exclude_from_ban_count: false,
  ...overrides,
})

function mountView(): VueWrapper {
  return mount(SecondaryReviewView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        Icon: true,
      },
    },
  })
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

describe('admin SecondaryReviewView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getSecondaryReviewConfig.mockResolvedValue(secondaryConfig())
    getSecondaryReviewStatus.mockResolvedValue(readyStatus())
    getConfig.mockResolvedValue(moderationConfig())
    updateSecondaryReviewConfig.mockImplementation(async (payload: UpdateSecondaryReviewConfig) => ({
      ...secondaryConfig(),
      ...payload,
      token_configured: payload.clear_token ? false : true,
      token_masked: payload.clear_token ? '' : 'tok...5678',
    }))
    testSecondaryReview.mockResolvedValue({
      label: 'actionable_probe',
      score: 0.9621,
      model_version: 'cyber-intent-v20260720.1',
      trace_id: 'trace-1',
      latency_ms: 24,
      would_review: true,
      would_block: true,
    })
  })

  it('loads config without putting the stored token into the password field', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(getSecondaryReviewConfig).toHaveBeenCalledOnce()
    expect(getSecondaryReviewStatus).toHaveBeenCalledOnce()
    expect(getConfig).toHaveBeenCalledOnce()
    expect(wrapper.get<HTMLInputElement>('#secondary-review-endpoint').element.value).toBe('http://intent-classifier:8080')
    expect(wrapper.get<HTMLInputElement>('#secondary-review-token').element.value).toBe('')
    expect(wrapper.get('[data-test="token-status"]').text()).toContain('admin.secondaryReview.tokenConfigured')
    expect(wrapper.text()).toContain('tok...1234')
  })

  it('shows live, ready, version, preprocessing, and latency status', async () => {
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="status-summary"]').text()).toContain('admin.secondaryReview.serviceStatus.labels.ready')
    expect(wrapper.get('[data-test="status-model-version"]').text()).toContain('cyber-intent-v20260720.1')
    expect(wrapper.get('[data-test="status-preprocessing-version"]').text()).toContain('text-v3')
    expect(wrapper.get('[data-test="status-latency"]').text()).toContain('12 ms')
    expect(wrapper.text()).not.toContain('new-secret-token')
  })

  it('shows model_not_ready without treating the live service as offline', async () => {
    getSecondaryReviewStatus.mockResolvedValueOnce({
      live: true,
      ready: false,
      code: 'model_not_ready',
      active_model_version: null,
      preprocessing_version: null,
      latency_ms: 9,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="status-summary"]').text()).toContain('admin.secondaryReview.serviceStatus.labels.notReady')
    expect(wrapper.get('[data-test="status-reason"]').text()).toContain('admin.secondaryReview.serviceStatus.reasons.modelNotReady')
    expect(wrapper.text()).toContain('admin.secondaryReview.serviceStatus.values.live')
    expect(wrapper.get('[data-test="status-model-version"]').text()).toContain('admin.secondaryReview.serviceStatus.emptyValue')
  })

  it('warns when the active model does not match the configured version', async () => {
    getSecondaryReviewStatus.mockResolvedValueOnce({
      live: true,
      ready: false,
      code: 'model_version_mismatch',
      active_model_version: 'cyber-intent-v20260720.2',
      preprocessing_version: 'text-v4',
      latency_ms: 11,
    })

    const wrapper = mountView()
    await flushPromises()

    const summary = wrapper.get('[data-test="status-summary"]')
    expect(summary.text()).toContain('admin.secondaryReview.serviceStatus.labels.versionMismatch')
    expect(summary.text()).toContain('cyber-intent-v20260720.2')
    expect(summary.text()).toContain('cyber-intent-v20260720.1')
    expect(summary.classes()).toContain('border-amber-200')
    expect(wrapper.get('[data-test="status-model-version"]').text()).toContain('cyber-intent-v20260720.2')
    expect(wrapper.get('[data-test="status-preprocessing-version"]').text()).toContain('text-v4')
  })

  it('refreshes status manually and never renders an upstream error body', async () => {
    getSecondaryReviewStatus.mockRejectedValueOnce({ message: 'SENSITIVE_CLASSIFIER_RESPONSE_BODY' })
    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get('[data-test="status-error"]').text()).toContain('admin.secondaryReview.errors.statusLoad')
    expect(wrapper.text()).not.toContain('SENSITIVE_CLASSIFIER_RESPONSE_BODY')

    getSecondaryReviewStatus.mockResolvedValueOnce(readyStatus())
    await wrapper.get('[data-test="status-retry"]').trigger('click')
    await flushPromises()

    expect(getSecondaryReviewStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="status-model-version"]').text()).toContain('cyber-intent-v20260720.1')
  })

  it('disables manual status refresh until a service endpoint is configured', async () => {
    getSecondaryReviewConfig.mockResolvedValueOnce({ ...secondaryConfig(), mode: 'off', endpoint: '' })
    getSecondaryReviewStatus.mockResolvedValueOnce({
      live: false,
      ready: false,
      code: 'not_configured',
      active_model_version: null,
      preprocessing_version: null,
      latency_ms: 0,
    })

    const wrapper = mountView()
    await flushPromises()

    expect(wrapper.get<HTMLButtonElement>('[data-test="status-refresh"]').element.disabled).toBe(true)
    expect(wrapper.get('[data-test="status-summary"]').text()).toContain('admin.secondaryReview.serviceStatus.labels.notConfigured')
  })

  it('submits a new token while omitting clear_token', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-token').setValue('new-secret-token')
    await wrapper.get('[data-test="save"]').trigger('click')
    await flushPromises()

    expect(updateSecondaryReviewConfig).toHaveBeenCalledWith(expect.objectContaining({
      mode: 'shadow',
      token: 'new-secret-token',
      endpoint: 'http://intent-classifier:8080',
      review_threshold: 0.6,
      block_threshold: 0.9,
    }))
    expect(updateSecondaryReviewConfig.mock.calls[0]?.[0]).not.toHaveProperty('clear_token')
    expect(showSuccess).toHaveBeenCalledWith('admin.secondaryReview.messages.saved')
  })

  it('uses clear_token explicitly and never sends an empty token as deletion', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="clear-token"]').trigger('click')
    await wrapper.get('[data-test="save"]').trigger('click')
    await flushPromises()

    expect(updateSecondaryReviewConfig).toHaveBeenCalledWith(expect.objectContaining({ clear_token: true }))
    expect(updateSecondaryReviewConfig.mock.calls[0]?.[0]).not.toHaveProperty('token')
  })

  it('blocks incompatible shadow and enforce modes in the client', async () => {
    getConfig.mockResolvedValue(moderationConfig({ blocked_keywords: [] }))
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-timeout').setValue('500')
    expect(wrapper.get('[data-test="compatibility-status"]').text()).toContain('admin.secondaryReview.compatibilityIssues.keywords')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(true)

    await wrapper.get('[data-test="mode-enforce"]').trigger('click')
    await wrapper.get('#secondary-review-review-threshold').setValue('0.95')
    await wrapper.get('#secondary-review-block-threshold').setValue('0.9')
    expect(wrapper.text()).toContain('admin.secondaryReview.validation.thresholdOrder')
    expect(updateSecondaryReviewConfig).not.toHaveBeenCalled()
  })

  it('requires an endpoint in shadow and enforce modes but permits an empty endpoint when off', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-endpoint').setValue('')
    expect(wrapper.text()).toContain('admin.secondaryReview.validation.endpointRequired')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(true)

    await wrapper.get('[data-test="mode-off"]').trigger('click')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(false)
  })

  it('accepts only a service root and leaves the classifier path to the Go adapter', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-endpoint').setValue('http://intent-classifier:8080/v1/classify')

    expect(wrapper.text()).toContain('admin.secondaryReview.validation.endpointInvalid')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(true)
  })

  it('requires an expected model version only in enforce mode', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-model-version').setValue('')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(false)

    await wrapper.get('[data-test="mode-enforce"]').trigger('click')
    expect(wrapper.text()).toContain('admin.secondaryReview.validation.modelVersionRequired')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(true)
  })

  it('enforces the calibrated 0.5 score floor for policy thresholds', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-review-threshold').setValue('0.49')

    expect(wrapper.text()).toContain('admin.secondaryReview.validation.thresholdRange')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(true)

    await wrapper.get('#secondary-review-review-threshold').setValue('0.5')
    expect(wrapper.get<HTMLButtonElement>('[data-test="save"]').element.disabled).toBe(false)
  })

  it('tests the saved configuration and renders the structured result', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-test-text').setValue('请探测目标网段')
    await wrapper.get('#secondary-review-test-keyword').setValue('探测')
    await wrapper.get('[data-test="run-test"]').trigger('click')
    await flushPromises()

    expect(testSecondaryReview).toHaveBeenCalledWith({ text: '请探测目标网段', matched_keyword: '探测' })
    const result = wrapper.get('[data-test="test-result"]').text()
    expect(result).toContain('actionable_probe')
    expect(result).toContain('0.9621')
    expect(result).toContain('cyber-intent-v20260720.1')
    expect(result).toContain('trace-1')
    expect(result).toContain('24 ms')
  })

  it('keeps the connection test disabled and visibly loading while a request is in flight', async () => {
    const pending = deferred<SecondaryReviewTestResult>()
    testSecondaryReview.mockReturnValueOnce(pending.promise)
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="run-test"]').trigger('click')

    const button = wrapper.get<HTMLButtonElement>('[data-test="run-test"]')
    expect(button.element.disabled).toBe(true)
    expect(button.text()).toContain('admin.secondaryReview.testing')

    pending.resolve({
      label: 'benign',
      score: 0.08,
      model_version: 'cyber-intent-v20260720.1',
      trace_id: 'trace-pending',
      latency_ms: 18,
      would_review: false,
      would_block: false,
    })
    await flushPromises()

    expect(button.element.disabled).toBe(false)
    expect(wrapper.get('[data-test="test-result"]').text()).toContain('benign')
  })

  it('keeps the test disabled for unsaved config and shows a failed request inline', async () => {
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('#secondary-review-endpoint').setValue('http://changed:8080')
    expect(wrapper.get<HTMLButtonElement>('[data-test="run-test"]').element.disabled).toBe(true)

    await wrapper.get('[data-test="reset"]').trigger('click')
    testSecondaryReview.mockRejectedValueOnce({
      reason: 'UNKNOWN_SECONDARY_REVIEW_REASON',
      message: 'SENSITIVE_UNKNOWN_UPSTREAM_RESPONSE_BODY',
    })
    await wrapper.get('[data-test="run-test"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.secondaryReview.testFailure')
    expect(wrapper.text()).toContain('admin.secondaryReview.testErrors.unknown')
    expect(wrapper.text()).not.toContain('SENSITIVE_UNKNOWN_UPSTREAM_RESPONSE_BODY')
  })

  it.each([
    'SECONDARY_REVIEW_HTTP_401',
    'SECONDARY_REVIEW_HTTP_403',
    'SECONDARY_REVIEW_UPSTREAM_4XX',
    'SECONDARY_REVIEW_UPSTREAM_5XX',
    'SECONDARY_REVIEW_TIMEOUT',
    'SECONDARY_REVIEW_INVALID_RESPONSE',
    'SECONDARY_REVIEW_BUSY',
    'SECONDARY_REVIEW_UNAVAILABLE',
  ])('maps the safe %s reason without rendering an upstream response body', async (reason) => {
    testSecondaryReview.mockRejectedValueOnce({
      reason,
      message: 'SENSITIVE_UPSTREAM_RESPONSE_BODY',
    })
    const wrapper = mountView()
    await flushPromises()

    await wrapper.get('[data-test="run-test"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain(`admin.secondaryReview.testErrors.${reason}`)
    expect(wrapper.text()).not.toContain('SENSITIVE_UPSTREAM_RESPONSE_BODY')
  })
})
