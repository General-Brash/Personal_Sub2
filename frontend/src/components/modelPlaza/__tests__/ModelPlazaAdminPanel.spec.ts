import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ModelPlazaAdminPanel from '../ModelPlazaAdminPanel.vue'

const client = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn() }))
vi.mock('@/api/client', () => ({ apiClient: client }))
vi.mock('vue-i18n', async () => {
  const vue = await vi.importActual<typeof import('vue')>('vue')
  return { useI18n: () => ({ locale: vue.ref('zh'), t: (k: string) => k }) }
})

const settings = () => ({
  models: [
    { key: 'anthropic:claude-sonnet', model_id: 'claude-sonnet', display_name: 'Claude Sonnet', platform: 'anthropic', availability_state: 'eligible', hidden: false, pinned: false, sort_order: 0 },
    { key: 'openai:gpt-4o', model_id: 'gpt-4o', display_name: 'GPT-4o', platform: 'openai', availability_state: 'temporarily_unavailable', hidden: false, pinned: false, sort_order: 0 },
  ],
  hide_no_account: true,
  version: 'v1',
})

let wrappers: VueWrapper[] = []
function render() {
  const wrapper = mount(ModelPlazaAdminPanel, { global: { stubs: { RouterLink: true } } })
  wrappers.push(wrapper)
  return wrapper
}

beforeEach(() => {
  vi.resetAllMocks()
  client.get.mockResolvedValue({ data: settings() })
  client.put.mockResolvedValue({ data: { ...settings(), version: 'v2' } })
})
afterEach(() => {
  wrappers.forEach((w) => w.unmount())
  wrappers = []
})

describe('ModelPlazaAdminPanel', () => {
  it('loads the admin catalog only on first expand', async () => {
    const wrapper = render()
    expect(client.get).not.toHaveBeenCalled()
    await wrapper.get('[data-testid="plaza-admin-toggle"]').trigger('click')
    await flushPromises()
    expect(client.get).toHaveBeenCalledWith('/admin/model-plaza', expect.anything())
    expect(wrapper.text()).toContain('Claude Sonnet')
    expect(wrapper.text()).toContain('GPT-4o')
  })

  it('saves only changed overrides with the optimistic-lock version', async () => {
    const wrapper = render()
    await wrapper.get('[data-testid="plaza-admin-toggle"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="plaza-admin-hidden-claude-sonnet"]').setValue(true)
    await wrapper.get('[data-testid="plaza-admin-save"]').trigger('click')
    await flushPromises()
    expect(client.put).toHaveBeenCalledWith('/admin/model-plaza', {
      overrides: { 'anthropic:claude-sonnet': { hidden: true, pinned: false, sort_order: 0 } },
      hide_no_account: true,
      version: 'v1',
    })
    expect(wrapper.text()).toContain('已保存')
  })
})
