import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import ModelDefaultPricingDialog from '../ModelDefaultPricingDialog.vue'
import type { ModelDefaultPricingDetail } from '@/api/admin/modelPlaza'

const mocks = vi.hoisted(() => ({ get: vi.fn(), put: vi.fn(), post: vi.fn(), write: true }))
vi.mock('@/api/client', () => ({ apiClient: mocks }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ canAdmin: (permission: string) => !permission.endsWith('.write') || mocks.write }) }))
vi.mock('vue-i18n', async () => {
  const vue = await vi.importActual<typeof import('vue')>('vue')
  return { useI18n: () => ({ locale: vue.ref('zh'), t: (key: string) => key }) }
})
function pricing(exists = false, version = '1'): ModelDefaultPricingDetail {
  const prices = exists ? { billing_mode: 'token' as const, input_price: 0.000002, output_price: 0.000008 } : {}
  return {
    requested_model_id: 'vendor/new-v1', pricing_key: 'vendor/new-v1', matched_model_id: 'vendor/new-v1', match_type: exists ? 'exact' : 'none',
    source: exists ? 'admin_default' : 'unavailable', currency: 'USD', pricing_unit: 'per_token', version, loaded_version: version, refresh_interval_seconds: 3,
    has_admin_override: exists, has_exact_system_standard: false, effective_pricing_available: exists,
    system_baseline: { prices: {}, source: 'unavailable', matched_model_id: 'vendor/new-v1', match_type: 'none', has_exact_standard: false },
    admin_override: { ...prices }, effective_pricing: { ...prices }, editable_fields: [], supported_modes: ['token', 'per_request', 'image', 'video'],
  }
}
let wrappers: VueWrapper[] = []
function render(modelId = 'vendor/new-v1') {
  const wrapper = mount(ModelDefaultPricingDialog, {
    props: { show: true, modelId },
    global: { stubs: { BaseDialog: { props: ['show'], template: '<div v-if="show"><slot/><slot name="footer"/></div>' } } },
  })
  wrappers.push(wrapper)
  return wrapper
}
beforeEach(() => {
  vi.resetAllMocks(); mocks.write = true
  mocks.get.mockResolvedValue({ data: pricing() })
  mocks.put.mockResolvedValue({ data: pricing(true, '2') })
  mocks.post.mockResolvedValue({ data: pricing(false, '2') })
})
afterEach(() => { wrappers.forEach(wrapper => wrapper.unmount()); wrappers = [] })

describe('ModelDefaultPricingDialog', () => {
  it('starts an unknown ID with blank prices, preserves slashes and converts only token money', async () => {
    const wrapper = render()
    await flushPromises()
    expect(mocks.get).toHaveBeenCalledWith('/admin/model-plaza/pricing', expect.objectContaining({ params: { model_id: 'vendor/new-v1' } }))
    expect((wrapper.get('[data-testid="default-pricing-input_price"]').element as HTMLInputElement).value).toBe('')
    expect(wrapper.get('[data-testid="default-pricing-save"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-testid="default-pricing-input_price"]').setValue('2')
    await wrapper.get('[data-testid="default-pricing-output_price"]').setValue('8')
    await wrapper.get('[data-testid="default-pricing-save"]').trigger('click')
    await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('/admin/model-plaza/pricing', { model_id: 'vendor/new-v1', version: '1', patch: { billing_mode: 'token', input_price: 0.000002, output_price: 0.000008 } })
    expect(wrapper.emitted('saved')).toHaveLength(1)
    expect(wrapper.find('[data-testid="default-pricing-saved"]').exists()).toBe(true)
  })

  it('retains explicit zero and sends null only for the inheritance action', async () => {
    const original = pricing(true)
    original.admin_override.cache_write_price = 0.000003
    original.effective_pricing.cache_write_price = 0.000003
    mocks.get.mockResolvedValue({ data: original })
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="default-pricing-input_price"]').setValue('0')
    await wrapper.get('[data-testid="default-pricing-inherit-cache_write_price"]').trigger('click')
    await wrapper.get('[data-testid="default-pricing-save"]').trigger('click'); await flushPromises()
    expect(mocks.put).toHaveBeenCalledWith('/admin/model-plaza/pricing', { model_id: 'vendor/new-v1', version: '1', patch: { input_price: 0, cache_write_price: null } })
  })

  it('does not turn a cleared input into free pricing or silently reset it', async () => {
    mocks.get.mockResolvedValue({ data: pricing(true) })
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="default-pricing-input_price"]').setValue('')
    expect(wrapper.get('[data-testid="default-pricing-save"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="default-pricing-validation"]').exists()).toBe(true)
    expect(mocks.put).not.toHaveBeenCalled()
  })

  it('keeps a 409 draft, shows changed fields and requires explicit review before retrying', async () => {
    const newer = pricing(true, '2'); newer.admin_override.output_price = 0.000012; newer.effective_pricing.output_price = 0.000012
    mocks.get.mockResolvedValueOnce({ data: pricing(true) }).mockResolvedValueOnce({ data: newer })
    mocks.put.mockRejectedValueOnce({ status: 409, code: 'MODEL_DEFAULT_PRICING_VERSION_CONFLICT' })
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="default-pricing-input_price"]').setValue('3')
    await wrapper.get('[data-testid="default-pricing-save"]').trigger('click'); await flushPromises()
    expect(wrapper.find('[data-testid="default-pricing-conflict"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('output_price')
    expect((wrapper.get('[data-testid="default-pricing-input_price"]').element as HTMLInputElement).value).toBe('3')
    expect(mocks.put).toHaveBeenCalledTimes(1)
    await wrapper.get('[data-testid="default-pricing-accept-latest"]').trigger('click')
    expect((wrapper.get('[data-testid="default-pricing-output_price"]').element as HTMLInputElement).value).toBe('12')
    await wrapper.get('[data-testid="default-pricing-save"]').trigger('click'); await flushPromises()
    expect(mocks.put).toHaveBeenLastCalledWith('/admin/model-plaza/pricing', { model_id: 'vendor/new-v1', version: '2', patch: { input_price: 0.000003 } })
  })

  it('retains input on failure and does not emit a saved event', async () => {
    mocks.get.mockResolvedValue({ data: pricing(true) }); mocks.put.mockRejectedValue(new Error('offline'))
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="default-pricing-output_price"]').setValue('9')
    await wrapper.get('[data-testid="default-pricing-save"]').trigger('click'); await flushPromises()
    expect((wrapper.get('[data-testid="default-pricing-output_price"]').element as HTMLInputElement).value).toBe('9')
    expect(wrapper.emitted('saved')).toBeUndefined()
  })

  it.each([
    ['per_request', ['per_request_price']],
    ['image', ['image_price_1k', 'image_price_2k', 'image_price_4k']],
    ['video', ['video_price_480p', 'video_price_720p', 'video_price_1080p']],
  ])('uses the native money unit for %s', async (mode, fields) => {
    const wrapper = render(); await flushPromises()
    await wrapper.get('[data-testid="default-pricing-mode"]').setValue(mode)
    for (const field of fields) await wrapper.get(`[data-testid="default-pricing-${field}"]`).setValue('0.25')
    await wrapper.get('[data-testid="default-pricing-save"]').trigger('click'); await flushPromises()
    expect(mocks.put.mock.calls[0][1].patch).toEqual({ billing_mode: mode, ...Object.fromEntries(fields.map(field => [field, 0.25])) })
  })

  it('blocks removing the only standard and respects read-only admin permissions', async () => {
    mocks.get.mockResolvedValue({ data: pricing(true) })
    const wrapper = render(); await flushPromises()
    expect(wrapper.get('[data-testid="default-pricing-reset"]').attributes('disabled')).toBeDefined()
    mocks.write = false
    const readOnly = render(); await flushPromises()
    expect(readOnly.find('[data-testid="default-pricing-save"]').exists()).toBe(false)
    expect(readOnly.get('[data-testid="default-pricing-input_price"]').attributes('disabled')).toBeDefined()
  })
})
it('previews new-model cache fallback without persisting derived fields as overrides', async () => {
  const empty = pricing(); empty.cache_fallback_to_input = true
  mocks.get.mockResolvedValue({ data: empty })
  const wrapper = render(); await flushPromises()
  await wrapper.get('[data-testid="default-pricing-input_price"]').setValue('2')
  await wrapper.get('[data-testid="default-pricing-output_price"]').setValue('8')
  expect(wrapper.get('[data-testid="default-pricing-effective-cache_write_price"]').text()).toBe('2')
  expect(wrapper.get('[data-testid="default-pricing-effective-cache_read_price"]').text()).toBe('2')
  await wrapper.get('[data-testid="default-pricing-cache_write_price"]').setValue('0')
  expect(wrapper.get('[data-testid="default-pricing-effective-cache_write_1h_price"]').text()).toBe('0')
  expect(wrapper.get('[data-testid="default-pricing-effective-cache_read_price"]').text()).toBe('2')
  await wrapper.get('[data-testid="default-pricing-save"]').trigger('click'); await flushPromises()
  expect(mocks.put.mock.calls[0][1].patch).toEqual({ billing_mode: 'token', input_price: 0.000002, output_price: 0.000008, cache_write_price: 0 })
})

it('does not silently use a family estimate as one side of a new exact standard', async () => {
  const family = pricing()
  family.system_baseline = { prices: { billing_mode: 'token', input_price: 0.000002, output_price: 0.000008 }, source: 'fallback', matched_model_id: 'family-model', match_type: 'family', has_exact_standard: false }
  family.effective_pricing = { ...family.system_baseline.prices }
  mocks.get.mockResolvedValue({ data: family })
  const wrapper = render(); await flushPromises()
  await wrapper.get('[data-testid="default-pricing-input_price"]').setValue('3')
  expect(wrapper.get('[data-testid="default-pricing-save"]').attributes('disabled')).toBeDefined()
  expect(wrapper.text()).toContain('modelPlaza.defaultPricing.exactRequired')
  await wrapper.get('[data-testid="default-pricing-output_price"]').setValue('9')
  expect(wrapper.get('[data-testid="default-pricing-save"]').attributes('disabled')).toBeUndefined()
})

it('cancels an unsaved override without creating a mode-only manual standard', async () => {
  const inherited = pricing()
  inherited.system_baseline = { prices: { billing_mode: 'token', input_price: 0.000002, output_price: 0.000008 }, source: 'litellm', matched_model_id: 'vendor/new-v1', match_type: 'exact', has_exact_standard: true }
  inherited.effective_pricing = { ...inherited.system_baseline.prices }
  mocks.get.mockResolvedValue({ data: inherited })
  const wrapper = render(); await flushPromises()
  await wrapper.get('[data-testid="default-pricing-input_price"]').setValue('3')
  await wrapper.get('[data-testid="default-pricing-inherit-input_price"]').trigger('click')
  expect(wrapper.get('[data-testid="default-pricing-save"]').attributes('disabled')).toBeDefined()
  expect(mocks.put).not.toHaveBeenCalled()
})
