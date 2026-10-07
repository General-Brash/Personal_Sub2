import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import AdminPermissionsPanel from '../AdminPermissionsPanel.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'

const api = vi.hoisted(() => ({
  listAdminPermissionCatalog: vi.fn(),
  getUserAdminPermissions: vi.fn(),
  grantUserAdminPermission: vi.fn(),
  revokeUserAdminPermission: vi.fn(),
}))
vi.mock('@/api/adminPermissions', () => api)
const auth = vi.hoisted(() => ({ user: null as { id: number; role: string } | null, refreshUser: vi.fn() }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => auth }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

type Grant = { permission: string; effect: 'allow' | 'deny'; scope?: Record<string, unknown> }
const def = (permission: string, sensitive = false) => {
  const [resource, ...action] = permission.split('.')
  return { permission, resource, action: action.join('.'), sensitive, description: `说明 ${permission}` }
}
// 与后端一致：按 resource, action 排序返回
const catalog = () => [
  def('groups.create'), def('groups.rates.manage'), def('groups.read'), def('groups.update'),
  def('oidc.clients.read'), def('oidc.keys.rotate', true),
  def('security.permissions.grant'),
  def('users.delete', true), def('users.read'), def('users.update'),
]
const grants = (): Grant[] => [
  { permission: 'groups.create', effect: 'deny' },
  { permission: 'groups.read', effect: 'allow', scope: { '*': '*' } },
  { permission: 'users.read', effect: 'allow', scope: { '*': '*' } },
  { permission: 'users.update', effect: 'allow', scope: { group_id: 3 } },
]
const permissionState = (overrides: Record<string, unknown> = {}) => ({
  user_id: 7,
  role: 'admin',
  version: 1,
  grants: grants(),
  capabilities: { writes_enabled: true, can_write: true, mode: 'enforce' },
  ...overrides,
})
function deferred<T>() { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((res, rej) => { resolve = res; reject = rej }); return { promise, resolve, reject } }

let wrappers: VueWrapper[] = []
function render(props: Record<string, unknown> = {}) {
  const wrapper = mount(AdminPermissionsPanel, {
    props: { show: true, userId: 7, ...props },
    global: {
      stubs: {
        BaseDialog: { props: ['show'], template: '<div v-if="show"><slot /><slot name="footer" /></div>' },
        TotpStepUpDialog: true,
      },
    },
  })
  wrappers.push(wrapper)
  return wrapper
}
const tid = (id: string) => `[data-testid="${id}"]`
const isDisabled = (wrapper: VueWrapper, id: string) => (wrapper.get(tid(id)).element as HTMLButtonElement).disabled
const sectionIds = (wrapper: VueWrapper) => wrapper.findAll('section[data-testid^="permission-section-"]').map(section => section.attributes('data-testid'))
const reasonValue = (wrapper: VueWrapper) => (wrapper.get(tid('permission-reason')).element as HTMLInputElement).value
function buttonByText(wrapper: VueWrapper, text: string) {
  const button = wrapper.findAll('button').find(item => item.text() === text)
  if (!button) throw new Error(`button "${text}" not found`)
  return button
}
async function ready(wrapper: VueWrapper, reason = 'audit reason') {
  await flushPromises()
  await wrapper.get(tid('permission-reason')).setValue(reason)
}

beforeEach(() => {
  vi.resetAllMocks()
  auth.user = { id: 1, role: 'super_admin' }
  api.listAdminPermissionCatalog.mockImplementation(async () => catalog())
  api.getUserAdminPermissions.mockImplementation(async () => permissionState())
  api.grantUserAdminPermission.mockResolvedValue(undefined)
  api.revokeUserAdminPermission.mockResolvedValue(undefined)
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = [] })

describe('AdminPermissionsPanel 分组与批量授权', () => {
  it('按 resource 分组、按板块常量表排序，默认全部折叠', async () => {
    const wrapper = render(); await flushPromises()
    expect(sectionIds(wrapper)).toEqual(['permission-section-users', 'permission-section-groups', 'permission-section-security', 'permission-section-oidc'])
    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
    const users = wrapper.get(tid('permission-section-users')).text()
    expect(users).toContain('用户')
    expect(users).toContain('已允许 2 / 总数 3')
    expect(users).not.toContain('· 拒绝')
    expect(wrapper.get(tid('permission-section-groups')).text()).toContain('已允许 1 / 总数 4 · 拒绝 1')
    expect(wrapper.get(tid('permission-oidc-note')).text()).toContain('OIDC 权限必须逐条显式授予')
    // 原因为空时批量按钮禁用
    expect(isDisabled(wrapper, 'permission-batch-allow-groups')).toBe(true)

    await wrapper.get(tid('permission-section-toggle-users')).trigger('click')
    expect(wrapper.findAll('tbody tr')).toHaveLength(3)
    expect(wrapper.get(tid('permission-section-users')).text()).toContain('users.delete')
    expect(wrapper.get(tid('permission-section-users')).text()).toContain('· 敏感')
  })

  it('未登记的 resource 进入“其他 · xxx”并排在已知板块之后', async () => {
    api.listAdminPermissionCatalog.mockImplementation(async () => [
      { permission: 'zeta.run', resource: 'zeta', action: 'run', sensitive: false, description: '' },
      { permission: 'alpha.view', resource: 'alpha', action: 'view', sensitive: false, description: '' },
      ...catalog(),
    ])
    const wrapper = render(); await flushPromises()
    const ids = sectionIds(wrapper)
    expect(ids).toHaveLength(6)
    expect(ids.slice(-2)).toEqual(['permission-section-alpha', 'permission-section-zeta'])
    expect(wrapper.get(tid('permission-section-alpha')).text()).toContain('其他 · alpha')
    expect(wrapper.get(tid('permission-section-zeta')).text()).toContain('其他 · zeta')
  })

  it('全部允许跳过已全局允许的项，逐条顺序提交且共用操作原因', async () => {
    const first = deferred<void>()
    api.grantUserAdminPermission.mockReturnValueOnce(first.promise)
    const wrapper = render(); await ready(wrapper, 'batch reason')
    await wrapper.get(tid('permission-section-toggle-groups')).trigger('click')
    await wrapper.get(tid('permission-batch-allow-groups')).trigger('click'); await flushPromises()

    // 第一条未完成前不会发出第二条
    expect(api.grantUserAdminPermission).toHaveBeenCalledTimes(1)
    expect(wrapper.get(tid('permission-batch-progress')).text()).toContain('进度 0 / 3')
    expect(isDisabled(wrapper, 'permission-batch-allow-users')).toBe(true)
    expect(isDisabled(wrapper, 'permission-batch-deny-groups')).toBe(true)
    const rowButtons = wrapper.get(tid('permission-section-groups')).findAll('tbody button')
    expect(rowButtons.length).toBeGreaterThan(0)
    expect(rowButtons.every(button => (button.element as HTMLButtonElement).disabled)).toBe(true)

    first.resolve(); await flushPromises()
    const body = (permission: string) => ({ permission, effect: 'allow', scope: { '*': '*' }, reason: 'batch reason' })
    expect(api.grantUserAdminPermission.mock.calls).toEqual([
      [7, body('groups.create')],
      [7, body('groups.rates.manage')],
      [7, body('groups.update')],
    ])
    expect(wrapper.find(tid('permission-batch-progress')).exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
    // 操作后的刷新不清空原因，便于连续批量操作
    expect(reasonValue(wrapper)).toBe('batch reason')
  })

  it('中途失败会继续执行后续项，结束后汇总失败清单且只刷新一次', async () => {
    api.grantUserAdminPermission
      .mockRejectedValueOnce({ status: 409, message: 'stale version' })
      .mockRejectedValueOnce({ status: 500 })
    const wrapper = render(); await ready(wrapper)
    expect(api.getUserAdminPermissions).toHaveBeenCalledTimes(1)
    await wrapper.get(tid('permission-batch-allow-groups')).trigger('click'); await flushPromises()

    expect(api.grantUserAdminPermission).toHaveBeenCalledTimes(3)
    const failures = wrapper.get(tid('permission-batch-failures')).text()
    expect(failures).toContain('以下 2 项失败：')
    expect(failures).toContain('groups.create：stale version')
    expect(failures).toContain('groups.rates.manage：未知错误')
    expect(failures).not.toContain('groups.update')
    expect(api.getUserAdminPermissions).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('本批含敏感权限时先确认：确认前与取消后都不调用接口', async () => {
    const wrapper = render(); await ready(wrapper)
    await wrapper.get(tid('permission-batch-allow-users')).trigger('click'); await flushPromises()
    const confirm = wrapper.getComponent(ConfirmDialog)
    expect(confirm.props('show')).toBe(true)
    expect(confirm.props('danger')).toBe(true)
    expect(confirm.props('confirmText')).toBe('确认执行')
    expect(confirm.props('cancelText')).toBe('取消')
    expect(confirm.text()).toContain('users.delete')
    expect(api.grantUserAdminPermission).not.toHaveBeenCalled()

    await buttonByText(wrapper, '取消').trigger('click'); await flushPromises()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(false)
    expect(api.grantUserAdminPermission).not.toHaveBeenCalled()

    await wrapper.get(tid('permission-batch-allow-users')).trigger('click')
    await buttonByText(wrapper, '确认执行').trigger('click'); await flushPromises()
    expect(api.grantUserAdminPermission.mock.calls.map(call => call[1].permission)).toEqual(['users.delete', 'users.update'])
  })

  it('安全 / OIDC 板块即使无敏感项也要确认，普通板块无敏感项直接执行', async () => {
    api.getUserAdminPermissions.mockImplementation(async () => permissionState({
      grants: [...grants(), { permission: 'oidc.keys.rotate', effect: 'allow', scope: { '*': '*' } }],
    }))
    const wrapper = render(); await ready(wrapper)

    await wrapper.get(tid('permission-batch-allow-security')).trigger('click'); await flushPromises()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(true)
    await buttonByText(wrapper, '取消').trigger('click')

    // oidc 本批只剩非敏感的 oidc.clients.read，仍需确认
    await wrapper.get(tid('permission-batch-allow-oidc')).trigger('click'); await flushPromises()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(true)
    await buttonByText(wrapper, '取消').trigger('click'); await flushPromises()
    expect(api.grantUserAdminPermission).not.toHaveBeenCalled()

    await wrapper.get(tid('permission-batch-allow-groups')).trigger('click'); await flushPromises()
    expect(wrapper.getComponent(ConfirmDialog).props('show')).toBe(false)
    expect(api.grantUserAdminPermission).toHaveBeenCalledTimes(3)
  })

  it.each([
    ['disabled', true],
    ['enforce', false],
  ])('mode=%s 时 disabled 提示显示=%s', async (mode, visible) => {
    api.getUserAdminPermissions.mockImplementation(async () => permissionState({ capabilities: { writes_enabled: true, can_write: true, mode } }))
    const wrapper = render(); await flushPromises()
    expect(wrapper.find(tid('permission-mode-disabled-notice')).exists()).toBe(visible)
  })

  it.each([
    [{ status: 403, code: 'STEP_UP_TOTP_NOT_ENABLED', message: 'totp required' }, '此操作需要开启二次验证（TOTP），请先在个人资料中启用。'],
    [{ status: 403, reason: 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' }, '管理 API Key 无法执行此操作，请使用已通过二次验证的管理员会话。'],
  ])('二次验证被阻断时中止剩余项并提示（%#）', async (error, text) => {
    api.grantUserAdminPermission.mockRejectedValueOnce(error)
    const wrapper = render(); await ready(wrapper)
    await wrapper.get(tid('permission-batch-allow-groups')).trigger('click'); await flushPromises()

    expect(api.grantUserAdminPermission).toHaveBeenCalledTimes(1)
    expect(wrapper.get(tid('permission-batch-notice')).text()).toBe(text)
    expect(wrapper.find(tid('permission-batch-failures')).exists()).toBe(false)
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('取消二次验证时中止剩余项并提示剩余数量', async () => {
    api.grantUserAdminPermission.mockRejectedValueOnce({ status: 403, code: 'STEP_UP_REQUIRED' })
    const wrapper = render(); await ready(wrapper)
    await wrapper.get(tid('permission-batch-allow-groups')).trigger('click'); await flushPromises()
    const controller = wrapper.getComponent(TotpStepUpDialog).props('controller')
    expect(controller.visible.value).toBe(true)

    controller.onCancel(); await flushPromises()
    expect(api.grantUserAdminPermission).toHaveBeenCalledTimes(1)
    expect(wrapper.get(tid('permission-batch-notice')).text()).toBe('已取消二次验证，剩余 3 项未执行。')
  })

  it('二次验证通过后重试当前项并继续后续项', async () => {
    api.grantUserAdminPermission.mockRejectedValueOnce({ status: 403, code: 'STEP_UP_REQUIRED' })
    const wrapper = render(); await ready(wrapper)
    await wrapper.get(tid('permission-batch-allow-groups')).trigger('click'); await flushPromises()

    wrapper.getComponent(TotpStepUpDialog).props('controller').onVerified(); await flushPromises()
    expect(api.grantUserAdminPermission.mock.calls.map(call => call[1].permission)).toEqual(['groups.create', 'groups.create', 'groups.rates.manage', 'groups.update'])
    expect(wrapper.find(tid('permission-batch-notice')).exists()).toBe(false)
    expect(wrapper.emitted('changed')).toHaveLength(1)
  })

  it('超管打开自己的面板：只有 OIDC 板块可写，成功后刷新当前用户', async () => {
    auth.user = { id: 7, role: 'super_admin' }
    api.getUserAdminPermissions.mockImplementation(async () => permissionState({ role: 'super_admin' }))
    const wrapper = render(); await ready(wrapper)
    expect(wrapper.get(tid('permission-self-hint')).text()).toBe('这是你自己的账号：超管默认拥有除 OIDC 外的全部权限，只能为自己授予 / 撤销 OIDC 权限。')
    for (const resource of ['users', 'groups', 'security']) {
      for (const action of ['allow', 'deny', 'revoke']) expect(isDisabled(wrapper, `permission-batch-${action}-${resource}`)).toBe(true)
    }
    expect(isDisabled(wrapper, 'permission-batch-allow-oidc')).toBe(false)
    await wrapper.get(tid('permission-section-toggle-groups')).trigger('click')
    const groupButtons = wrapper.get(tid('permission-section-groups')).findAll('tbody button')
    expect(groupButtons.every(button => (button.element as HTMLButtonElement).disabled)).toBe(true)
    expect(wrapper.get(tid('permission-section-groups')).find('input').exists()).toBe(false)

    await wrapper.get(tid('permission-batch-allow-oidc')).trigger('click')
    await buttonByText(wrapper, '确认执行').trigger('click'); await flushPromises()
    const body = (permission: string) => ({ permission, effect: 'allow', scope: { '*': '*' }, reason: 'audit reason' })
    expect(api.grantUserAdminPermission.mock.calls).toEqual([[7, body('oidc.clients.read')], [7, body('oidc.keys.rotate')]])
    expect(auth.refreshUser).toHaveBeenCalledTimes(1)
  })

  it('普通管理员打开自己的面板：所有写操作禁用', async () => {
    auth.user = { id: 7, role: 'admin' }
    const wrapper = render(); await flushPromises()
    expect(wrapper.get(tid('permission-self-hint')).text()).toBe('不能修改自己的管理员权限。')
    expect(wrapper.find(tid('permission-reason')).exists()).toBe(false)
    const batchButtons = wrapper.findAll('button[data-testid^="permission-batch-"]')
    expect(batchButtons).toHaveLength(12)
    expect(batchButtons.every(button => (button.element as HTMLButtonElement).disabled)).toBe(true)
  })

  it('全部撤销只撤销已授予项，全部拒绝提交空 scope', async () => {
    const wrapper = render(); await ready(wrapper)
    expect(isDisabled(wrapper, 'permission-batch-revoke-security')).toBe(true)

    await wrapper.get(tid('permission-batch-revoke-users')).trigger('click'); await flushPromises()
    expect(api.revokeUserAdminPermission.mock.calls).toEqual([[7, 'users.read', 'audit reason'], [7, 'users.update', 'audit reason']])

    await wrapper.get(tid('permission-batch-deny-groups')).trigger('click'); await flushPromises()
    const body = (permission: string) => ({ permission, effect: 'deny', scope: {}, reason: 'audit reason' })
    // groups.create 已是全局 deny（无 scope），跳过
    expect(api.grantUserAdminPermission.mock.calls).toEqual([
      [7, body('groups.rates.manage')],
      [7, body('groups.read')],
      [7, body('groups.update')],
    ])
  })

  it('单行操作同样经过二次验证包装，失败信息显示在提示区', async () => {
    api.grantUserAdminPermission.mockRejectedValueOnce({ status: 400, message: 'scope invalid' })
    const wrapper = render(); await ready(wrapper)
    await wrapper.get(tid('permission-section-toggle-groups')).trigger('click')
    const row = wrapper.findAll('tbody tr').find(item => item.text().includes('groups.update'))
    await row!.findAll('button').find(button => button.text() === 'Allow')!.trigger('click'); await flushPromises()

    expect(api.grantUserAdminPermission).toHaveBeenCalledWith(7, { permission: 'groups.update', effect: 'allow', scope: { '*': '*' }, reason: 'audit reason' })
    expect(wrapper.get(tid('permission-batch-notice')).text()).toBe('groups.update：scope invalid')
    expect(wrapper.emitted('changed')).toBeUndefined()
  })

  it('重新打开面板时清空原因、失败清单并恢复折叠', async () => {
    api.grantUserAdminPermission.mockRejectedValueOnce({ status: 409, message: 'boom' })
    const wrapper = render(); await ready(wrapper)
    await wrapper.get(tid('permission-section-toggle-groups')).trigger('click')
    await wrapper.get(tid('permission-batch-allow-groups')).trigger('click'); await flushPromises()
    expect(wrapper.find(tid('permission-batch-failures')).exists()).toBe(true)
    expect(wrapper.findAll('tbody tr')).toHaveLength(4)

    await wrapper.setProps({ show: false }); await wrapper.setProps({ show: true }); await flushPromises()
    expect(wrapper.find(tid('permission-batch-failures')).exists()).toBe(false)
    expect(reasonValue(wrapper)).toBe('')
    expect(wrapper.findAll('tbody tr')).toHaveLength(0)
  })
})
