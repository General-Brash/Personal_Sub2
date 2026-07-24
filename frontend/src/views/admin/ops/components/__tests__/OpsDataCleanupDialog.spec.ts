import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, nextTick } from 'vue'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import OpsDataCleanupDialog from '../OpsDataCleanupDialog.vue'

const mockGetConfig = vi.fn()
const mockUpdateConfig = vi.fn()
const mockPreview = vi.fn()
const mockExecute = vi.fn()
const mockListAudits = vi.fn()
const mockShowError = vi.fn()
const mockShowSuccess = vi.fn()

vi.mock('@/api/admin/ops', () => ({
  opsAPI: {
    getDataCleanupConfig: (...args: any[]) => mockGetConfig(...args),
    updateDataCleanupConfig: (...args: any[]) => mockUpdateConfig(...args),
    previewDataCleanup: (...args: any[]) => mockPreview(...args),
    executeDataCleanup: (...args: any[]) => mockExecute(...args),
    listDataCleanupAudits: (...args: any[]) => mockListAudits(...args),
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: mockShowError,
    showSuccess: mockShowSuccess,
  }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, unknown>) => params ? `${key}:${JSON.stringify(params)}` : key,
    }),
  }
})

const ToggleStub = defineComponent({
  name: 'Toggle',
  props: { modelValue: { type: Boolean, required: true } },
  emits: ['update:modelValue'],
  template: '<button type="button" class="toggle-stub" @click="$emit(\'update:modelValue\', !modelValue)" />',
})

const IconStub = defineComponent({
  name: 'Icon',
  props: { name: { type: String, default: '' } },
  template: '<span :data-icon="name" />',
})

const categories = [
  'ops_error_logs',
  'ops_alert_events',
  'ops_system_logs',
  'ops_system_log_cleanup_audits',
  'ops_system_metrics',
  'ops_metrics_hourly',
  'ops_metrics_daily',
  'usage_logs',
]

function makeConfig() {
  return {
    data_retention: {
      cleanup_enabled: true,
      cleanup_schedule: '0 2 * * *',
      targets: Object.fromEntries(categories.map(category => [category, { enabled: true, retention_days: 30 }])),
      error_log_retention_days: 30,
      minute_metrics_retention_days: 30,
      hourly_metrics_retention_days: 30,
    },
    audit_log_retention_days: 180,
  }
}

function makePreview(overrides: Record<string, unknown> = {}) {
  return {
    category: 'ops_error_logs',
    mode: 'range',
    matched_rows: 4,
    blocked_rows: 0,
    confirmation: 'DELETE ops_error_logs 4',
    requires_totp: false,
    max_range_days: 31,
    preview_token: 'signed-preview-token',
    ...overrides,
  }
}

const wrappers: VueWrapper[] = []

async function mountOpened() {
  const wrapper = mount(OpsDataCleanupDialog, {
    props: { show: false },
    global: {
      stubs: {
        Teleport: true,
        Toggle: ToggleStub,
        Icon: IconStub,
      },
    },
  })
  wrappers.push(wrapper)
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

async function setRange(wrapper: VueWrapper, start = '2026-07-01', end = '2026-07-02') {
  const dates = wrapper.findAll('input[type="date"]')
  await dates[0].setValue(start)
  await dates[1].setValue(end)
}

beforeEach(() => {
  mockGetConfig.mockImplementation(async () => makeConfig())
  mockUpdateConfig.mockImplementation(async config => config)
  mockPreview.mockResolvedValue(makePreview())
  mockExecute.mockResolvedValue({ audit_id: 1, status: 'succeeded', deleted_rows: 4 })
  mockListAudits.mockResolvedValue([])
})

afterEach(() => {
  wrappers.splice(0).forEach(wrapper => wrapper.unmount())
  document.body.classList.remove('modal-open')
  vi.useRealTimers()
  vi.clearAllMocks()
})

describe('OpsDataCleanupDialog', () => {
  it('任一筛选条件变化都会使已有预览失效', async () => {
    const mutations = [
      async (wrapper: VueWrapper) => wrapper.findAll('select')[0].setValue('usage_logs'),
      async (wrapper: VueWrapper) => wrapper.findAll('select')[1].setValue('all'),
      async (wrapper: VueWrapper) => wrapper.findAll('input[type="date"]')[0].setValue('2026-06-30'),
      async (wrapper: VueWrapper) => wrapper.findAll('input[type="date"]')[1].setValue('2026-07-03'),
    ]

    for (const mutate of mutations) {
      const wrapper = await mountOpened()
      await setRange(wrapper)
      await wrapper.get('[data-testid="cleanup-preview"]').trigger('click')
      await flushPromises()
      expect(wrapper.text()).toContain('admin.ops.cleanup.previewRows')

      await mutate(wrapper)
      await nextTick()
      expect(wrapper.text()).not.toContain('admin.ops.cleanup.previewRows')
      wrapper.unmount()
      wrappers.splice(wrappers.indexOf(wrapper), 1)
    }
  })

  it('忽略已失效的旧预览响应，并阻止重复预览请求', async () => {
    let resolvePreview!: (value: ReturnType<typeof makePreview>) => void
    mockPreview.mockImplementation(() => new Promise(resolve => { resolvePreview = resolve }))
    const wrapper = await mountOpened()
    await setRange(wrapper)

    const previewButton = wrapper.get('[data-testid="cleanup-preview"]')
    await previewButton.trigger('click')
    await previewButton.trigger('click')
    expect(mockPreview).toHaveBeenCalledTimes(1)

    await wrapper.findAll('input[type="date"]')[1].setValue('2026-07-03')
    resolvePreview(makePreview())
    await flushPromises()

    expect(wrapper.text()).not.toContain('admin.ops.cleanup.previewRows')
  })

  it('显示受保护行和敏感 TOTP，并携带 token 与完整筛选执行', async () => {
    mockPreview.mockResolvedValue(makePreview({
      category: 'bank_ledger',
      matched_rows: 7,
      blocked_rows: 2,
      confirmation: 'DELETE bank_ledger 7',
      requires_totp: true,
      preview_token: 'bank-token',
    }))
    mockExecute.mockResolvedValue({ audit_id: 7, status: 'pending', deleted_rows: 0, task_id: 23 })
    const wrapper = await mountOpened()
    await wrapper.findAll('select')[0].setValue('bank_ledger')
    await setRange(wrapper, '2026-07-01', '2026-07-04')
    await wrapper.get('[data-testid="cleanup-preview"]').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('admin.ops.cleanup.blockedRows')
    expect(wrapper.get('[data-testid="cleanup-totp"]').attributes('type')).toBe('password')
    await wrapper.get('[data-testid="cleanup-confirmation"]').setValue('DELETE bank_ledger 7')
    await wrapper.get('[data-testid="cleanup-totp"]').setValue('123456')
    await wrapper.get('[data-testid="cleanup-execute"]').trigger('click')
    await flushPromises()

    expect(mockExecute).toHaveBeenCalledWith({
      category: 'bank_ledger',
      mode: 'range',
      start_time: '2026-07-01T00:00:00+08:00',
      end_time: '2026-07-04T00:00:00+08:00',
      preview_rows: 7,
      confirmation: 'DELETE bank_ledger 7',
      totp_code: '123456',
      preview_token: 'bank-token',
    })
    expect(mockShowSuccess).toHaveBeenCalledWith('admin.ops.cleanup.executePending')
  })

  it('展示 detail/reason/message 错误并允许重试', async () => {
    mockGetConfig
      .mockRejectedValueOnce({ response: { data: { detail: 'config detail' } } })
      .mockImplementationOnce(async () => makeConfig())
    const wrapper = await mountOpened()
    expect(wrapper.get('[data-testid="cleanup-load-error"]').text()).toContain('config detail')

    await wrapper.get('[data-testid="cleanup-load-error"] button').trigger('click')
    await flushPromises()
    await setRange(wrapper)
    mockPreview
      .mockRejectedValueOnce({ response: { data: { reason: 'preview reason' } } })
      .mockResolvedValueOnce(makePreview())
    await wrapper.get('[data-testid="cleanup-preview"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="cleanup-preview-error"]').text()).toContain('preview reason')

    await wrapper.get('[data-testid="cleanup-preview-error"] button').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="cleanup-confirmation"]').setValue('DELETE ops_error_logs 4')
    mockExecute
      .mockRejectedValueOnce({ response: { data: { message: 'execute message' } } })
      .mockResolvedValueOnce({ audit_id: 2, status: 'succeeded', deleted_rows: 4 })
    await wrapper.get('[data-testid="cleanup-execute"]').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-testid="cleanup-execute-error"]').text()).toContain('execute message')

    await wrapper.get('[data-testid="cleanup-execute-error"] button').trigger('click')
    await flushPromises()
    expect(mockExecute).toHaveBeenCalledTimes(2)
  })

  it('显示完整审计字段并轮询 pending/running 任务', async () => {
    vi.useFakeTimers()
    mockListAudits
      .mockResolvedValueOnce([{
        id: 3,
        operator_id: 9,
        operator_email: 'admin@example.com',
        auth_method: 'session',
        category: 'usage_logs',
        mode: 'range',
        filters: '{"task_id":88}',
        preview_rows: 10,
        blocked_rows: 1,
        deleted_rows: 0,
        status: 'pending',
        error_message: 'queued detail',
        started_at: '2026-07-24T00:00:00Z',
        created_at: '2026-07-24T00:00:00Z',
      }])
      .mockResolvedValueOnce([{
        id: 3,
        operator_id: 9,
        operator_email: 'admin@example.com',
        auth_method: 'session',
        category: 'usage_logs',
        mode: 'range',
        filters: '{"task_id":88}',
        preview_rows: 10,
        blocked_rows: 1,
        deleted_rows: 9,
        status: 'succeeded',
        error_message: '',
        started_at: '2026-07-24T00:00:00Z',
        finished_at: '2026-07-24T00:00:01Z',
        created_at: '2026-07-24T00:00:00Z',
      }])

    const wrapper = await mountOpened()
    expect(wrapper.get('[data-testid="cleanup-audit-pending"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin@example.com')
    expect(wrapper.text()).toContain('usage_logs')
    expect(wrapper.text()).toContain('range')
    expect(wrapper.text()).toContain('"preview":10,"blocked":1,"deleted":0')
    expect(wrapper.text()).toContain('88')
    expect(wrapper.text()).toContain('queued detail')

    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()
    expect(mockListAudits).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="cleanup-audit-pending"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('"preview":10,"blocked":1,"deleted":9')
  })

  it('Escape 和关闭只发出 close，返回只发出 back，并正确维护滚动锁', async () => {
    const wrapper = await mountOpened()
    expect(document.body.classList.contains('modal-open')).toBe(true)

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(wrapper.emitted('back')).toBeUndefined()

    await wrapper.get('[data-testid="cleanup-close"]').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(2)
    await wrapper.get('[data-testid="cleanup-back"]').trigger('click')
    expect(wrapper.emitted('back')).toEqual([[]])

    await wrapper.setProps({ show: false })
    await nextTick()
    expect(document.body.classList.contains('modal-open')).toBe(false)
  })

  it('Cron 非五段格式时提示错误并禁用保存', async () => {
    const wrapper = await mountOpened()
    const cron = wrapper.get('[data-testid="cleanup-cron"]')
    const save = wrapper.get('[data-testid="cleanup-save"]')

    await cron.setValue('0 0 2 * * *')
    expect(save.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('admin.ops.cleanup.cronInvalid')

    await cron.setValue('0 2 * * *')
    expect(save.attributes('disabled')).toBeUndefined()
    await save.trigger('click')
    await flushPromises()
    expect(mockUpdateConfig).toHaveBeenCalledTimes(1)
  })
})
