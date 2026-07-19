import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, flushPromises, mount } from '@vue/test-utils'

import type { BankPolicy, BankStatus } from '@/api/bank'
import BankView from '../BankView.vue'

enableAutoUnmount(afterEach)

afterEach(() => {
  vi.useRealTimers()
})

const {
  authState,
  exchangePermanentForTemporary,
  getBankSettings,
  getBankStatus,
  requestBankAdvance,
  refreshUser,
  showError,
  showSuccess,
  updateBankSettings,
} = vi.hoisted(() => ({
  authState: { isAdmin: false, refreshUser: vi.fn() },
  exchangePermanentForTemporary: vi.fn(),
  getBankSettings: vi.fn(),
  getBankStatus: vi.fn(),
  requestBankAdvance: vi.fn(),
  refreshUser: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  updateBankSettings: vi.fn(),
}))

vi.mock('@/api/bank', () => ({
  exchangePermanentForTemporary,
  getBankSettings,
  getBankStatus,
  requestBankAdvance,
  updateBankSettings,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
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
      t: (key: string, values?: Record<string, string | number>) =>
        [key, ...Object.values(values ?? {})].join(':'),
    }),
  }
})

const policy: BankPolicy = {
  advance_min_amount: '5.00000000',
  advance_max_amount: '20.00000000',
  debt_grace_days: 3,
  debt_conversion_ratio: '1.25000000',
  exchange_rate: '2.00000000',
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

const mountView = async () => {
  const wrapper = mount(BankView, {
    global: {
      stubs: {
        AppLayout: { template: '<main><slot /></main>' },
        Icon: { template: '<i />' },
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

describe('BankView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-19T08:00:00.000Z'))
    authState.isAdmin = false
    authState.refreshUser = refreshUser
    localStorage.clear()
    exchangePermanentForTemporary.mockReset()
    getBankSettings.mockReset()
    getBankStatus.mockReset()
    requestBankAdvance.mockReset()
    refreshUser.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    updateBankSettings.mockReset()
    getBankStatus.mockResolvedValue(baseStatus())
    getBankSettings.mockResolvedValue(policy)
    requestBankAdvance.mockResolvedValue({ amount: '5.00000000' })
    refreshUser.mockResolvedValue(undefined)
    exchangePermanentForTemporary.mockResolvedValue({ permanent_spent: '1.00000000' })
    updateBankSettings.mockResolvedValue(policy)
  })

  it('renders all balances and ledger deltas with exactly eight decimals', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({
      permanent_balance: '12.3',
      temporary_credit_available: '3.5',
      temporary_debt: '1',
    }))

    const wrapper = await mountView()

    expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('12.30000000')
    expect(wrapper.get('[data-test="temporary-balance"]').text()).toBe('3.50000000')
    expect(wrapper.get('[data-test="temporary-debt"]').text()).toBe('1.00000000')
    expect(wrapper.get('[data-test="ledger-row"]').text()).toContain('-1.25000000')
    expect(wrapper.get('[data-test="ledger-row"]').text()).toContain('+2.50000000')
  })

  it('hides bank settings from non-admin users', async () => {
    const wrapper = await mountView()

    expect(wrapper.find('[data-test="bank-settings-button"]').exists()).toBe(false)
    expect(getBankSettings).not.toHaveBeenCalled()
  })

  it('validates the configured advance range and submits a valid advance', async () => {
    const wrapper = await mountView()
    const input = wrapper.get('[data-test="advance-input"]')

    await input.setValue('4.99999999')
    expect(wrapper.get('[data-test="advance-error"]').text()).toContain('5.00000000')
    expect(wrapper.get('[data-test="advance-submit"]').attributes('disabled')).toBeDefined()

    await input.setValue('5.00000000')
    await wrapper.get('[data-test="advance-submit"]').trigger('submit')
    await flushPromises()

    expect(requestBankAdvance).toHaveBeenCalledWith('5.00000000', expect.stringMatching(/^bank-advance-/))
    expect(showSuccess).toHaveBeenCalledWith('bank.messages.advanceSucceeded')
    expect(getBankStatus).toHaveBeenCalledTimes(2)
    expect(refreshUser).toHaveBeenCalledTimes(2)
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
    const input = wrapper.get('[data-test="exchange-input"]')

    await input.setValue('12.34567891')
    expect(wrapper.get('[data-test="exchange-error"]').text()).toContain('bank.validation.insufficientPermanent')
    expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeDefined()

    await input.setValue('1.25000000')
    expect(wrapper.text()).toContain('2.50000000')
    await wrapper.get('[data-test="exchange-submit"]').trigger('submit')
    await flushPromises()

    expect(exchangePermanentForTemporary).toHaveBeenCalledWith('1.25000000', expect.stringMatching(/^bank-exchange-/))
    expect(showSuccess).toHaveBeenCalledWith('bank.messages.exchangeSucceeded')
    expect(refreshUser).toHaveBeenCalledTimes(2)
  })

  it.each([
    ['23:54:59', '2026-07-19T15:54:59.000Z', false],
    ['23:55:00', '2026-07-19T15:55:00.000Z', true],
    ['00:04:59', '2026-07-19T16:04:59.000Z', true],
    ['00:05:00', '2026-07-19T16:05:00.000Z', false],
  ])('applies the Beijing exchange window at %s', async (_label, timestamp, blocked) => {
    vi.setSystemTime(new Date(timestamp))

    const wrapper = await mountView()
    const input = wrapper.get('[data-test="exchange-input"]')

    expect(input.attributes('disabled') !== undefined).toBe(blocked)
    expect(wrapper.find('[data-test="exchange-maintenance"]').exists()).toBe(blocked)
    if (blocked) {
      expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeDefined()
    } else {
      await input.setValue('1.00000000')
      expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeUndefined()
    }
  })

  it('rechecks the exchange window immediately before submitting', async () => {
    vi.setSystemTime(new Date('2026-07-19T15:54:59.000Z'))
    const wrapper = await mountView()
    const input = wrapper.get('[data-test="exchange-input"]')

    await input.setValue('1.00000000')
    expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeUndefined()

    vi.setSystemTime(new Date('2026-07-19T15:55:00.000Z'))
    await wrapper.get('[data-test="exchange-submit"]').trigger('submit')

    expect(exchangePermanentForTemporary).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('bank.exchangeMaintenance')
    expect(wrapper.get('[data-test="exchange-maintenance"]').exists()).toBe(true)
  })

  it('localizes the server maintenance-window rejection', async () => {
    vi.setSystemTime(new Date('2026-07-19T15:54:59.000Z'))
    exchangePermanentForTemporary.mockRejectedValueOnce({
      status: 403,
      code: 'BANK_EXCHANGE_MAINTENANCE_WINDOW',
      message: 'server-only message',
    })
    const wrapper = await mountView()

    await wrapper.get('[data-test="exchange-input"]').setValue('1.00000000')
    await wrapper.get('[data-test="exchange-submit"]').trigger('submit')
    await flushPromises()

    expect(exchangePermanentForTemporary).toHaveBeenCalledTimes(1)
    expect(showError).toHaveBeenCalledWith('bank.exchangeMaintenance')
  })

  it('compares the full eight-decimal balance without floating-point rounding', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({
      permanent_balance: '999999999999.99999998',
    }))
    const wrapper = await mountView()

    await wrapper.get('[data-test="exchange-input"]').setValue('999999999999.99999999')

    expect(wrapper.get('[data-test="exchange-error"]').text()).toContain('bank.validation.insufficientPermanent')
    expect(wrapper.get('[data-test="exchange-submit"]').attributes('disabled')).toBeDefined()
  })

  it('disables both bank mutations when permanent balance is negative', async () => {
    getBankStatus.mockResolvedValueOnce(baseStatus({ permanent_balance: '-0.00000001' }))

    const wrapper = await mountView()

    expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('-0.00000001')
    expect(wrapper.get('[data-test="advance-input"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="exchange-input"]').attributes('disabled')).toBeDefined()
  })

  it('allows admins to load, validate, and save the bank policy', async () => {
    authState.isAdmin = true
    const updatedPolicy: BankPolicy = {
      advance_min_amount: '6.00000000',
      advance_max_amount: '30.00000000',
      debt_grace_days: 5,
      debt_conversion_ratio: '1.50000000',
      exchange_rate: '3.00000000',
    }
    updateBankSettings.mockResolvedValueOnce(updatedPolicy)
    const wrapper = await mountView()

    await wrapper.get('[data-test="bank-settings-button"]').trigger('click')
    await flushPromises()
    expect(getBankSettings).toHaveBeenCalled()

    await wrapper.get('[data-test="settings-min"]').setValue(updatedPolicy.advance_min_amount)
    await wrapper.get('[data-test="settings-max"]').setValue(updatedPolicy.advance_max_amount)
    await wrapper.get('[data-test="settings-grace-days"]').setValue('5')
    await wrapper.get('[data-test="settings-debt-ratio"]').setValue(updatedPolicy.debt_conversion_ratio)
    await wrapper.get('[data-test="settings-exchange-rate"]').setValue(updatedPolicy.exchange_rate)
    await wrapper.get('[data-test="settings-save"]').trigger('click')
    await flushPromises()

    expect(updateBankSettings).toHaveBeenCalledWith(updatedPolicy, expect.stringMatching(/^bank-settings-/))
    expect(showSuccess).toHaveBeenCalledWith('bank.messages.settingsSaved')
    expect(wrapper.find('[data-test="dialog"]').exists()).toBe(false)
  })

  it('offers a retry after the initial bank status request fails', async () => {
    getBankStatus.mockRejectedValueOnce(new Error('unavailable')).mockResolvedValueOnce(baseStatus())
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="bank-load-error"]').exists()).toBe(true)
    await wrapper.get('[aria-label="bank.actions.reloadStatus"]').trigger('click')
    await flushPromises()

    expect(getBankStatus).toHaveBeenCalledTimes(2)
    expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('12.34567890')
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

    expect(wrapper.get('[data-test="permanent-balance"]').text()).toBe('12.34567890')
    expect(showError).not.toHaveBeenCalled()
  })
})
