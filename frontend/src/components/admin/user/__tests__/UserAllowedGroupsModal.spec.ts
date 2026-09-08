import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

const apiMocks = vi.hoisted(() => ({
  listGroups: vi.fn(),
  updateUser: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    groups: { list: apiMocks.listGroups },
    users: { update: apiMocks.updateUser },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess: vi.fn() }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/components/common/BaseDialog.vue', () => ({
  default: {
    props: ['show'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>',
  },
}))

vi.mock('@/components/common/PlatformIcon.vue', () => ({
  default: { template: '<span />' },
}))

import UserAllowedGroupsModal from '../UserAllowedGroupsModal.vue'

const groups = [
  { id: 1, name: 'Exclusive A', platform: 'openai', is_exclusive: true },
  { id: 2, name: 'Public A', platform: 'anthropic', is_exclusive: false },
  { id: 3, name: 'Public B', platform: 'gemini', is_exclusive: false },
].map((group) => ({
  ...group,
  subscription_type: 'standard',
  status: 'active',
  rate_multiplier: 1,
}))

function makeUser(overrides: Record<string, unknown> = {}) {
  return {
    id: 9,
    email: 'user@example.com',
    allowed_groups: [1],
    group_rates: { 1: 1.25 },
    ...overrides,
  } as any
}

async function mountAndOpen(user = makeUser()) {
  const wrapper = mount(UserAllowedGroupsModal, {
    props: { show: false, user },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

function findRestrictionCheckbox(wrapper: Awaited<ReturnType<typeof mountAndOpen>>) {
  const label = wrapper.findAll('label').find((item) =>
    item.text().includes('admin.users.restrictPublicGroups'),
  )
  expect(label).toBeDefined()
  return label!.get('input[type="checkbox"]')
}

function findPublicCheckbox(wrapper: Awaited<ReturnType<typeof mountAndOpen>>, groupName: string) {
  const card = wrapper.findAll('div.relative').find((item) => item.text().includes(groupName))
  expect(card).toBeDefined()
  return card!.get('input[type="checkbox"]')
}

function save(wrapper: Awaited<ReturnType<typeof mountAndOpen>>) {
  const button = wrapper.findAll('button').find((item) => item.text() === 'common.save')
  expect(button).toBeDefined()
  return button!.trigger('click')
}

describe('UserAllowedGroupsModal public group restriction', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    apiMocks.listGroups.mockResolvedValue({ items: groups })
    apiMocks.updateUser.mockResolvedValue({})
  })

  it('旧用户缺省为不限制，公开分组保持默认可用且不写入 allowed_groups', async () => {
    const wrapper = await mountAndOpen(makeUser({ restrict_public_groups: undefined }))

    expect((findRestrictionCheckbox(wrapper).element as HTMLInputElement).checked).toBe(false)
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(2)

    await save(wrapper)
    await flushPromises()

    expect(apiMocks.updateUser).toHaveBeenCalledWith(9, {
      allowed_groups: [1],
      restrict_public_groups: false,
      group_rates: { 1: 1.25 },
    })
  })

  it('限制开启时按 allowed_groups 初始化公开分组选择', async () => {
    const wrapper = await mountAndOpen(makeUser({
      restrict_public_groups: true,
      allowed_groups: [1, 3],
    }))

    expect((findRestrictionCheckbox(wrapper).element as HTMLInputElement).checked).toBe(true)
    expect((findPublicCheckbox(wrapper, 'Public A').element as HTMLInputElement).checked).toBe(false)
    expect((findPublicCheckbox(wrapper, 'Public B').element as HTMLInputElement).checked).toBe(true)
  })

  it('限制开启时可选择公开分组并与专属分组一起提交', async () => {
    const wrapper = await mountAndOpen(makeUser({
      restrict_public_groups: true,
      allowed_groups: [1, 2],
      group_rates: { 1: 1.25, 2: 1.5 },
    }))

    await findPublicCheckbox(wrapper, 'Public B').trigger('change')
    await save(wrapper)
    await flushPromises()

    expect(apiMocks.updateUser).toHaveBeenCalledWith(9, {
      allowed_groups: [1, 2, 3],
      restrict_public_groups: true,
      group_rates: { 1: 1.25, 2: 1.5 },
    })
  })

  it('关闭限制后恢复全部公开分组可用语义并从 allowed_groups 排除公开组', async () => {
    const wrapper = await mountAndOpen(makeUser({
      restrict_public_groups: true,
      allowed_groups: [1, 3],
    }))

    await findRestrictionCheckbox(wrapper).trigger('change')
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(2)

    await save(wrapper)
    await flushPromises()

    expect(apiMocks.updateUser).toHaveBeenCalledWith(9, {
      allowed_groups: [1],
      restrict_public_groups: false,
      group_rates: { 1: 1.25 },
    })
  })
})
