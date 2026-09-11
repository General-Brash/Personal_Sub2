import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { copyToClipboard, syncUpstreamModels, syncUpstreamModelsPreview, showWarning, showSuccess, showInfo } = vi.hoisted(() => ({
  copyToClipboard: vi.fn().mockResolvedValue(true),
  syncUpstreamModels: vi.fn(),
  syncUpstreamModelsPreview: vi.fn(),
  showWarning: vi.fn(),
  showSuccess: vi.fn(),
  showInfo: vi.fn()
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => (key === 'common.copy' ? '复制' : key)
    })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess,
    showInfo,
    showWarning
  })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard
  })
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: { syncUpstreamModels, syncUpstreamModelsPreview },
  syncUpstreamModels,
  syncUpstreamModelsPreview
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

function mountSelector() {
  return mount(ModelWhitelistSelector, {
    props: {
      modelValue: [],
      platform: 'openai'
    },
    global: {
      stubs: {
        ModelIcon: true
      }
    }
  })
}

function findModelRow(wrapper: ReturnType<typeof mountSelector>, modelId: string) {
  const row = wrapper
    .findAll('[data-testid="model-option"]')
    .find(candidate => candidate.text().includes(modelId))

  if (!row) {
    throw new Error(`Model row not found: ${modelId}`)
  }

  return row
}

describe('ModelWhitelistSelector', () => {
  beforeEach(() => {
    copyToClipboard.mockClear()
  })

  it('copies a model ID without selecting the model', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')

    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-5.6-sol')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-5.6-sol')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps the existing model selection behavior', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-5.6-sol')
    await row.get('[data-testid="select-model"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-5.6-sol']]])
    expect(copyToClipboard).not.toHaveBeenCalled()
  })

  it('selects and clears GPT Image 2.5 Flare/Sunburst with exact model IDs', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    await findModelRow(wrapper, 'gpt-image-2.5-flare').get('[data-testid="select-model"]').trigger('click')
    await wrapper.setProps({ modelValue: ['gpt-image-2.5-flare'] })
    await findModelRow(wrapper, 'gpt-image-2.5-sunburst').get('[data-testid="select-model"]').trigger('click')
    await wrapper.setProps({ modelValue: ['gpt-image-2.5-flare', 'gpt-image-2.5-sunburst'] })

    expect(wrapper.emitted('update:modelValue')).toEqual([
      [['gpt-image-2.5-flare']],
      [['gpt-image-2.5-flare', 'gpt-image-2.5-sunburst']]
    ])
    expect(copyToClipboard).not.toHaveBeenCalled()

    const clearButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.clearAllModels')
    expect(clearButton).toBeDefined()
    await clearButton!.trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([[]])
  })

  it('keeps copy behavior distinct for Image 2.5 options', async () => {
    const wrapper = mountSelector()
    await wrapper.get('div.cursor-pointer').trigger('click')

    const row = findModelRow(wrapper, 'gpt-image-2.5-sunburst')
    const copyButton = row.get('[data-testid="copy-model-id"]')
    expect(copyButton.attributes('aria-label')).toBe('复制 gpt-image-2.5-sunburst')

    await copyButton.trigger('click')
    await flushPromises()

    expect(copyToClipboard).toHaveBeenCalledWith('gpt-image-2.5-sunburst')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('warns when model IDs sync but capability metadata is incomplete', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['x-preview-f-free'],
      warnings: [
        {
          code: 'upstream_model_metadata_incomplete',
          message: 'Model IDs were synced, but capability metadata could not be updated.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['x-preview-f-free']]])
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataIncomplete')
    expect(showSuccess).not.toHaveBeenCalled()
  })

  it('shows success and a partial warning when some capabilities were saved', async () => {
    syncUpstreamModels.mockResolvedValue({
      models: ['gpt-6-astra', 'gpt-image-2'],
      warnings: [
        {
          code: 'upstream_model_metadata_partial',
          message: 'Some model capabilities were saved; remaining models are still incomplete.'
        }
      ]
    })
    const wrapper = mount(ModelWhitelistSelector, {
      props: {
        modelValue: [],
        platform: 'openai',
        accountId: 46
      },
      global: {
        stubs: {
          ModelIcon: true
        }
      }
    })

    const syncButton = wrapper
      .findAll('button')
      .find(button => button.text() === 'admin.accounts.syncUpstreamModels')
    expect(syncButton).toBeDefined()
    await syncButton!.trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:modelValue')).toEqual([[['gpt-6-astra', 'gpt-image-2']]])
    expect(showSuccess).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsSuccess')
    expect(showWarning).toHaveBeenCalledWith('admin.accounts.syncUpstreamModelsMetadataPartial')
  })

})
