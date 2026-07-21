import { describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import OpsSettingsDialog from '../OpsSettingsDialog.vue'

const mockGetAlertRuntimeSettings = vi.fn()
const mockGetEmailNotificationConfig = vi.fn()
const mockGetAdvancedSettings = vi.fn()
const mockGetMetricThresholds = vi.fn()

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getAlertRuntimeSettings: (...args: any[]) => mockGetAlertRuntimeSettings(...args),
    getEmailNotificationConfig: (...args: any[]) => mockGetEmailNotificationConfig(...args),
    getAdvancedSettings: (...args: any[]) => mockGetAdvancedSettings(...args),
    getMetricThresholds: (...args: any[]) => mockGetMetricThresholds(...args),
    updateAlertRuntimeSettings: vi.fn(),
    updateEmailNotificationConfig: vi.fn(),
    updateAdvancedSettings: vi.fn(),
    updateMetricThresholds: vi.fn(),
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  template: '<div><slot /><slot name="footer" /></div>',
})

const ToggleStub = defineComponent({
  name: 'Toggle',
  props: { modelValue: { type: Boolean, required: true } },
  emits: ['update:modelValue'],
  template: '<button type="button" class="toggle-stub" @click="$emit(\'update:modelValue\', !modelValue)" />',
})

const SelectStub = defineComponent({
  name: 'SelectControlStub',
  props: { modelValue: { type: [String, Number], default: '' } },
  emits: ['update:modelValue'],
  template: '<div class="select-stub" />',
})

describe('OpsSettingsDialog', () => {
  it('提供独立数据清理入口，同时保留高级设置中的原入口并共享配置状态', async () => {
    mockGetAlertRuntimeSettings.mockResolvedValue({
      evaluation_interval_seconds: 60,
      distributed_lock: { enabled: false, key: '', ttl_seconds: 60 },
      silencing: { enabled: false, global_until_rfc3339: '', global_reason: '' },
      thresholds: {},
    })
    mockGetEmailNotificationConfig.mockResolvedValue({
      alert: {
        enabled: false,
        recipients: [],
        min_severity: '',
        rate_limit_per_hour: 0,
        batching_window_seconds: 0,
        include_resolved_alerts: false,
      },
      report: {
        enabled: false,
        recipients: [],
        daily_summary_enabled: false,
        daily_summary_schedule: '',
        weekly_summary_enabled: false,
        weekly_summary_schedule: '',
        error_digest_enabled: false,
        error_digest_schedule: '',
        error_digest_min_count: 0,
        account_health_enabled: false,
        account_health_schedule: '',
        account_health_error_rate_threshold: 0,
      },
    })
    mockGetAdvancedSettings.mockResolvedValue({
      data_retention: {
        cleanup_enabled: true,
        cleanup_schedule: '0 2 * * *',
        error_log_retention_days: 30,
        minute_metrics_retention_days: 30,
        hourly_metrics_retention_days: 30,
      },
      aggregation: { aggregation_enabled: false },
      openai_account_quota_auto_pause: { default_threshold_5h: 0, default_threshold_7d: 0 },
      ignore_count_tokens_errors: false,
      ignore_context_canceled: false,
      ignore_no_available_accounts: false,
      ignore_invalid_api_key_errors: false,
      ignore_insufficient_balance_errors: false,
      display_openai_token_stats: false,
      display_alert_events: true,
      auto_refresh_enabled: false,
      auto_refresh_interval_seconds: 30,
    })
    mockGetMetricThresholds.mockResolvedValue({})

    const wrapper = mount(OpsSettingsDialog, {
      props: { show: false },
      global: {
        stubs: {
          BaseDialog: BaseDialogStub,
          Toggle: ToggleStub,
          Select: SelectStub,
        },
      },
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    const cleanupSection = wrapper.get('[data-testid="ops-data-cleanup-section"]')
    const advancedSection = wrapper.get('[data-testid="ops-advanced-settings"]')

    expect(cleanupSection.get('summary').text()).toBe('admin.ops.settings.dataCleanup')
    expect(advancedSection.get('summary').text()).toBe('admin.ops.settings.advancedSettings')
    expect(cleanupSection.findAll('input')).toHaveLength(4)

    const cleanupRetentionInput = cleanupSection.findAll('input[type="number"]')[0]
    const originalRetentionInput = advancedSection.findAll('input[type="number"]')[0]
    await cleanupRetentionInput.setValue('14')

    expect((originalRetentionInput.element as HTMLInputElement).value).toBe('14')
  })
})
