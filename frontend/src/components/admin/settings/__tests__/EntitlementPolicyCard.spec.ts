import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import EntitlementPolicyCard from '../EntitlementPolicyCard.vue'
const api = vi.hoisted(() => ({ getEntitlementCatalog: vi.fn(), updateEntitlementPolicy: vi.fn() }))
vi.mock('@/api/adminEntitlements', () => api)
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ user: { id: 1 } }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string, params?: Record<string, unknown>) => (params ? `${key} ${Object.values(params).join(' ')}` : key) }) }
})
let premium: any, capabilities: any
let wrappers: VueWrapper[] = []
const copy = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
const catalog = () => ({ tiers: [{ tier: 'standard', display_name: 'Standard', enabled: true, version: 1, groups: [] }, copy(premium)], capabilities })
function render() { const wrapper = mount(EntitlementPolicyCard); wrappers.push(wrapper); return wrapper }
// onMounted auto-loads the catalog now; the manual "reload" button remains but tests only need to await the mount-time load.
async function load(_wrapper: VueWrapper) { await flushPromises() }
beforeEach(() => {
  vi.resetAllMocks(); sessionStorage.clear()
  premium = { tier: 'premium', display_name: 'Premium', enabled: false, version: 3, groups: [{ group_id: 1, source: 'tier' }] }
  capabilities = { mode: 'enforce', writes_enabled: true, can_write: true }
  api.getEntitlementCatalog.mockImplementation(async () => catalog())
  api.updateEntitlementPolicy.mockImplementation(async input => { premium = { tier: input.tier, display_name: input.display_name, enabled: input.enabled, version: input.expected_version + 1, groups: copy(input.groups) }; return copy(premium) })
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = []; sessionStorage.clear() })

describe('P3.5 policy lifecycle', () => {
  it('saves content without enabling, then enables with a separate CAS request', async () => {
    const wrapper = render(); await load(wrapper)
    await wrapper.findAll('input')[0].setValue('Preferred')
    await wrapper.get('[data-testid="policy-reason"]').setValue('save policy')
    expect(wrapper.get('[data-testid="policy-toggle"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="policy-save"]').trigger('click'); await flushPromises()
    expect(api.updateEntitlementPolicy.mock.calls[0][0]).toMatchObject({ enabled: false, expected_version: 3, display_name: 'Preferred', request_id: expect.any(String) })
    expect(wrapper.get('[data-testid="policy-status"]').text()).toContain('entitlementPolicy.savedNotice')
    await wrapper.get('[data-testid="policy-reason"]').setValue('enable policy')
    await wrapper.get('[data-testid="policy-toggle"]').trigger('click'); await flushPromises()
    expect(api.updateEntitlementPolicy.mock.calls[1][0]).toMatchObject({ enabled: true, expected_version: 4, reason: 'enable policy' })
    expect(api.updateEntitlementPolicy.mock.calls[0][0].request_id).not.toBe(api.updateEntitlementPolicy.mock.calls[1][0].request_id)
    expect(wrapper.get('[data-testid="policy-status"]').text()).toContain('entitlementPolicy.enabled')
  })

  it.each(['missing', 'forbidden', 'mode-off'])('does not write when capability is %s', async state => {
    capabilities = state === 'missing' ? undefined : { mode: state === 'mode-off' ? 'disabled' : 'enforce', writes_enabled: state !== 'mode-off', can_write: false }
    const wrapper = render(); await load(wrapper)
    await wrapper.get('[data-testid="policy-reason"]').setValue('reason')
    expect(wrapper.get('[data-testid="policy-save"]').attributes('disabled')).toBeDefined()
    if (state === 'missing') expect(wrapper.text()).toContain('entitlementPolicy.cap.unknown')
    expect(api.updateEntitlementPolicy).not.toHaveBeenCalled()
  })

  it('reuses the frozen policy request on a network-unknown result', async () => {
    api.updateEntitlementPolicy.mockRejectedValueOnce({ status: 0 })
    const wrapper = render(); await load(wrapper)
    await wrapper.get('[data-testid="policy-reason"]').setValue('reason')
    await wrapper.get('[data-testid="policy-save"]').trigger('click'); await flushPromises()
    const original = copy(api.updateEntitlementPolicy.mock.calls[0][0])
    expect(wrapper.get('[data-testid="policy-retry"]').exists()).toBe(true)
    await wrapper.get('[data-testid="policy-retry"]').trigger('click'); await flushPromises()
    expect(api.updateEntitlementPolicy.mock.calls[1][0]).toEqual(original)
    expect(sessionStorage.getItem('sub2:entitlement-policy-pending:v1:1')).toBeNull()
  })

  it('reports save success separately from refresh failure', async () => {
    const wrapper = render(); await load(wrapper)
    api.getEntitlementCatalog.mockRejectedValueOnce({ status: 503 })
    await wrapper.get('[data-testid="policy-reason"]').setValue('reason')
    await wrapper.get('[data-testid="policy-save"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="policy-status"]').text()).toContain('entitlementPolicy.savedRefreshFailed')
    expect(wrapper.find('[data-testid="policy-retry"]').exists()).toBe(false)
  })

  it('requires a reread after CAS conflict instead of automatically overwriting', async () => {
    api.updateEntitlementPolicy.mockRejectedValueOnce({ status: 409 })
    const wrapper = render(); await load(wrapper)
    await wrapper.get('[data-testid="policy-reason"]').setValue('reason')
    await wrapper.get('[data-testid="policy-save"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="policy-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('entitlementPolicy.err.casConflict')
    expect(api.updateEntitlementPolicy).toHaveBeenCalledTimes(1)
  })
})
