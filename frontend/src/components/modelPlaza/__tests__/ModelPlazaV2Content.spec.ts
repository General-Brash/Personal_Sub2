import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import ModelPlazaV2Content from '../ModelPlazaV2Content.vue'
import type { ModelPlazaV2GroupChoice, ModelPlazaV2Model, ModelPlazaV2PriceQuote } from '@/api/modelPlaza'

vi.mock('@/api/client', () => ({ apiClient: { get: vi.fn(), put: vi.fn() } }))
vi.mock('@/stores/auth', () => ({ useAuthStore: () => ({ isAdmin: false }) }))
vi.mock('vue-i18n', async () => {
  const vue = await vi.importActual<typeof import('vue')>('vue')
  return { useI18n: () => ({ locale: vue.ref('zh') }) }
})

const quote = (input: number, output = input, extra: Partial<ModelPlazaV2PriceQuote> = {}): ModelPlazaV2PriceQuote => ({
  quote_version: 'v1', priced_at: '2026-09-23T00:00:00Z', pricing_unit: 'per_1m_tokens', currency: 'USD',
  input_per_million: input, output_per_million: output, effective_rate_multiplier: 1,
  dynamic_factor_status: 'not_configured', ...extra,
})
const choice = (id: number, name: string, price: ModelPlazaV2PriceQuote | null, extra: Partial<ModelPlazaV2GroupChoice> = {}): ModelPlazaV2GroupChoice => ({
  group_id: id, group_name: name, platform: 'openai', route_kind: 'direct', availability_state: 'eligible',
  schedulable: true, price_quote: price, ...extra,
})
const model = (id: string, choices: ModelPlazaV2GroupChoice[], platform = 'openai'): ModelPlazaV2Model => ({
  model_id: id, display_name: id, platform, availability_state: 'eligible', eligibility: 'eligible', user_group_choices: choices,
})
let wrappers: VueWrapper[] = []
function render(models: ModelPlazaV2Model[]) {
  const wrapper = mount(ModelPlazaV2Content, {
    props: { response: { models, generated_at: '2026-09-23T00:00:00Z' }, loading: false, error: false },
    global: { stubs: { ModelPlazaAdminPanel: true } },
  })
  wrappers.push(wrapper)
  return wrapper
}
afterEach(() => { wrappers.forEach((wrapper) => wrapper.unmount()); wrappers = [] })

function groupButtons(wrapper: VueWrapper) { return wrapper.findAll('[aria-label="可选分组"] button') }
function priceText(wrapper: VueWrapper) { return wrapper.get('[data-testid="plaza-v2-quote"]').text() }

describe('ModelPlazaV2Content', () => {
  it('uses 1/2/3 columns and renders only the selected complete quote', async () => {
    const wrapper = render([model('alpha', [choice(1, 'Same', quote(8, 8, { cache_read_per_million: 2, intervals: [{ tier_label: 'large', input_per_million: 10 }] })), choice(2, 'Same', quote(2, 2), { route_kind: 'proxy' })])])
    expect(wrapper.get('[data-testid="plaza-v2-grid"]').classes()).toEqual(expect.arrayContaining(['grid-cols-1', 'md:grid-cols-2', 'xl:grid-cols-3']))
    expect(wrapper.findAll('[data-testid="plaza-v2-quote"]')).toHaveLength(1)
    expect(groupButtons(wrapper)).toHaveLength(2)
    const card = wrapper.get('article')
    const header = card.get('[data-testid="plaza-v2-card-header"]')
    expect(header.get('h2').text()).toBe('alpha')
    expect(header.findAll('[aria-label="可选分组"] button')).toHaveLength(2)
    expect(header.get('span.rounded-full').text()).toBeTruthy()
    expect(groupButtons(wrapper)[0].attributes('aria-label')).toContain('#1')
    expect(groupButtons(wrapper)[0].attributes('aria-label')).toContain('direct')
    expect(groupButtons(wrapper)[1].attributes('aria-label')).toContain('#2')
    expect(groupButtons(wrapper)[1].attributes('aria-label')).toContain('proxy')
    expect(priceText(wrapper)).toContain('large')
    expect(priceText(wrapper)).toContain('缓存读取')
    await groupButtons(wrapper)[1].trigger('click')
    expect(groupButtons(wrapper)[1].attributes('aria-pressed')).toBe('true')
    expect(priceText(wrapper)).not.toContain('large')
    expect(priceText(wrapper)).toContain('proxy')
  })

  it('compares representative totals with peak, time and dynamic factors, not base prices', () => {
    const wrapper = render([model('compare', [
      choice(1, 'base-cheap', quote(1, 1, { peak_rate_multiplier: 3, channel_time_multiplier: 2, dynamic_factor_status: 'available', dynamic_factor: { factor: 2, source: 'tier' } })),
      choice(2, 'effective-cheap', quote(3, 3)),
    ])])
    expect(priceText(wrapper)).toContain('effective-cheap')
    expect(wrapper.text()).toContain('样例估算最低')
  })

  it('compares per-request quotes only to per-request quotes, honoring the effective independent image rate', () => {
    const wrapper = render([model('image', [
      choice(1, 'base-low', quote(0, 0, { pricing_unit: 'per_request', per_request_price: 1, image_rate_independent: true, image_rate_multiplier: 5, effective_rate_multiplier: 5 })),
      choice(2, 'total-low', quote(0, 0, { pricing_unit: 'per_request', per_request_price: 2, effective_rate_multiplier: 1 })),
    ])])
    expect(priceText(wrapper)).toContain('total-low')
    expect(wrapper.text()).toContain('样例估算最低')
  })

  it('conservatively falls back for unknown, conditional, tiered, mixed units and unknown dynamics', () => {
    const variants = [
      quote(1, 1, { unknown_fields: ['pricing'] }),
      quote(1, 1, { price_conditions: [{ pattern: '*', pricing_unit: 'per_1m_tokens' }] }),
      quote(1, 1, { intervals: [{ min_tokens: 0 }] }),
      quote(1, 1, { pricing_unit: 'per_request', per_request_price: 0.01 }),
      quote(1, 1, { dynamic_factor_status: 'unknown' }),
      quote(1, 1, { effective_rate_multiplier: null }),
    ]
    for (const variant of variants) {
      const wrapper = render([model('fallback', [choice(1, 'known', quote(5)), choice(2, 'uncertain', variant)])])
      expect(priceText(wrapper)).toContain('known')
      expect(wrapper.text()).toContain('谨慎展示')
      wrapper.unmount()
    }
  })

  it('filters by independent image multiplier across choices of the same group and keeps manual choice until invalid', async () => {
    const wrapper = render([model('image', [
      choice(1, 'Image', quote(2, 2, { image_rate_independent: true, image_rate_multiplier: 4, effective_rate_multiplier: 4 })),
      choice(1, 'Image', quote(3, 3, { effective_rate_multiplier: 2 }), { route_kind: 'proxy' }),
      choice(2, 'Other', quote(1, 1)),
    ])])
    await groupButtons(wrapper)[1].trigger('click')
    expect(priceText(wrapper)).toContain('proxy')
    await wrapper.get('input[type="search"]').setValue('image')
    expect(priceText(wrapper)).toContain('proxy')
    const filter = wrapper.get('[data-testid="plaza-v2-filters"]')
    await filter.findAll('button').find((button) => button.text() === '4x')!.trigger('click')
    expect(groupButtons(wrapper)).toHaveLength(1)
    expect(priceText(wrapper)).toContain('图片独立倍率 ×4')
    // Clear rate to restore all three choices.
    await filter.findAll('button').find((button) => button.text() === '全部' && button.element.parentElement?.textContent?.includes('倍率'))!.trigger('click')
    expect(groupButtons(wrapper)).toHaveLength(3)
  })

  it('shows an unknown quote rather than a fabricated zero for a catalog model without choices', () => {
    const wrapper = render([model('catalog', [])])
    expect(wrapper.findAll('article')).toHaveLength(1)
    expect(wrapper.text()).toContain('无可选分组')
    expect(wrapper.text()).toContain('报价未知')
    expect(wrapper.find('[data-testid="plaza-v2-quote"]').exists()).toBe(false)
  })

  it('isolates selection for repeated model ids on different platforms and refreshes invalid choices', async () => {
    const first = model('same-id', [choice(1, 'A', quote(3)), choice(2, 'B', quote(1))])
    const second = model('same-id', [choice(3, 'C', quote(7), { platform: 'anthropic' })], 'anthropic')
    const wrapper = render([first, second])
    const cards = wrapper.findAll('article')
    await cards[0].findAll('[aria-label="可选分组"] button')[0].trigger('click')
    expect(cards[0].get('[data-testid="plaza-v2-quote"]').text()).toContain('A')
    expect(cards[1].get('[data-testid="plaza-v2-quote"]').text()).toContain('C')
    await wrapper.setProps({ response: { models: [model('same-id', [choice(2, 'B', quote(1))]), second], generated_at: '2026-09-23T00:00:00Z' } })
    expect(wrapper.findAll('article')[0].get('[data-testid="plaza-v2-quote"]').text()).toContain('B')
  })

  it('ignores only backend non-sample unknown cache fields when comparing quotes', () => {
    const wrapper = render([model('cache-unknown', [
      choice(1, 'costly', quote(5, 5, { unknown_fields: ['cache_write_per_million', 'cache_read_per_million'] })),
      choice(2, 'cheap', quote(1, 1, { unknown_fields: ['cache_write_per_million', 'cache_read_per_million'] })),
    ])])
    expect(priceText(wrapper)).toContain('cheap')
    expect(wrapper.text()).toContain('样例估算最低')
  })

  it('compares backend per-request unknown input/output/cache, but not missing sample prices or factors', () => {
    const perRequest = render([model('requests', [
      choice(1, 'costly', quote(0, 0, { pricing_unit: 'per_request', per_request_price: 5,
        input_per_million: null, output_per_million: null,
        unknown_fields: ['input_per_million', 'output_per_million', 'cache_write_per_million', 'cache_read_per_million'] })),
      choice(2, 'cheap', quote(0, 0, { pricing_unit: 'per_request', per_request_price: 1,
        input_per_million: null, output_per_million: null,
        unknown_fields: ['input_per_million', 'output_per_million', 'cache_write_per_million', 'cache_read_per_million'] })),
    ])])
    expect(priceText(perRequest)).toContain('cheap')
    expect(perRequest.text()).toContain('样例估算最低')

    for (const uncertain of [
      quote(1, 1, { input_per_million: null, unknown_fields: ['input_per_million', 'cache_read_per_million'] }),
      quote(1, 1, { pricing_unit: 'per_request', per_request_price: null,
        unknown_fields: ['per_request_price', 'input_per_million', 'output_per_million'] }),
      quote(1, 1, { unknown_fields: ['effective_rate_multiplier', 'cache_read_per_million'] }),
    ]) {
      const wrapper = render([model('missing', [choice(1, 'known', quote(5)), choice(2, 'uncertain', uncertain)])])
      expect(priceText(wrapper)).toContain('known')
      expect(wrapper.text()).toContain('谨慎展示')
    }
  })

  it('invalidates ambiguous duplicate route selection on reorder and deletion, with accessible disambiguation', async () => {
    const a = choice(1, 'Duplicate', quote(4))
    const b = choice(1, 'Duplicate', quote(8))
    const other = choice(2, 'Other', quote(1))
    const wrapper = render([model('duplicates', [a, b, other])])
    expect(groupButtons(wrapper)[1].attributes('aria-label')).toContain('重复路由第 2 项，共 2 项')
    await groupButtons(wrapper)[1].trigger('click')
    expect(priceText(wrapper)).toContain('$8')
    expect(groupButtons(wrapper)[1].attributes('aria-pressed')).toBe('true')
    await wrapper.setProps({ response: { models: [model('duplicates', [b, a, other])], generated_at: '2026-09-23T00:01:00Z' } })
    expect(groupButtons(wrapper)[1].attributes('aria-pressed')).toBe('false')
    expect(groupButtons(wrapper)[2].attributes('aria-pressed')).toBe('true')
    expect(priceText(wrapper)).toContain('Other')
    expect(wrapper.get('[role="status"]').text()).toContain('重新选择')
    await groupButtons(wrapper)[0].trigger('click')
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    await wrapper.setProps({ response: { models: [model('duplicates', [b, other])], generated_at: '2026-09-23T00:02:00Z' } })
    expect(groupButtons(wrapper)[1].attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[role="status"]').text()).toContain('重新选择')
  })
})
