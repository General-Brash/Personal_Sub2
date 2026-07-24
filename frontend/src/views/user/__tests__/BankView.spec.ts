import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount, type VueWrapper } from '@vue/test-utils'

import type { BankPolicy, BankStatus } from '@/api/bank'
import BankView from '../BankView.vue'

enableAutoUnmount(afterEach)

afterEach(() => {
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const {
  authState,
  appState,
  englishBankLabels,
  exchangePermanentForTemporary,
  getBankLedger,
  getBankSettings,
  getBankStatus,
  requestBankAdvance,
  repayBankDebt,
  refreshUser,
  showError,
  showSuccess,
  updateBankSettings,
} = vi.hoisted(() => ({
  authState: { isAdmin: false, refreshUser: vi.fn() },
  appState: { cachedPublicSettings: null as null | Record<string, boolean> },
  englishBankLabels: {
    'bank.exchange.amount': 'Permanent credit to use',
    'bank.exchange.preview': 'Estimated credit received',
  },
  exchangePermanentForTemporary: vi.fn(),
  getBankLedger: vi.fn(),
  getBankSettings: vi.fn(),
  getBankStatus: vi.fn(),
  requestBankAdvance: vi.fn(),
  repayBankDebt: vi.fn(),
  refreshUser: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  updateBankSettings: vi.fn(),
}))

vi.mock('@/api/bank', () => ({
  exchangePermanentForTemporary,
  getBankLedger,
  getBankSettings,
  getBankStatus,
  requestBankAdvance,
  repayBankDebt,
  updateBankSettings,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ ...appState, showError, showSuccess }),
}))

vi.mock('@/stores/auth', () => ({
  useAuthStore: () => authState,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'zh' },
      t: (key: string, values?: Record<string, string | number>) => {
        const englishLabel = (englishBankLabels as Record<string, string>)[key]
        return englishLabel ?? [key, ...Object.values(values ?? {})].join(':')
      },
    }),
  }
})

const policy: BankPolicy = {
  advance_min_amount: '5.00000000',
  advance_max_amount: '20.00000000',
  debt_grace_days: 3,
  debt_conversion_ratio: '1.25000000',
  exchange_rate: '2.00000000',
  unused_advance_debt_reduction_ratio: '0.75000000',
  early_repay_temporary_ratio: '1.00000000',
  early_repay_permanent_ratio: '2.00000000',
}

const baseStatus = (overrides: Partial<BankStatus> = {}): BankStatus => ({
  permanent_balance: '12.34567890',
  temporary_credit_available: '3.50000000',
  temporary_credit_earliest_expires_at: '2026-07-19T16:00:00Z',
  temporary_debt: '0.00000000',
  temporary_debt_due_at: null,
  active_advance: null,
  policy,
  ledger: [
    {
      id: 1,
      operation: 'exchange',
      permanent_delta: '-1.25000000',
      temporary_delta: '2.50000000',
      debt_delta: '0.00000000',
      debt_before: '0.00000000',
      debt_after: '0.00000000',
      created_at: '2026-07-19T08:00:00Z',
    },
  ],
  ...overrides,
})

const ledgerItem = (id: number) => ({
  id,
  operation: 'exchange',
  permanent_delta: '-1.00000000',
  temporary_delta: '2.00000000',
  debt_delta: '0.00000000',
  debt_before: '0.00000000',
  debt_after: '0.00000000',
  created_at: `2026-07-19T08:${String(id).padStart(2, '0')}:00Z`,
})

const mountView = async (attachTo?: Element) => {
  const wrapper = mount(BankView, {
    ...(attachTo ? { attachTo } : {}),
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: { props: ['name'], template: '<i :data-icon="name" />' },
        RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
        BaseDialog: {
          props: ['show', 'title'],
          template: '<section v-if="show" data-test="dialog"><slot /><slot name="footer" /></section>',
        },
      },
    },
  })
  await flushPromises()
  return wrapper
}

const showExchangeMode = async (wrapper: VueWrapper) => {
  await wrapper.get('[data-test="bank-mode-exchange"]').trigger('click')
}

const showRepayMode = async (wrapper: VueWrapper) => {
  await wrapper.get('[data-test="bank-mode-repay"]').trigger('click')
}

describe('BankView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-19T08:00:00.000Z'))
    authState.isAdmin = false
    authState.refreshUser = refreshUser
    appState.cachedPublicSettings = null
    localStorage.clear()
    exchangePermanentForTemporary.mockReset()
    getBankLedger.mockReset()
    getBankSettings.mockReset()
    getBankStatus.mockReset()
    requestBankAdvance.mockReset()
    repayBankDebt.mockReset()
    refreshUser.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    updateBankSettings.mockReset()
    getBankStatus.mockResolvedValue(baseStatus())
    getBankLedger.mockResolvedValue({
      items: baseStatus().ledger,
      total: 1,
      page: 1,
      page_size: 5,
      pages: 1,
    })
    getBankSettings.mockResolvedValue(policy)
    requestBankAdvance.mockResolvedValue({ amount: '5.00000000' })
    repayBankDebt.mockResolvedValue({ source: 'temporary', credit_spent: '1.00000000', debt_reduced: '1.00000000' })
    refreshUser.mockResolvedValue(undefined)
    exchangePermanentForTemporary.mockResolvedValue({ permanent_spent: '1.00000000' })
    updateBankSettings.mockResolvedValue(policy)
  })

  it('renders all visible balances and ledger deltas with exactly two decimals', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({
      permanent_balance: '12.3',
      temporary_credit_available: '3.5',
      temporary_debt: '1',
    }))

    const wrapper = await mountView()

    expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('12.30')
    expect(wrapper.get('[data-test="temporary-balance"]').text()).toBe('3.50')
    expect(wrapper.get('[data-test="temporary-debt"]').text()).toBe('1.00')
    expect(wrapper.get('[data-test="ledger-row"]').text()).toContain('-1.25')
    expect(wrapper.get('[data-test="ledger-row"]').text()).toContain('+2.50')
  })

  it('renders exactly five ledger rows per page and keeps the selected page on refresh', async () => {
    const items = Array.from({ length: 11 }, (_, index) => ledgerItem(index + 1))
    getBankLedger.mockImplementation(async (page: number) => ({
      items: items.slice((page - 1) * 5, page * 5),
      total: items.length,
      page,
      page_size: 5 as const,
      pages: 3,
    }))

    const wrapper = await mountView()

    expect(getBankLedger).toHaveBeenCalledWith(1)
    expect(wrapper.findAll('[data-test="ledger-row"]')).toHaveLength(5)
    expect(wrapper.get('[data-test="ledger-pagination"]').exists()).toBe(true)

    await wrapper.get('[aria-label="pagination.next"]').trigger('click')
    await flushPromises()
    expect(getBankLedger).toHaveBeenLastCalledWith(2)
    expect(wrapper.findAll('[data-test="ledger-row"]')).toHaveLength(5)

    await wrapper.get('[aria-label="bank.ledger.refresh"]').trigger('click')
    await flushPromises()
    expect(getBankLedger).toHaveBeenLastCalledWith(2)
  })

  it('returns the ledger to page one after a successful bank operation', async () => {
    const items = Array.from({ length: 11 }, (_, index) => ledgerItem(index + 1))
    getBankLedger.mockImplementation(async (page: number) => ({
      items: items.slice((page - 1) * 5, page * 5),
      total: items.length,
      page,
      page_size: 5 as const,
      pages: 3,
    }))
    const wrapper = await mountView()
    await wrapper.get('[aria-label="pagination.next"]').trigger('click')
    await flushPromises()

    await wrapper.get('[data-test="advance-input"]').setValue('5.00000000')
    await wrapper.get('[data-test="advance-submit"]').trigger('submit')
    await flushPromises()

    expect(getBankLedger).toHaveBeenLastCalledWith(1)
  })

  it('falls back to the last valid ledger page when data shrinks during refresh', async () => {
    const items = Array.from({ length: 11 }, (_, index) => ledgerItem(index + 1))
    let shrunk = false
    getBankLedger.mockImplementation(async (page: number) => {
      if (shrunk && page > 1) {
        return { items: [], total: 4, page, page_size: 5 as const, pages: 1 }
      }
      const visibleItems = shrunk ? items.slice(0, 4) : items.slice((page - 1) * 5, page * 5)
      return {
        items: visibleItems,
        total: shrunk ? 4 : items.length,
        page,
        page_size: 5 as const,
        pages: shrunk ? 1 : 3,
      }
    })

    const wrapper = await mountView()
    await wrapper.get('[aria-label="pagination.goToPage:3"]').trigger('click')
    await flushPromises()
    expect(getBankLedger).toHaveBeenLastCalledWith(3)

    shrunk = true
    await wrapper.get('[aria-label="bank.ledger.refresh"]').trigger('click')
    await flushPromises()

    expect(getBankLedger.mock.calls.slice(-2).map(([page]) => page)).toEqual([3, 1])
    expect(wrapper.findAll('[data-test="ledger-row"]')).toHaveLength(4)
    expect(wrapper.find('[data-test="ledger-pagination"]').exists()).toBe(false)
  })

  it('keeps the bank page usable when the ledger request fails and can retry it', async () => {
    getBankLedger.mockRejectedValueOnce(new Error('ledger unavailable'))
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="permanent-balance"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="ledger-load-error"]').exists()).toBe(true)

    await wrapper.get('[data-test="ledger-load-error"] button').trigger('click')
    await flushPromises()
    expect(wrapper.get('[data-test="ledger-row"]').exists()).toBe(true)
  })

  it('switches both operations inside one segmented card and renders directional flow targets', async () => {
    const wrapper = await mountView()
    const advanceTab = wrapper.get('[data-test="bank-mode-advance"]')
    const exchangeTab = wrapper.get('[data-test="bank-mode-exchange"]')
    const repayTab = wrapper.get('[data-test="bank-mode-repay"]')
    const advancePanel = wrapper.get(`#${advanceTab.attributes('aria-controls')}`)
    const exchangePanel = wrapper.get(`#${exchangeTab.attributes('aria-controls')}`)
    const repayPanel = wrapper.get(`#${repayTab.attributes('aria-controls')}`)

    expect(wrapper.findAll('[data-test="bank-operation-card"]')).toHaveLength(1)
    expect(advanceTab.attributes('aria-selected')).toBe('true')
    expect(exchangeTab.attributes('aria-selected')).toBe('false')
    expect(wrapper.get('[data-test="bank-mode-indicator"]').attributes('style')).toContain('translateX(0)')
    expect(advancePanel.attributes('aria-hidden')).toBe('false')
    expect(exchangePanel.attributes('aria-hidden')).toBe('true')
    expect(advancePanel.attributes('hidden')).toBeUndefined()
    expect(wrapper.get('[data-test="advance-wallet-balance"]').text()).toBe('3.50')
    expect(exchangePanel.attributes('hidden')).toBeDefined()
    expect(repayPanel.attributes('hidden')).toBeDefined()

    await showExchangeMode(wrapper)

    expect(advanceTab.attributes('aria-selected')).toBe('false')
    expect(exchangeTab.attributes('aria-selected')).toBe('true')
    expect(wrapper.get('[data-test="bank-mode-indicator"]').attributes('style')).toContain('translateX(100%)')
    expect(advancePanel.attributes('aria-hidden')).toBe('true')
    expect(exchangePanel.attributes('aria-hidden')).toBe('false')
    expect(advancePanel.attributes('hidden')).toBeDefined()
    expect(exchangePanel.attributes('hidden')).toBeUndefined()
    expect(wrapper.get('[data-test="exchange-rate"]').text()).toContain('2.00')
    expect(wrapper.get('[data-test="exchange-preview"]').element.tagName).toBe('OUTPUT')
    expect(wrapper.get('[data-test="exchange-preview-amount"]').text()).toBe('0.00')
  })

  it('keeps roving focus and selection synchronized for click and every tab navigation key', async () => {
    const wrapper = await mountView(document.body)
    const advanceTab = wrapper.get('[data-test="bank-mode-advance"]')
    const exchangeTab = wrapper.get('[data-test="bank-mode-exchange"]')
    const repayTab = wrapper.get('[data-test="bank-mode-repay"]')

    expect(advanceTab.attributes('tabindex')).toBe('0')
    expect(exchangeTab.attributes('tabindex')).toBe('-1')

    await exchangeTab.trigger('click')
    await flushPromises()
    expect(exchangeTab.attributes('aria-selected')).toBe('true')
    expect(advanceTab.attributes('tabindex')).toBe('-1')
    expect(exchangeTab.attributes('tabindex')).toBe('0')
    expect(document.activeElement).toBe(exchangeTab.element)

    await exchangeTab.trigger('keydown', { key: 'ArrowLeft' })
    await flushPromises()
    expect(advanceTab.attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(advanceTab.element)

    await advanceTab.trigger('keydown', { key: 'ArrowLeft' })
    await flushPromises()
    expect(repayTab.attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(repayTab.element)

    await repayTab.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(advanceTab.attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(advanceTab.element)

    await advanceTab.trigger('keydown', { key: 'End' })
    await flushPromises()
    expect(repayTab.attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(repayTab.element)

    await repayTab.trigger('keydown', { key: 'Home' })
    await flushPromises()
    expect(advanceTab.attributes('aria-selected')).toBe('true')
    expect(advanceTab.attributes('tabindex')).toBe('0')
    expect(exchangeTab.attributes('tabindex')).toBe('-1')
    expect(document.activeElement).toBe(advanceTab.element)
  })

  it('stacks the English exchange flow below 390px without constraining long labels or input width', async () => {
    vi.stubGlobal('innerWidth', 320)
    const wrapper = await mountView()
    await showExchangeMode(wrapper)

    const flowGrid = wrapper.get('[data-test="exchange-flow-grid"]')
    const inputLabel = wrapper.get('[data-test="exchange-input-label"]')
    const previewLabel = wrapper.get('[data-test="exchange-preview-label"]')

    expect(window.innerWidth).toBe(320)
    expect(flowGrid.classes()).toContain('grid-cols-1')
    expect(flowGrid.classes()).toContain('min-[390px]:grid-cols-[minmax(0,1fr)_auto_minmax(0,1fr)]')
    expect(wrapper.get('[data-test="exchange-input-card"]').classes()).toContain('min-w-0')
    expect(wrapper.get('[data-test="exchange-input"]').classes()).toContain('min-w-0')
    expect(inputLabel.text()).toBe('Permanent credit to use')
    expect(inputLabel.classes()).toContain('break-words')
    expect(previewLabel.text()).toBe('Estimated credit received')
    expect(previewLabel.classes()).toContain('break-words')
  })

  it('hides bank settings from non-admin users', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('[data-test="bank-settings-button"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="bank-transactions-button"]').exists()).toBe(false)
    expect(getBankSettings).not.toHaveBeenCalled()
  })

  it('hides only the administrator transaction shortcut when its page is disabled', async () => {
    authState.isAdmin = true
    appState.cachedPublicSettings = { admin_bank_transactions_enabled: false }

    const wrapper = await mountView()

    expect(wrapper.find('[data-test="bank-transactions-button"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="bank-settings-button"]').exists()).toBe(true)
  })

  it('validates the configured advance range and submits a valid advance', async () => {
    const wrapper = await mountView()
    const input = wrapper.get('[data-test="advance-input"]')

    await input.setValue('4.99999999')
    expect(wrapper.get('[data-test="advance-error"]').text()).toContain('5.00')
    expect(wrapper.get('[data-test="advance-submit"]').attributes('disabled')).toBeDefined()

    await input.setValue('5.00000000')
    await wrapper.get('[data-test="advance-submit"]').trigger('submit')
    await flushPromises()

    expect(requestBankAdvance).toHaveBeenCalledWith('5.00000000', expect.stringMatching(/^bank-advance-/))
    expect(showSuccess).toHaveBeenCalledWith('bank.messages.advanceSucceeded')
    expect(getBankStatus).toHaveBeenCalledTimes(2)
    expect(refreshUser).toHaveBeenCalledTimes(2)
  })

  it('previews a valid advance beside the current wallet balance', async () => {
    const wrapper = await mountView()
    const input = wrapper.get('[data-test="advance-input"]')
    const walletBalance = wrapper.get('[data-test="advance-wallet-balance"]')

    expect(walletBalance.text()).toBe('3.50')
    expect(wrapper.find('[data-test="advance-wallet-addition"]').exists()).toBe(false)

    await input.setValue('5')
    expect(walletBalance.text()).toBe('3.50+5.00')
    expect(wrapper.get('[data-test="advance-wallet-addition"]').text()).toBe('+5.00')

    await input.setValue('4.99999999')
    expect(walletBalance.text()).toBe('3.50')

    await input.setValue('invalid')
    expect(walletBalance.text()).toBe('3.50')

    await input.setValue('')
    expect(walletBalance.text()).toBe('3.50')
  })

  it('blocks another advance while one is active', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({
      temporary_debt: '5.00000000',
      active_advance: {
        id: 9,
        principal: '5.00000000',
        debt_remaining: '5.00000000',
        status: 'active',
        granted_at: '2026-07-19T08:00:00Z',
        grant_expires_at: '2026-07-19T16:00:00Z',
        settlement_due_at: '2026-07-22T16:00:00Z',
      },
    }))

    const wrapper = await mountView()

    expect(wrapper.get('[data-test="active-advance"]').exists()).toBe(true)
    expect(wrapper.get('[data-test="advance-input"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="advance-submit"]').attributes('disabled')).toBeDefined()
  })

  it('shows insufficient permanent balance and exchanges a valid amount', async () => {
    const wrapper = await mountView()
    await showExchangeMode(wrapper)
    const input = wrapper.get('[data-test="exchange-input"]')

    await input.setValue('12.34567891')
    expect(wrapper.get('[data-test="exchange-error"]').text()).toContain('bank.validation.insufficientPermanent')
    expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeDefined()

    await input.setValue('1.23456789')
    expect(wrapper.get('[data-test="exchange-preview-amount"]').text()).toBe('2.47')
    await wrapper.get('[data-test="exchange-submit"]').trigger('submit')
    await flushPromises()

    expect(exchangePermanentForTemporary).toHaveBeenCalledWith('1.23456789', expect.stringMatching(/^bank-exchange-/))
    expect(showSuccess).toHaveBeenCalledWith('bank.messages.exchangeSucceeded')
    expect(refreshUser).toHaveBeenCalledTimes(2)
  })

  it('previews an exchange across multiple daily pricing tiers', async () => {
    const tieredPolicy: BankPolicy = {
      ...policy,
      exchange_tiers: [
        { up_to: '50.00000000', rate: '2.00000000' },
        { up_to: '150.00000000', rate: '1.90000000' },
        { up_to: null, rate: '1.80000000' },
      ],
    }
    getBankStatus.mockResolvedValueOnce(baseStatus({
      permanent_balance: '100.00000000',
      policy: tieredPolicy,
      exchange_progress: {
        date: '2026-07-19',
        permanent_exchanged_today: '40.00000000',
        current_tier_index: 0,
        current_tier_rate: '2.00000000',
        current_tier_up_to: '50.00000000',
        next_tier_rate: '1.90000000',
        amount_until_next_tier: '10.00000000',
      },
    }))

    const wrapper = await mountView()
    await showExchangeMode(wrapper)
    await wrapper.get('[data-test="exchange-input"]').setValue('30.00000000')

    expect(wrapper.get('[data-test="exchange-daily-used"]').text()).toContain('40.00')
    expect(wrapper.get('[data-test="exchange-preview-amount"]').text()).toBe('58.00')
    const tooltipTrigger = wrapper.get('[data-test="exchange-tier-tooltip-trigger"]')
    await tooltipTrigger.trigger('mouseenter')
    await flushPromises()
    const tooltip = document.querySelector('[data-test="exchange-tier-tooltip"]') as HTMLElement | null
    expect(tooltip).not.toBeNull()
    expect(tooltipTrigger.get('[data-icon="questionCircle"]').exists()).toBe(true)
    expect(tooltipTrigger.attributes('aria-describedby')).toBe('exchange-tier-tooltip')
    expect(tooltipTrigger.attributes('aria-expanded')).toBe('true')
    expect(tooltip?.querySelectorAll('li')).toHaveLength(3)
    expect(tooltip?.textContent).toContain('1 : 2.00')
    expect(tooltip?.textContent).toContain('1 : 1.90')
    expect(tooltip?.textContent).toContain('1 : 1.80')
    expect(tooltip?.querySelector('li')?.className).toContain('text-indigo-200')
  })

  it('clamps the tier tooltip inside narrow, two-column, and desktop viewports', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({
      policy: {
        ...policy,
        exchange_tiers: [
          { up_to: '50.00000000', rate: '2.00000000' },
          { up_to: null, rate: '1.90000000' },
        ],
      },
      exchange_progress: {
        date: '2026-07-19',
        permanent_exchanged_today: '40.00000000',
        current_tier_index: 0,
        current_tier_rate: '2.00000000',
        current_tier_up_to: '50.00000000',
        next_tier_rate: '1.90000000',
        amount_until_next_tier: '10.00000000',
      },
    }))
    vi.stubGlobal('innerWidth', 320)
    vi.stubGlobal('innerHeight', 800)

    const wrapper = await mountView(document.body)
    await showExchangeMode(wrapper)
    const summary = wrapper.get('[data-test="exchange-progress-summary"]')
    const trigger = wrapper.get('[data-test="exchange-tier-tooltip-trigger"]')
    const tooltip = document.querySelector('[data-test="exchange-tier-tooltip"]') as HTMLElement
    expect(summary.classes()).toEqual(expect.arrayContaining([
      'grid-cols-1',
      'min-[390px]:grid-cols-2',
      'sm:grid-cols-4',
    ]))
    expect(trigger.classes()).toEqual(expect.arrayContaining(['h-6', 'w-6']))
    Object.defineProperty(tooltip, 'offsetWidth', { configurable: true, value: 288 })
    Object.defineProperty(tooltip, 'offsetHeight', { configurable: true, value: 140 })
    const rectSpy = vi.spyOn(trigger.element, 'getBoundingClientRect')
    rectSpy.mockReturnValue({
      left: 8, right: 32, top: 100, bottom: 124, width: 24, height: 24,
      x: 8, y: 100, toJSON: () => ({}),
    } as DOMRect)

    await trigger.trigger('focus')
    await flushPromises()
    expect(tooltip.style.left).toBe('16px')
    expect(trigger.attributes('aria-expanded')).toBe('true')

    await trigger.trigger('mouseenter')
    await trigger.trigger('mouseleave')
    await flushPromises()
    expect(trigger.attributes('aria-expanded')).toBe('true')

    vi.stubGlobal('innerWidth', 500)
    rectSpy.mockReturnValue({
      left: 468, right: 492, top: 100, bottom: 124, width: 24, height: 24,
      x: 468, y: 100, toJSON: () => ({}),
    } as DOMRect)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(tooltip.style.left).toBe('196px')

    vi.stubGlobal('innerWidth', 1200)
    rectSpy.mockReturnValue({
      left: 600, right: 624, top: 100, bottom: 124, width: 24, height: 24,
      x: 600, y: 100, toJSON: () => ({}),
    } as DOMRect)
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(tooltip.style.left).toBe('468px')

    await trigger.trigger('keydown', { key: 'Escape' })
    expect(trigger.attributes('aria-expanded')).toBe('false')
    expect(tooltip.style.display).toBe('none')
  })

  it('previews and submits early repayment with either configured source ratio', async () => {
    getBankStatus.mockResolvedValue(baseStatus({
      temporary_debt: '5.00000000',
      temporary_debt_due_at: '2026-07-22T16:00:00Z',
    }))
    const wrapper = await mountView()
    await showRepayMode(wrapper)

    await wrapper.get('[data-test="repay-input"]').setValue('1.25000000')
    expect(wrapper.get('[data-test="repay-preview-amount"]').text()).toBe('1.25')
    expect(wrapper.get('[data-test="repay-debt-remaining"]').text()).toContain('3.75')
    await wrapper.get('[data-test="repay-submit"]').trigger('submit')
    await flushPromises()
    expect(repayBankDebt).toHaveBeenCalledWith(
      'temporary',
      '1.25000000',
      expect.stringMatching(/^bank-repay-/),
    )

    getBankStatus.mockResolvedValue(baseStatus({ temporary_debt: '5.00000000' }))
    await wrapper.get('[data-test="repay-source-permanent"]').trigger('click')
    await wrapper.get('[data-test="repay-input"]').setValue('1.00000000')
    expect(wrapper.get('[data-test="repay-preview-amount"]').text()).toBe('2.00')
    expect(wrapper.get('[data-test="repay-debt-remaining"]').text()).toContain('3.00')

    await wrapper.get('[data-test="repay-input"]').setValue('9.00000000')
    expect(wrapper.get('[data-test="repay-preview-amount"]').text()).toBe('5.00')
    expect(wrapper.get('[data-test="repay-debt-remaining"]').text()).toContain('0.00')
  })

  it.each([
    ['23:55:00', '2026-07-19T15:55:00.000Z'],
    ['00:04:59', '2026-07-19T16:04:59.000Z'],
  ])('allows exchange at the former Beijing maintenance-window time %s', async (_label, timestamp) => {
    vi.setSystemTime(new Date(timestamp))

    const wrapper = await mountView()
    await showExchangeMode(wrapper)
    const input = wrapper.get('[data-test="exchange-input"]')

    expect(input.attributes('disabled')).toBeUndefined()
    await input.setValue('1.00000000')
    expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="exchange-submit"]').trigger('submit')
    await flushPromises()

    expect(exchangePermanentForTemporary).toHaveBeenCalledTimes(1)
  })

  it('compares the full eight-decimal balance without floating-point rounding', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({
      permanent_balance: '999999999999.99999998',
    }))
    const wrapper = await mountView()
    await showExchangeMode(wrapper)

    await wrapper.get('[data-test="exchange-input"]').setValue('999999999999.99999999')

    expect(wrapper.get('[data-test="exchange-error"]').text()).toContain('bank.validation.insufficientPermanent')
    expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeDefined()
  })

  it('allows temporary-credit repayment while permanent balance is negative', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({
      permanent_balance: '-1.00000000',
      temporary_credit_available: '3.50000000',
      temporary_debt: '5.00000000',
    }))

    const wrapper = await mountView()
    await showRepayMode(wrapper)
    const input = wrapper.get('[data-test="repay-input"]')

    expect(input.attributes('disabled')).toBeUndefined()
    await input.setValue('1.00000000')
    expect(wrapper.get('[data-test="repay-submit"]').attributes('disabled')).toBeUndefined()
    await wrapper.get('[data-test="repay-flow"]').trigger('submit')
    await flushPromises()

    expect(repayBankDebt).toHaveBeenCalledWith(
      'temporary',
      '1.00000000',
      expect.stringMatching(/^bank-repay-/),
    )
  })

  it.each(['-0.00000001', 'not-a-number'])(
    'blocks permanent-backed mutations when permanent balance is unusable: %s',
    async (permanentBalance) => {
      getBankStatus.mockResolvedValueOnce(baseStatus({
        permanent_balance: permanentBalance,
        temporary_debt: '5.00000000',
      }))

      const wrapper = await mountView()

      expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('0.00')
      expect(wrapper.get('[data-test="advance-input"]').attributes('disabled')).toBeDefined()
      await showExchangeMode(wrapper)
      expect(wrapper.get('[data-test="exchange-input"]').attributes('disabled')).toBeDefined()
      await showRepayMode(wrapper)
      expect(wrapper.get('[data-test="repay-input"]').attributes('disabled')).toBeUndefined()
      await wrapper.get('[data-test="repay-source-permanent"]').trigger('click')
      expect(wrapper.get('[data-test="repay-input"]').attributes('disabled')).toBeDefined()
      expect(wrapper.get('[data-test="repay-submit"]').attributes('disabled')).toBeDefined()
      await wrapper.get('[data-test="repay-flow"]').trigger('submit')
      expect(repayBankDebt).not.toHaveBeenCalled()
    },
  )

  it('allows admins to save exchange tiers and derives the compatibility rate from the first tier', async () => {
    authState.isAdmin = true
    const updatedPolicy: BankPolicy = {
      advance_min_amount: '6.00000000',
      advance_max_amount: '30.00000000',
      debt_grace_days: 5,
      debt_conversion_ratio: '1.50000000',
      exchange_rate: '3.00000000',
      unused_advance_debt_reduction_ratio: '0.75000000',
      early_repay_temporary_ratio: '1.00000000',
      early_repay_permanent_ratio: '2.00000000',
      exchange_tiers: [
        { up_to: '50.00000000', rate: '3.00000000' },
        { up_to: '150.00000000', rate: '1.90000000' },
        { up_to: null, rate: '1.80000000' },
      ],
    }
    getBankSettings.mockResolvedValueOnce({
      ...updatedPolicy,
      exchange_rate: '2.00000000',
      exchange_tiers: [
        { up_to: '50.00000000', rate: '2.00000000' },
        { up_to: '150.00000000', rate: '1.90000000' },
        { up_to: null, rate: '1.80000000' },
      ],
    })
    updateBankSettings.mockResolvedValueOnce(updatedPolicy)
    const wrapper = await mountView()

    await wrapper.get('[data-test="bank-settings-button"]').trigger('click')
    await flushPromises()
    expect(getBankSettings).toHaveBeenCalled()

    await wrapper.get('[data-test="settings-min"]').setValue(updatedPolicy.advance_min_amount)
    await wrapper.get('[data-test="settings-max"]').setValue(updatedPolicy.advance_max_amount)
    await wrapper.get('[data-test="settings-grace-days"]').setValue('5')
    await wrapper.get('[data-test="settings-debt-ratio"]').setValue(updatedPolicy.debt_conversion_ratio)
    expect(wrapper.find('[data-test="settings-exchange-rate"]').exists()).toBe(false)
    await wrapper.get('[data-test="settings-section-exchange"]').trigger('click')
    await wrapper.get('[data-test="settings-tier-rate-0"]').setValue('3.00000000')
    await wrapper.get('[data-test="settings-save"]').trigger('click')
    await flushPromises()

    expect(updateBankSettings).toHaveBeenCalledWith(updatedPolicy, expect.stringMatching(/^bank-settings-/))
    expect(showSuccess).toHaveBeenCalledWith('bank.messages.settingsSaved')
    expect(wrapper.find('[data-test="dialog"]').exists()).toBe(false)
  })

  it('shows two decimals in bank settings while preserving untouched and edited precision on save', async () => {
    authState.isAdmin = true
    const precisePolicy: BankPolicy = {
      ...policy,
      advance_min_amount: '5.12345678',
      advance_max_amount: '20.98765432',
      debt_conversion_ratio: '1.23456789',
      unused_advance_debt_reduction_ratio: '0.76543210',
      early_repay_temporary_ratio: '0.91234567',
      early_repay_permanent_ratio: '1.87654321',
      exchange_tiers: [
        { up_to: '50.12345678', rate: '2.12345678' },
        { up_to: null, rate: '1.87654321' },
      ],
    }
    getBankSettings.mockResolvedValueOnce(precisePolicy)
    updateBankSettings.mockResolvedValueOnce(precisePolicy)

    const wrapper = await mountView()
    await wrapper.get('[data-test="bank-settings-button"]').trigger('click')
    await flushPromises()

    expect((wrapper.get('[data-test="settings-min"]').element as HTMLInputElement).value).toBe('5.12')
    expect((wrapper.get('[data-test="settings-max"]').element as HTMLInputElement).value).toBe('20.99')
    expect((wrapper.get('[data-test="settings-debt-ratio"]').element as HTMLInputElement).value).toBe('1.23')
    expect((wrapper.get('[data-test="settings-grace-days"]').element as HTMLInputElement).value).toBe('3')

    await wrapper.get('[data-test="settings-section-exchange"]').trigger('click')
    expect((wrapper.get('[data-test="settings-tier-upper-0"]').element as HTMLInputElement).value).toBe('50.12')
    expect((wrapper.get('[data-test="settings-tier-rate-0"]').element as HTMLInputElement).value).toBe('2.12')

    await wrapper.get('[data-test="settings-min"]').setValue('6.12345678')
    await wrapper.get('[data-test="settings-tier-rate-0"]').setValue('2.23456789')
    await wrapper.get('[data-test="settings-save"]').trigger('click')
    await flushPromises()

    expect(updateBankSettings).toHaveBeenCalledWith(expect.objectContaining({
      advance_min_amount: '6.12345678',
      advance_max_amount: '20.98765432',
      debt_conversion_ratio: '1.23456789',
      unused_advance_debt_reduction_ratio: '0.76543210',
      early_repay_temporary_ratio: '0.91234567',
      early_repay_permanent_ratio: '1.87654321',
      exchange_rate: '2.23456789',
      exchange_tiers: [
        { up_to: '50.12345678', rate: '2.23456789' },
        { up_to: null, rate: '1.87654321' },
      ],
    }), expect.stringMatching(/^bank-settings-/))
  })

  it('exposes bank setting sections as linked tabs with roving keyboard focus', async () => {
    authState.isAdmin = true
    const wrapper = await mountView(document.body)

    await wrapper.get('[data-test="bank-settings-button"]').trigger('click')
    await flushPromises()

    const advanceTab = wrapper.get('[data-test="settings-section-advance"]')
    const exchangeTab = wrapper.get('[data-test="settings-section-exchange"]')
    const repayTab = wrapper.get('[data-test="settings-section-repay"]')
    const advancePanel = wrapper.get(`#${advanceTab.attributes('aria-controls')}`)
    const exchangePanel = wrapper.get(`#${exchangeTab.attributes('aria-controls')}`)
    const repayPanel = wrapper.get(`#${repayTab.attributes('aria-controls')}`)
    const exchangeElement = exchangeTab.element as HTMLElement

    expect(advanceTab.attributes('role')).toBe('tab')
    expect(advanceTab.attributes('aria-selected')).toBe('true')
    expect(advanceTab.attributes('tabindex')).toBe('0')
    expect(exchangeTab.attributes('tabindex')).toBe('-1')
    expect(advancePanel.attributes('role')).toBe('tabpanel')
    expect(advancePanel.attributes('aria-labelledby')).toBe(advanceTab.attributes('id'))
    expect(advancePanel.attributes('aria-hidden')).toBe('false')
    expect(exchangePanel.attributes('aria-hidden')).toBe('true')

    exchangeElement.focus()
    await exchangeTab.trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(exchangeTab.attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(exchangeTab.element)

    await exchangeTab.trigger('keydown', { key: 'ArrowLeft' })
    await flushPromises()
    expect(advanceTab.attributes('aria-selected')).toBe('true')

    await advanceTab.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(exchangeTab.attributes('aria-selected')).toBe('true')
    expect(exchangeTab.attributes('tabindex')).toBe('0')
    expect(exchangePanel.attributes('aria-hidden')).toBe('false')
    expect(document.activeElement).toBe(exchangeTab.element)

    await exchangeTab.trigger('keydown', { key: 'End' })
    await flushPromises()
    expect(repayTab.attributes('aria-selected')).toBe('true')
    expect(repayPanel.attributes('aria-labelledby')).toBe(repayTab.attributes('id'))
    expect(document.activeElement).toBe(repayTab.element)

    await repayTab.trigger('keydown', { key: 'ArrowRight' })
    await flushPromises()
    expect(advanceTab.attributes('aria-selected')).toBe('true')

    await advanceTab.trigger('keydown', { key: 'ArrowLeft' })
    await flushPromises()
    expect(repayTab.attributes('aria-selected')).toBe('true')

    await repayTab.trigger('keydown', { key: 'Home' })
    await flushPromises()
    expect(advanceTab.attributes('aria-selected')).toBe('true')
    expect(document.activeElement).toBe(advanceTab.element)
  })

  it('offers a retry after the initial bank status request fails', async () => {
    getBankStatus.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce(baseStatus())
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="bank-load-error"]').exists()).toBe(true)
    await wrapper.get('[aria-label="bank.actions.reloadStatus"]').trigger('click')
    await flushPromises()

    expect(getBankStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('12.35')
  })

  it('reuses an advance idempotency key after a lost response and rotates it after success', async () => {
    requestBankAdvance.mockRejectedValueOnce(new Error('connection lost'))
    const wrapper = await mountView()
    const input = wrapper.get('[data-test="advance-input"]')
    const submit = wrapper.get('[data-test="advance-submit"]')

    await input.setValue('5.00000000')
    await submit.trigger('submit')
    await flushPromises()
    const firstKey = requestBankAdvance.mock.calls[0][1]

    await submit.trigger('submit')
    await flushPromises()
    const retryKey = requestBankAdvance.mock.calls[1][1]

    expect(retryKey).toBe(firstKey)
    expect(localStorage.getItem('bank-advance-idempotency-key')).toBeNull()

    await input.setValue('6.00000000')
    await submit.trigger('submit')
    await flushPromises()

    expect(requestBankAdvance.mock.calls[2][1]).not.toBe(firstKey)
  })

  it('keeps bank status usable when refreshing the header user fails', async () => {
    refreshUser.mockRejectedValueOnce(new Error('profile unavailable'))

    const wrapper = await mountView()

    expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('12.35')
    expect(showError).not.toHaveBeenCalled()
  })
})
