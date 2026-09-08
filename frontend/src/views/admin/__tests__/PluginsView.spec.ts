import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PluginsView from '../PluginsView.vue'

const { listPlugins, getPluginConfig, createUISession } = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  getPluginConfig: vi.fn(),
  createUISession: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    plugins: {
      list: listPlugins,
      upload: vi.fn(),
      enable: vi.fn(),
      disable: vi.fn(),
      remove: vi.fn(),
      getConfig: getPluginConfig,
      saveConfig: vi.fn(),
      test: vi.fn(),
      createUISession
    }
  }
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn()
  })
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: (action: () => unknown) => action() }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => ''
}))

vi.mock('vue-i18n', async () => ({
  ...(await vi.importActual<typeof import('vue-i18n')>('vue-i18n')),
  useI18n: () => ({ t: (key: string) => key })
}))

const makePlugin = (id: number) => ({
  id,
  plugin_key: `local.test.${id}`,
  name: `Test Plugin ${id}`,
  version: '1.0.0',
  description: '',
  author: 'test',
  manifest: {
    schema_version: 1,
    id: `local.test.${id}`,
    name: `Test Plugin ${id}`,
    version: '1.0.0',
    requires: {
      sub2api: '>=0.1.0',
      plugin_protocol: 1,
      transport_api: 1,
      ui_bridge: 1
    },
    capabilities: [],
    ui: { entrypoint: 'ui/index.html' }
  },
  binary_sha256: 'a'.repeat(64),
  signature_status: 'trusted' as const,
  state: 'disabled' as const,
  last_error: '',
  installed_at: '2026-08-22T00:00:00Z',
  updated_at: '2026-08-22T00:00:00Z',
  bindings: [],
  compatibility: {
    compatible: true,
    tested: true,
    status: 'compatible' as const,
    message: '',
    current_sub2api_version: '0.1.0',
    required_sub2api_version: '>=0.1.0',
    recommended_sub2api_version: '0.1.0',
    plugin_protocol: 1,
    transport_api: 1,
    ui_bridge: 1
  },
  runtime_healthy: false,
  runtime_message: ''
})

function mountView() {
  return mount(PluginsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: { template: '<div><slot /></div>' },
        Icon: true,
        TotpStepUpDialog: true
      }
    }
  })
}

describe('PluginsView configuration session binding', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    createUISession.mockReset()
    getPluginConfig.mockReset()
    listPlugins.mockResolvedValue([makePlugin(1), makePlugin(2)])
  })

  it('does not let a slower stale UI session replace the currently selected plugin', async () => {
    let resolveFirst!: (value: unknown) => void
    let resolveSecond!: (value: unknown) => void
    createUISession
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveSecond = resolve }))

    const wrapper = mountView()
    await flushPromises()
    const configureButtons = wrapper
      .findAll('button')
      .filter((button) => button.text().includes('admin.plugins.configure'))

    await configureButtons[0].trigger('click')
    await configureButtons[1].trigger('click')

    resolveSecond({
      url: '/api/v1/plugin-ui/plugin-two/index.html#bridge_token=bridge-two',
      bridge_token: 'bridge-two',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z'
    })
    await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toContain('plugin-two')

    resolveFirst({
      url: '/api/v1/plugin-ui/plugin-one/index.html#bridge_token=bridge-one',
      bridge_token: 'bridge-one',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z'
    })
    await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toContain('plugin-two')
    wrapper.unmount()
  })

  it('does not send an old config result to a new session reusing the request id', async () => {
    let resolveFirst!: (value: unknown) => void
    let resolveSecond!: (value: unknown) => void
    getPluginConfig
      .mockImplementationOnce(() => new Promise((resolve) => { resolveFirst = resolve }))
      .mockImplementationOnce(() => new Promise((resolve) => { resolveSecond = resolve }))
    createUISession.mockImplementation((id: number) => Promise.resolve({
      url: `/api/v1/plugin-ui/plugin-${id}/index.html#bridge_token=bridge-${id}`,
      bridge_token: `bridge-${id}`,
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z'
    }))

    const wrapper = mountView()
    await flushPromises()
    const configureButtons = wrapper
      .findAll('button')
      .filter((button) => button.text().includes('admin.plugins.configure'))

    const requestConfig = (frame: HTMLIFrameElement, id: number) => {
      const source = { postMessage: vi.fn() }
      Object.defineProperty(frame, 'contentWindow', { configurable: true, value: source })
      const event = new MessageEvent('message', {
        origin: 'null',
        data: {
          source: 'sub2api-plugin-ui',
          bridge_token: `bridge-${id}`,
          type: 'config.load',
          request_id: 'shared-request'
        }
      })
      Object.defineProperty(event, 'source', { value: source })
      window.dispatchEvent(event)
      return source
    }

    await configureButtons[0].trigger('click')
    await flushPromises()
    requestConfig(wrapper.get('iframe').element as HTMLIFrameElement, 1)

    await configureButtons[1].trigger('click')
    await flushPromises()
    const secondSource = requestConfig(wrapper.get('iframe').element as HTMLIFrameElement, 2)
    expect(getPluginConfig).toHaveBeenNthCalledWith(1, 1)
    expect(getPluginConfig).toHaveBeenNthCalledWith(2, 2)

    resolveFirst({ owner: 'plugin-one' })
    await flushPromises()
    expect(secondSource.postMessage).not.toHaveBeenCalled()

    resolveSecond({ owner: 'plugin-two' })
    await flushPromises()
    expect(secondSource.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ bridge_token: 'bridge-2', config: { owner: 'plugin-two' } }),
      '*'
    )
    wrapper.unmount()
  })

})
