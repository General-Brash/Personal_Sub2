import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import DataCleanupView from '../DataCleanupView.vue'

const api = vi.hoisted(() => ({ config: vi.fn(), overview: vi.fn(), audits: vi.fn(), preview: vi.fn(), execute: vi.fn(), save: vi.fn(), cancel: vi.fn() }))
vi.mock('@/api/admin/dataCleanup', () => ({ dataCleanupAPI: api }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess: vi.fn() }) }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ canAdmin: () => true }) }))
vi.mock('@/features/prompt-audit/api', () => ({ default: {} }))
vi.mock('vue-i18n', async importOriginal => ({ ...await importOriginal<typeof import('vue-i18n')>(), useI18n: () => ({ t: (key: string) => key, locale: { value: 'en' } }) }))

let wrapper: VueWrapper
beforeEach(() => {
  api.config.mockResolvedValue({ data_retention: { cleanup_enabled: false, cleanup_schedule: '0 2 * * *', targets: {} }, audit_log_retention_days: 180 })
  api.overview.mockResolvedValue({ tables: [], months: [], requests: 5, tokens: 25, actual_cost: '0.1234567890', database_bytes: 1024, timezone: 'Asia/Shanghai' })
  api.audits.mockResolvedValue([])
  api.preview.mockResolvedValue({ matched_rows: 2, blocked_rows: 1, confirmation: 'COMPACT usage_logs 2', requires_totp: true, preview_token: 'signed-snapshot' })
  api.execute.mockResolvedValue({ status: 'pending', task_id: 1 })
  wrapper = mount(DataCleanupView, { global: { stubs: { AppLayout: { template: '<main><slot /></main>' }, Icon: true, Toggle: true, BaseDialog: true } } })
})
afterEach(() => { wrapper.unmount(); vi.clearAllMocks() })

describe('DataCleanupView', () => {
  it('compacts only a completed month with signed preview and two-factor confirmation', async () => {
    await flushPromises()
    await wrapper.findAll('nav button')[1].trigger('click')
    const select = wrapper.get('select')
    expect(select.findAll('option').some(option => option.element.value === 'bank_ledger')).toBe(false)
    await select.setValue('usage_logs')
    await wrapper.get('input[type="month"]').setValue('2026-10')
    await wrapper.findAll('button').find(button => button.text().includes('dataCleanup.preview'))!.trigger('click')
    await flushPromises()
    expect(api.preview).toHaveBeenCalledWith({ category: 'usage_logs', mode: 'range', month: '2026-10' }, expect.any(AbortSignal))
    const execute = wrapper.findAll('button').find(button => button.text().includes('dataCleanup.compact'))!
    expect(execute.attributes('disabled')).toBeDefined()
    await wrapper.get('input[autocomplete="off"]').setValue('COMPACT usage_logs 2')
    await wrapper.get('input[autocomplete="one-time-code"]').setValue('123456')
    await execute.trigger('click')
    await flushPromises()
    expect(api.execute).toHaveBeenCalledWith(expect.objectContaining({ preview_token: 'signed-snapshot', preview_rows: 2, totp_code: '123456' }))
  })

  it('invalidates a preview when the selected month changes', async () => {
    await flushPromises()
    await wrapper.findAll('nav button')[1].trigger('click')
    await wrapper.get('select').setValue('usage_logs')
    await wrapper.get('input[type="month"]').setValue('2026-10')
    await wrapper.findAll('button').find(button => button.text().includes('dataCleanup.preview'))!.trigger('click')
    await flushPromises()
    expect(wrapper.find('input[autocomplete="off"]').exists()).toBe(true)
    await wrapper.get('input[type="month"]').setValue('2026-09')
    expect(wrapper.find('input[autocomplete="off"]').exists()).toBe(false)
    expect(api.execute).not.toHaveBeenCalled()
  })
})
