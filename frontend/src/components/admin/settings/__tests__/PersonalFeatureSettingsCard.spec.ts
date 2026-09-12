import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import PersonalFeatureSettingsCard from '../PersonalFeatureSettingsCard.vue'
const { get, put, auth, showSuccess } = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), auth: { user: { permission_mode: 'disabled' } }, showSuccess: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: { get, put } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showSuccess }) }))
function policy(version = 'v1') { return { model_plaza_v2_enabled: false, player_invitations_enabled: false, invitation_ttl_seconds: 0, source_expiry: { checkin: '00:00', admin_grant: '00:00' }, version } }
beforeEach(() => { vi.resetAllMocks(); auth.user.permission_mode = 'disabled'; get.mockResolvedValue({ data: policy() }); put.mockResolvedValue({ data: policy('v2') }) })
describe('PersonalFeatureSettingsCard', () => {
 it('keeps writes disabled during the shadow/compatibility migration', async () => {
  const wrapper = mount(PersonalFeatureSettingsCard); await flushPromises()
  const save = wrapper.findAll('button').find(button => button.text() === '确认对新批次生效')!
  expect(save.attributes('disabled')).toBeDefined(); await save.trigger('click'); expect(put).not.toHaveBeenCalled(); wrapper.unmount()
 })
 it('sends the displayed version with policy changes and does not retry stale writes silently', async () => {
  auth.user.permission_mode = 'enforce'; const wrapper = mount(PersonalFeatureSettingsCard); await flushPromises()
  put.mockRejectedValueOnce({ code: 'CHECKIN_POLICY_VERSION_STALE' })
  const save = wrapper.findAll('button').find(button => button.text() === '确认对新批次生效')!
  await save.trigger('click'); await flushPromises()
  expect(put).toHaveBeenCalledWith('/admin/settings/personal-features', expect.objectContaining({ version: 'v1', player_invitations_enabled: false }))
  expect(put).toHaveBeenCalledTimes(1); expect(wrapper.get('[role="alert"]').text()).toContain('重新确认'); wrapper.unmount()
 })
})
