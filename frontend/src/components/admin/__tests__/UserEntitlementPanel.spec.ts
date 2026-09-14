import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import UserEntitlementPanel from '../UserEntitlementPanel.vue'

const api = vi.hoisted(() => ({ getUserEntitlement: vi.fn(), getEntitlementCatalog: vi.fn(), previewEntitlementChange: vi.fn(), applyEntitlementChange: vi.fn(), updateUserEntitlement: vi.fn() }))
vi.mock('@/api/adminEntitlements', () => api)
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 1 } }) }))
const cap = { mode: 'enforce', writes_enabled: true, can_write: true }
const entitlement = (id = 7) => ({ user_id: id, tier: 'standard', tier_enabled: true, version: 1, sources: [], manual_groups: [], subscription_groups: [], tier_groups: [], allowed_groups: [], default_rates: {}, capabilities: { ...cap } })
const catalog = () => ({ tiers: [{ tier: 'standard', enabled: true, version: 1, groups: [] }, { tier: 'premium', enabled: true, version: 3, groups: [] }], capabilities: { ...cap } })
const preview = (tier = 'premium', ids = [7], token = `${tier}-token`) => ({ tier, user_ids: ids, affected_user_ids: ids, already_at_tier: [], granted_group_ids: [], revoked_group_ids: [], preserved_manual_groups: true, preserved_subscriptions: true, policy_enabled: true, policy_version: 3, preview_token: token, expires_at: new Date(Date.now() + 600000).toISOString() })
const result = (tier = 'premium', idempotent = false) => ({ tier, requested: 1, changed: 1, unchanged: 0, version: 4, idempotent })
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }
let wrappers: VueWrapper[] = []
function render(props = {}) { const wrapper = mount(UserEntitlementPanel, { props: { show: true, userId: 7, ...props }, global: { stubs: { BaseDialog: { template: '<div><slot /></div>' } } } }); wrappers.push(wrapper); return wrapper }
beforeEach(() => {
  vi.resetAllMocks(); sessionStorage.clear()
  api.getUserEntitlement.mockImplementation(async (id: number) => entitlement(id))
  api.getEntitlementCatalog.mockResolvedValue(catalog())
  api.previewEntitlementChange.mockImplementation(async (ids: number[], tier: string) => preview(tier, ids))
  api.updateUserEntitlement.mockImplementation(async (_id: number, tier: string) => result(tier))
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = []; sessionStorage.clear() })

async function readyToApply(wrapper: VueWrapper) {
  await flushPromises(); await wrapper.get('[data-testid="entitlement-preview-premium"]').trigger('click'); await flushPromises()
  await wrapper.get('[data-testid="entitlement-reason"]').setValue('audit reason')
}

describe('P3.5 entitlement panel contracts', () => {
  it('keeps basic read-only data after catalog 403 and tolerates old null arrays', async () => {
    api.getUserEntitlement.mockResolvedValue({ ...entitlement(), sources: null, manual_groups: null, subscription_groups: null, capabilities: { ...cap, can_write: false } })
    api.getEntitlementCatalog.mockRejectedValue({ status: 403 })
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('默认 standard')
    expect(wrapper.text()).toContain('已成功读取的基础权益仍可查看')
    expect(wrapper.text()).toContain('没有等级政策目录读取权限')
    expect(wrapper.find('[data-testid="entitlement-apply"]').exists()).toBe(false)
  })

  it('does not describe missing capabilities as mode-off', async () => {
    api.getUserEntitlement.mockResolvedValue({ ...entitlement(), capabilities: undefined })
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('写入能力未知')
    expect(wrapper.text()).not.toContain('权限 enforce 未开启')
  })

  it('uses only the latest tier preview and disables application while it is loading', async () => {
    const first = deferred<ReturnType<typeof preview>>(), second = deferred<ReturnType<typeof preview>>()
    api.previewEntitlementChange.mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise)
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="entitlement-preview-premium"]').trigger('click')
    await wrapper.get('[data-testid="entitlement-reason"]').setValue('audit reason')
    await wrapper.get('[data-testid="entitlement-preview-standard"]').trigger('click')
    expect(wrapper.get('[data-testid="entitlement-apply"]').attributes('disabled')).toBeDefined()
    expect(api.previewEntitlementChange.mock.calls[0][2].aborted).toBe(true)
    second.resolve(preview('standard', [7], 'standard-fresh')); await flushPromises()
    first.resolve(preview('premium', [7], 'premium-stale')); await flushPromises()
    await wrapper.get('[data-testid="entitlement-apply"]').trigger('click'); await flushPromises()
    expect(api.updateUserEntitlement).toHaveBeenCalledWith(7, 'standard', 'audit reason', expect.any(String), 'standard-fresh')
    expect(api.applyEntitlementChange).not.toHaveBeenCalled()
  })

  it('invalidates a prior preview when targets change or a new preview fails', async () => {
    const wrapper = render(); await readyToApply(wrapper)
    expect(wrapper.get('[data-testid="entitlement-apply"]').attributes('disabled')).toBeUndefined()
    await wrapper.setProps({ userIds: [8] }); await flushPromises()
    expect(wrapper.find('[data-testid="entitlement-preview"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="entitlement-apply"]').attributes('disabled')).toBeDefined()
    api.previewEntitlementChange.mockRejectedValueOnce({ status: 503 })
    await wrapper.get('[data-testid="entitlement-preview-premium"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="entitlement-preview"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="entitlement-apply"]').attributes('disabled')).toBeDefined()
  })

  it('allows a disabled policy preview but never applies it', async () => {
    api.previewEntitlementChange.mockResolvedValue({ ...preview(), policy_enabled: false, affected_user_ids: null, already_at_tier: null })
    const wrapper = render(); await readyToApply(wrapper)
    expect(wrapper.text()).toContain('本次预览只读，不可应用')
    expect(wrapper.get('[data-testid="entitlement-apply"]').attributes('disabled')).toBeDefined()
    expect(api.updateUserEntitlement).not.toHaveBeenCalled()
  })

  it('retains the same frozen request across timeout and distinguishes saved-but-refresh-failed', async () => {
    api.updateUserEntitlement.mockRejectedValueOnce({ status: 0, message: 'network error' }).mockResolvedValueOnce(result('premium', true))
    const wrapper = render(); await readyToApply(wrapper)
    await wrapper.get('[data-testid="entitlement-apply"]').trigger('click'); await flushPromises()
    const original = [...api.updateUserEntitlement.mock.calls[0]]
    expect(wrapper.text()).toContain('原请求结果未知')
    expect(wrapper.get('[data-testid="entitlement-reason"]').attributes('disabled')).toBeDefined()
    expect(sessionStorage.getItem('sub2:entitlement-pending:v1:1')).toContain(original[3])
    api.getUserEntitlement.mockRejectedValueOnce({ status: 503 })
    await wrapper.get('[data-testid="entitlement-retry"]').trigger('click'); await flushPromises()
    expect(api.updateUserEntitlement.mock.calls[1]).toEqual(original)
    expect(wrapper.get('[data-testid="entitlement-status"]').text()).toContain('保存成功，但刷新失败')
    expect(wrapper.find('[data-testid="entitlement-retry"]').exists()).toBe(false)
    expect(sessionStorage.getItem('sub2:entitlement-pending:v1:1')).toBeNull()
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('restores an uncertain request after remount rather than generating another key', async () => {
    api.updateUserEntitlement.mockRejectedValueOnce({ status: 0 }).mockResolvedValueOnce(result('premium', true))
    const first = render(); await readyToApply(first)
    await first.get('[data-testid="entitlement-apply"]').trigger('click'); await flushPromises()
    const original = [...api.updateUserEntitlement.mock.calls[0]]
    first.unmount(); wrappers = []
    const reopened = render(); await flushPromises()
    expect(reopened.text()).toContain('原请求结果未知')
    await reopened.get('[data-testid="entitlement-retry"]').trigger('click'); await flushPromises()
    expect(api.updateUserEntitlement.mock.calls[1]).toEqual(original)
  })

  it('treats normalized 403 as a known denial, not an unknown network result', async () => {
    api.updateUserEntitlement.mockRejectedValueOnce({ status: 403 })
    const wrapper = render(); await readyToApply(wrapper)
    await wrapper.get('[data-testid="entitlement-apply"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('当前无写入权限')
    expect(wrapper.find('[data-testid="entitlement-retry"]').exists()).toBe(false)
    expect(sessionStorage.getItem('sub2:entitlement-pending:v1:1')).toBeNull()
  })
})
