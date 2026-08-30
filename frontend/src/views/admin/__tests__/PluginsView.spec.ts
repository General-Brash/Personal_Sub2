import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import PluginsView from '../PluginsView.vue'

const {
  listPlugins,
  uploadPlugin,
  enablePlugin,
  savePluginConfig,
  getPluginConfig,
  testPlugin,
  createUISession,
  stepUpRun,
} = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  uploadPlugin: vi.fn(),
  enablePlugin: vi.fn(),
  savePluginConfig: vi.fn(),
  getPluginConfig: vi.fn(),
  testPlugin: vi.fn(),
  createUISession: vi.fn(),
  stepUpRun: vi.fn((action: () => Promise<unknown>) => action()),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    plugins: {
      list: listPlugins,
      upload: uploadPlugin,
      enable: enablePlugin,
      disable: vi.fn(),
      remove: vi.fn(),
      getConfig: getPluginConfig,
      saveConfig: savePluginConfig,
      test: testPlugin,
      createUISession,
    },
  },
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
  }),
}))

vi.mock('@/composables/useStepUp', () => ({
  useStepUp: () => ({ run: stepUpRun }),
  isStepUpBlocked: () => false,
  isStepUpCancelled: () => false,
  stepUpBlockReason: () => '',
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key }),
}))

const plugin = {
  id: 7,
  plugin_key: 'local.test.transport',
  name: 'Test Transport',
  version: '1.0.0',
  description: '',
  author: 'test',
  manifest: {
    schema_version: 1,
    id: 'local.test.transport',
    name: 'Test Transport',
    version: '1.0.0',
    requires: {
      sub2api: '>=0.1.0',
      plugin_protocol: 1,
      transport_api: 1,
      ui_bridge: 1,
    },
    capabilities: [],
    ui: { entrypoint: 'ui/index.html' },
  },
  binary_sha256: 'a'.repeat(64),
  signature_status: 'trusted' as const,
  state: 'disabled' as const,
  last_error: '',
  installed_at: '2026-08-22T00:00:00Z',
  updated_at: '2026-08-22T00:00:00Z',
  bindings: [
    {
      id: 1,
      plugin_id: 7,
      capability: 'openai.oauth.outbound_transport.v1',
      platform: 'openai',
      account_type: 'oauth',
      enabled: false,
      rollout_percent: 100,
    },
  ],
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
    ui_bridge: 1,
  },
  runtime_healthy: false,
  runtime_message: '',
}

const secondPlugin = {
  ...plugin,
  id: 8,
  plugin_key: 'local.second.transport',
  name: 'Second Transport',
  bindings: plugin.bindings.map((binding) => ({ ...binding, plugin_id: 8 })),
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const sessionFor = (token: string) => ({
  url: `/api/v1/plugin-ui/${token}/index.html#bridge_token=${token}`,
  bridge_token: token,
  ui_bridge_version: 1,
  expires_at: '2026-08-22T01:00:00Z',
})

function mountView() {
  return mount(PluginsView, {
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        BaseDialog: {
          name: 'BaseDialog',
          props: { show: Boolean, title: String, width: String },
          emits: ['close'],
          template: '<div v-if="show"><slot /></div>',
        },
        Icon: true,
        TotpStepUpDialog: true,
      },
    },
  })
}

describe('管理员插件页二次验证', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    stepUpRun.mockImplementation((action: () => Promise<unknown>) => action())
    listPlugins.mockResolvedValue([plugin, secondPlugin])
    uploadPlugin.mockResolvedValue(plugin)
    enablePlugin.mockResolvedValue(plugin)
    savePluginConfig.mockResolvedValue({ enabled: true })
    getPluginConfig.mockResolvedValue({ enabled: true })
    testPlugin.mockResolvedValue({ success: true, message: 'ok', latency_ms: 1 })
    createUISession.mockResolvedValue({
      url: '/api/v1/plugin-ui/token/index.html#bridge_token=bridge',
      bridge_token: 'bridge',
      ui_bridge_version: 1,
      expires_at: '2026-08-22T01:00:00Z',
    })
  })

  it('启用插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()

    const button = wrapper.findAll('button').find((item) => item.text().includes('admin.plugins.enable'))
    expect(button).toBeDefined()
    await button!.trigger('click')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(enablePlugin).toHaveBeenCalledWith(7, 100, false)
  })

  it('上传插件通过 step-up 控制器执行', async () => {
    const wrapper = mountView()
    await flushPromises()
    const input = wrapper.get('input[type="file"]')
    Object.defineProperty(input.element, 'files', {
      configurable: true,
      value: [new File(['plugin'], 'transport.s2plugin', { type: 'application/zip' })],
    })

    await input.trigger('change')
    await flushPromises()

    expect(stepUpRun).toHaveBeenCalledTimes(1)
    expect(uploadPlugin).toHaveBeenCalledTimes(1)
  })

  it('ignores a stale UI session response after switching plugins', async () => {
    const first = deferred<ReturnType<typeof sessionFor>>()
    const second = deferred<ReturnType<typeof sessionFor>>()
    createUISession
      .mockImplementationOnce(() => first.promise)
      .mockImplementationOnce(() => second.promise)

    const wrapper = mountView()
    await flushPromises()
    const configureButtons = wrapper
      .findAll('button')
      .filter((button) => button.text().includes('admin.plugins.configure'))

    configureButtons[0] && void configureButtons[0].trigger('click')
    await flushPromises()
    configureButtons[1] && void configureButtons[1].trigger('click')

    second.resolve(sessionFor('second'))
    await flushPromises()
    expect(wrapper.find('iframe').attributes('src')).toBe(sessionFor('second').url)

    first.resolve(sessionFor('first'))
    await flushPromises()

    expect(wrapper.find('iframe').attributes('src')).toBe(sessionFor('second').url)
    expect(wrapper.text()).not.toContain('admin.plugins.uiUnavailable')
  })

  async function openConfiguredFrame() {
    const wrapper = mountView()
    await flushPromises()
    const configureButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.plugins.configure'))
    expect(configureButton).toBeDefined()
    await configureButton!.trigger('click')
    await flushPromises()

    const frame = wrapper.get('iframe')
    const sourceWindow = { postMessage: vi.fn() } as unknown as Window
    Object.defineProperty(frame.element, 'contentWindow', {
      configurable: true,
      value: sourceWindow,
    })
    await frame.trigger('load')
    return { wrapper, frame, sourceWindow }
  }

  function dispatchBridgeRequest(
    frame: ReturnType<ReturnType<typeof mountView>['get']>,
    type: 'config.load' | 'config.save' | 'config.test',
  ): void {
    window.dispatchEvent(new MessageEvent('message', {
      data: {
        source: 'sub2api-plugin-ui',
        bridge_token: 'bridge',
        type,
        request_id: 'reused-request-id',
        ...(type === 'config.save' ? { config: { enabled: true } } : {}),
      },
      origin: 'null',
      source: frame.element.contentWindow,
    }))
  }

  function dispatchBridgeReady(
    frame: ReturnType<ReturnType<typeof mountView>['get']>,
  ): void {
    window.dispatchEvent(new MessageEvent('message', {
      data: {
        source: 'sub2api-plugin-ui',
        bridge_token: 'bridge',
        type: 'sub2api.plugin.ready',
      },
      origin: 'null',
      source: frame.element.contentWindow,
    }))
  }

  it('accepts the initial bridge handshake and config.load before iframe load', async () => {
    const wrapper = mountView()
    await flushPromises()
    const configureButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.plugins.configure'))
    expect(configureButton).toBeDefined()
    await configureButton!.trigger('click')
    await flushPromises()

    const frame = wrapper.get('iframe')
    const sourceWindow = { postMessage: vi.fn() } as unknown as Window
    Object.defineProperty(frame.element, 'contentWindow', {
      configurable: true,
      value: sourceWindow,
    })

    dispatchBridgeReady(frame)
    dispatchBridgeRequest(frame, 'config.load')
    await flushPromises()

    expect(getPluginConfig).toHaveBeenCalledWith(7)
    expect(sourceWindow.postMessage).toHaveBeenCalledTimes(1)
    expect(sourceWindow.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({
        source: 'sub2api-plugin-host',
        bridge_token: 'bridge',
        type: 'config.load.result',
        request_id: 'reused-request-id',
        config: { enabled: true },
      }),
      '*',
    )

    // The load event for the first document must not invalidate its already
    // accepted pre-load request.
    await frame.trigger('load')
    expect(sourceWindow.postMessage).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('treats a new ready handshake as a document boundary before reload load', async () => {
    const stale = deferred<{ stale: boolean }>()
    const fresh = deferred<{ fresh: boolean }>()
    getPluginConfig
      .mockImplementationOnce(() => stale.promise)
      .mockImplementationOnce(() => fresh.promise)

    const { wrapper, frame, sourceWindow } = await openConfiguredFrame()
    dispatchBridgeReady(frame)
    dispatchBridgeRequest(frame, 'config.load')
    await flushPromises()

    // A reloaded plugin can send ready before the parent receives its load.
    dispatchBridgeReady(frame)
    dispatchBridgeRequest(frame, 'config.load')
    await flushPromises()
    await frame.trigger('load')

    fresh.resolve({ fresh: true })
    await flushPromises()
    expect(sourceWindow.postMessage).toHaveBeenCalledTimes(1)
    expect(sourceWindow.postMessage).toHaveBeenCalledWith(
      expect.objectContaining({ config: { fresh: true } }),
      '*',
    )

    stale.resolve({ stale: true })
    await flushPromises()
    expect(sourceWindow.postMessage).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it.each([
    ['config.load', getPluginConfig, { stale: true }, { fresh: true }],
    ['config.save', savePluginConfig, { stale: true }, { fresh: true }],
    ['config.test', testPlugin, { success: false, message: 'stale', latency_ms: 1 }, { success: true, message: 'fresh', latency_ms: 2 }],
  ] as const)(
    'does not deliver a stale %s response after iframe reload and request_id reuse',
    async (type, operation, staleValue, freshValue) => {
      const stale = deferred<typeof staleValue>()
      const fresh = deferred<typeof freshValue>()
      operation
        .mockImplementationOnce(() => stale.promise)
        .mockImplementationOnce(() => fresh.promise)

      const { wrapper, frame, sourceWindow } = await openConfiguredFrame()
      const postMessage = sourceWindow.postMessage as unknown as ReturnType<typeof vi.fn>

      dispatchBridgeRequest(frame, type)
      await flushPromises()

      await frame.trigger('load')
      dispatchBridgeRequest(frame, type)
      await flushPromises()

      fresh.resolve(freshValue)
      await flushPromises()
      expect(postMessage).toHaveBeenCalledTimes(1)
      expect(postMessage.mock.calls[0]?.[0]).toMatchObject({
        request_id: 'reused-request-id',
        type: `${type}.result`,
      })
      if (type === 'config.load' || type === 'config.save') {
        expect(postMessage.mock.calls[0]?.[0]).toMatchObject({ config: freshValue })
      } else {
        expect(postMessage.mock.calls[0]?.[0]).toMatchObject({ result: freshValue })
      }

      stale.resolve(staleValue)
      await flushPromises()
      expect(postMessage).toHaveBeenCalledTimes(1)

      wrapper.unmount()
    },
  )

  it('ignores a stale failure after closing and reopening the configuration dialog', async () => {
    const stale = deferred<ReturnType<typeof sessionFor>>()
    const current = deferred<ReturnType<typeof sessionFor>>()
    createUISession
      .mockImplementationOnce(() => stale.promise)
      .mockImplementationOnce(() => current.promise)

    const wrapper = mountView()
    await flushPromises()
    const configureButton = wrapper
      .findAll('button')
      .find((button) => button.text().includes('admin.plugins.configure'))
    expect(configureButton).toBeDefined()

    void configureButton!.trigger('click')
    await flushPromises()
    wrapper.findComponent({ name: 'BaseDialog' }).vm.$emit('close')
    await flushPromises()
    void configureButton!.trigger('click')

    stale.reject(new Error('stale session failed'))
    await flushPromises()

    expect(wrapper.text()).not.toContain('admin.plugins.uiUnavailable')

    current.resolve(sessionFor('current'))
    await flushPromises()

    expect(wrapper.find('iframe').attributes('src')).toBe(sessionFor('current').url)
  })
})
