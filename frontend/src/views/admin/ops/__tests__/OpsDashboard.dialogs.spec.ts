import { afterEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, onMounted, onUnmounted, watch } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import OpsDashboard from '../OpsDashboard.vue'

const mockRouterReplace = vi.fn()
const mockFetchAdminSettings = vi.fn().mockResolvedValue(undefined)

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ replace: mockRouterReplace }),
}))

vi.mock('@vueuse/core', () => ({
  useDebounceFn: (fn: (...args: any[]) => unknown) => fn,
  useIntervalFn: () => ({ pause: vi.fn(), resume: vi.fn() }),
}))

vi.mock('@/stores', () => ({
  useAdminSettingsStore: () => ({
    fetch: mockFetchAdminSettings,
    opsMonitoringEnabled: true,
    opsQueryModeDefault: 'auto',
  }),
  useAppStore: () => ({ showError: vi.fn() }),
}))

vi.mock('@/api/admin/ops', () => {
  const opsAPI = {
    getAdvancedSettings: vi.fn().mockResolvedValue({
      display_alert_events: false,
      display_openai_token_stats: false,
      auto_refresh_enabled: false,
      auto_refresh_interval_seconds: 30,
    }),
    getMetricThresholds: vi.fn().mockResolvedValue({}),
    getDashboardSnapshotV2: vi.fn().mockResolvedValue({
      overview: null,
      throughput_trend: null,
      error_trend: null,
    }),
    getThroughputTrend: vi.fn().mockResolvedValue(null),
    getLatencyHistogram: vi.fn().mockResolvedValue(null),
    getErrorDistribution: vi.fn().mockResolvedValue(null),
    getDashboardOverview: vi.fn().mockResolvedValue(null),
    getErrorTrend: vi.fn().mockResolvedValue(null),
  }
  return { opsAPI, default: opsAPI }
})

const HeaderStub = defineComponent({
  name: 'OpsDashboardHeader',
  emits: ['open-settings'],
  template: '<button data-testid="open-settings" type="button" @click="$emit(\'open-settings\')">settings</button>',
})

function createDialogStub(name: string, body: string, emits: string[]) {
  return defineComponent({
    name,
    props: { show: { type: Boolean, required: true } },
    emits,
    setup(props, { emit }) {
      const syncBodyLock = (show: boolean) => {
        document.body.classList.toggle('modal-open', show)
      }
      const handleEscape = (event: KeyboardEvent) => {
        if (props.show && event.key === 'Escape') emit('close')
      }
      watch(() => props.show, syncBodyLock, { immediate: true })
      onMounted(() => document.addEventListener('keydown', handleEscape))
      onUnmounted(() => {
        document.removeEventListener('keydown', handleEscape)
        if (props.show) document.body.classList.remove('modal-open')
      })
      return {}
    },
    template: body,
  })
}

const SettingsDialogStub = createDialogStub(
  'OpsSettingsDialog',
  '<div v-if="show" data-testid="settings-dialog" data-modal-visible="true"><button data-testid="open-cleanup" type="button" @click="$emit(\'openCleanup\')">cleanup</button></div>',
  ['close', 'saved', 'openCleanup'],
)

const CleanupDialogStub = createDialogStub(
  'OpsDataCleanupDialog',
  '<div v-if="show" data-testid="cleanup-dialog" data-modal-visible="true"><button data-testid="back-settings" type="button" @click="$emit(\'back\')">back</button></div>',
  ['close', 'back', 'saved'],
)

afterEach(() => {
  document.body.classList.remove('modal-open')
  vi.clearAllMocks()
})

describe('OpsDashboard dialog state', () => {
  it('设置与清理弹窗互斥，返回恢复设置，Escape 只关闭当前顶层', async () => {
    const wrapper = mount(OpsDashboard, {
      global: {
        stubs: {
          AppLayout: { template: '<main><slot /></main>' },
          OpsDashboardHeader: HeaderStub,
          OpsDashboardSkeleton: true,
          OpsConcurrencyCard: true,
          OpsSwitchRateTrendChart: true,
          OpsThroughputTrendChart: true,
          OpsLatencyChart: true,
          OpsErrorDistributionChart: true,
          OpsErrorTrendChart: true,
          OpsOpenAITokenStatsCard: true,
          OpsAlertEventsCard: true,
          OpsSystemLogTable: true,
          OpsSettingsDialog: SettingsDialogStub,
          OpsDataCleanupDialog: CleanupDialogStub,
          BaseDialog: true,
          OpsAlertRulesCard: true,
          OpsErrorDetailsModal: true,
          OpsErrorDetailModal: true,
          OpsRequestDetailsModal: true,
        },
      },
    })
    await flushPromises()

    await wrapper.get('[data-testid="open-settings"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="settings-dialog"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="cleanup-dialog"]').exists()).toBe(false)
    expect(wrapper.findAll('[data-modal-visible="true"]')).toHaveLength(1)
    expect(document.body.classList.contains('modal-open')).toBe(true)

    await wrapper.get('[data-testid="open-cleanup"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="settings-dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="cleanup-dialog"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-modal-visible="true"]')).toHaveLength(1)
    expect(document.body.classList.contains('modal-open')).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(wrapper.find('[data-testid="cleanup-dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="settings-dialog"]').exists()).toBe(false)
    expect(document.body.classList.contains('modal-open')).toBe(false)

    await wrapper.get('[data-testid="open-settings"]').trigger('click')
    await wrapper.get('[data-testid="open-cleanup"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="back-settings"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-testid="cleanup-dialog"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="settings-dialog"]').exists()).toBe(true)
    expect(wrapper.findAll('[data-modal-visible="true"]')).toHaveLength(1)
    expect(document.body.classList.contains('modal-open')).toBe(true)

    wrapper.unmount()
  })
})
