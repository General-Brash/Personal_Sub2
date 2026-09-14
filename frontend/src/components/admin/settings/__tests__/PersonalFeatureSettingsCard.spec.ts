import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import PersonalFeatureSettingsCard from '../PersonalFeatureSettingsCard.vue'
const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
const auth = vi.hoisted(() => ({ user: { id: 1, role: 'admin', permission_mode: 'enforce', permissions: ['system.settings.manage'] as string[] | undefined }, refreshUser: vi.fn(), canAdmin: vi.fn() }))
const app = vi.hoisted(() => ({ showSuccess: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => app }))
let wrappers: VueWrapper[] = []
const policy = () => ({ model_plaza_v2_enabled: false, player_invitations_enabled: false, invitation_ttl_seconds: 0, source_expiry: { checkin: '00:00', admin_grant: '00:00' }, version: 'v1' })
function render() { const wrapper = mount(PersonalFeatureSettingsCard); wrappers.push(wrapper); return wrapper }
beforeEach(() => {
  vi.resetAllMocks(); auth.user.permission_mode = 'enforce'; auth.user.permissions = ['system.settings.manage']
  auth.refreshUser.mockImplementation(async () => auth.user)
  auth.canAdmin.mockImplementation(permission => auth.user.permissions?.includes(permission) === true)
  client.get.mockResolvedValue({ data: policy() }); client.put.mockResolvedValue({ data: { ...policy(), version: 'v2' } })
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = [] })
describe('Personal settings write capability', () => {
  it('requires system.settings.manage, not just enforce mode', async () => {
    auth.user.permissions = []
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('没有 system.settings.manage 权限')
    expect(wrapper.get('[data-testid="personal-settings-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('fieldset').attributes('disabled')).toBeDefined()
    expect(client.put).not.toHaveBeenCalled()
  })
  it('distinguishes unknown permission metadata from mode-off', async () => {
    auth.user.permissions = undefined
    const wrapper = render(); await flushPromises()
    expect(wrapper.text()).toContain('写入能力未知')
    expect(wrapper.text()).not.toContain('enforce 未开启，当前设置只读')
    expect(wrapper.get('[data-testid="personal-settings-save"]').attributes('disabled')).toBeDefined()
  })
  it('preserves the success acknowledgement when the subsequent read fails', async () => {
    const wrapper = render(); await flushPromises()
    client.get.mockRejectedValueOnce({ status: 503 })
    await wrapper.get('[data-testid="personal-settings-save"]').trigger('click'); await flushPromises()
    expect(wrapper.get('[data-testid="personal-settings-status"]').text()).toContain('保存成功，但刷新失败')
    expect(client.put).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-testid="personal-settings-save"]').attributes('disabled')).toBeDefined()
  })
  it('requires a reread after an unknown write and never retries automatically', async () => {
    client.put.mockRejectedValueOnce({ status: 0 })
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="personal-settings-save"]').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('保存结果未知')
    expect(wrapper.get('[data-testid="personal-settings-save"]').attributes('disabled')).toBeDefined()
    expect(client.put).toHaveBeenCalledTimes(1)
  })
  it.each(['disabled', 'shadow'])('keeps writes disabled during %s migration mode', async mode => {
    auth.user.permission_mode = mode
    const wrapper = render(); await flushPromises()
    const save = wrapper.get('[data-testid="personal-settings-save"]')
    expect(save.attributes('disabled')).toBeDefined()
    await save.trigger('click')
    expect(client.put).not.toHaveBeenCalled()
  })
  it('sends the displayed version and never retries stale writes silently', async () => {
    client.put.mockRejectedValueOnce({ status: 409, code: 'CHECKIN_POLICY_VERSION_STALE' })
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="personal-settings-save"]').trigger('click'); await flushPromises()
    expect(client.put).toHaveBeenCalledWith('/admin/settings/personal-features', expect.objectContaining({ version: 'v1', player_invitations_enabled: false, model_plaza_v2_enabled: false }))
    expect(client.put).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('重新读取并确认')
  })

})
