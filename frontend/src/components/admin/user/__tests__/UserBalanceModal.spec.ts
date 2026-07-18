import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { updateBalance, grantTemporaryCredit, showError, showSuccess } = vi.hoisted(() => ({
  updateBalance: vi.fn(),
  grantTemporaryCredit: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { updateBalance, grantTemporaryCredit },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
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
    name: 'BaseDialog',
    props: [
      'show',
      'title',
      'width',
      'closeOnEscape',
      'closeOnClickOutside',
      'showCloseButton',
    ],
    emits: ['close'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>',
  },
}))

import UserBalanceModal from '../UserBalanceModal.vue'

const user = {
  id: 9,
  username: 'user-9',
  email: 'user9@example.com',
  role: 'user',
  balance: 12.5,
  concurrency: 1,
  status: 'active',
  allowed_groups: [],
  balance_notify_enabled: false,
  balance_notify_threshold: null,
  balance_notify_extra_emails: [],
  notes: '',
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
} as const

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })
  return { promise, resolve, reject }
}

function mountModal(operation: 'add' | 'subtract' = 'add') {
  return mount(UserBalanceModal, {
    props: { show: true, user: user as any, operation },
    global: { stubs: { Icon: true } },
  })
}

describe('UserBalanceModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    updateBalance.mockResolvedValue(user)
    grantTemporaryCredit.mockResolvedValue({
      temporary_credit_grant_id: 18,
      amount: '0.00000001',
      remaining_amount: '0.00000001',
      expires_at: '2026-07-17T16:00:00Z',
      notes: '',
    })
  })

  it('shows permanent and temporary choices only for add operations', () => {
    expect(mountModal('add').find('[data-testid="credit-type-temporary"]').exists()).toBe(true)
    expect(mountModal('subtract').find('[data-testid="credit-type-temporary"]').exists()).toBe(false)
  })

  it('sends temporary credit amounts unchanged as strings with a valid idempotency key', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('0.00000001')
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(grantTemporaryCredit).toHaveBeenCalledTimes(1)
    const [userID, payload, key] = grantTemporaryCredit.mock.calls[0]
    expect(userID).toBe(9)
    expect(payload).toEqual({ amount: '0.00000001', notes: '' })
    expect(typeof payload.amount).toBe('string')
    expect(key).toMatch(/^admin-temp-credit-9-[!-~]+$/)
    expect(key.length).toBeLessThanOrEqual(128)
  })

  it('reuses the same idempotency key when retrying an unchanged failed grant', async () => {
    grantTemporaryCredit
      .mockRejectedValueOnce(new Error('network error'))
      .mockResolvedValueOnce({
        temporary_credit_grant_id: 18,
        amount: '1.00000000',
        remaining_amount: '1.00000000',
        expires_at: '2026-07-17T16:00:00Z',
        notes: '',
      })
    const wrapper = mountModal()
    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.00000000')

    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(grantTemporaryCredit).toHaveBeenCalledTimes(2)
    expect(grantTemporaryCredit.mock.calls[1]?.[2]).toBe(grantTemporaryCredit.mock.calls[0]?.[2])
  })

  it('resets a completed grant and uses a new idempotency key after reopening', async () => {
    grantTemporaryCredit
      .mockResolvedValueOnce({
        temporary_credit_grant_id: 18,
        amount: '1.00000000',
        remaining_amount: '1.00000000',
        expires_at: '2026-07-17T16:00:00Z',
        notes: 'same grant',
      })
      .mockResolvedValueOnce({
        temporary_credit_grant_id: 19,
        amount: '1.00000000',
        remaining_amount: '1.00000000',
        expires_at: '2026-07-18T16:00:00Z',
        notes: 'same grant',
      })
    const wrapper = mountModal()

    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.00000000')
    await wrapper.get('#balance-notes').setValue('same grant')
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(wrapper.get('[data-testid="temporary-grant-result"]').text()).toContain(
      '2026-07-17T16:00:00Z',
    )

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })

    expect(wrapper.find('[data-testid="temporary-grant-result"]').exists()).toBe(false)
    expect((wrapper.get('[data-testid="balance-amount"]').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('#balance-notes').element as HTMLTextAreaElement).value).toBe('')
    expect(wrapper.get('[data-testid="credit-type-permanent"]').attributes('aria-pressed')).toBe('true')

    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.00000000')
    await wrapper.get('#balance-notes').setValue('same grant')
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(grantTemporaryCredit).toHaveBeenCalledTimes(2)
    expect(grantTemporaryCredit.mock.calls[1]?.[1]).toEqual(grantTemporaryCredit.mock.calls[0]?.[1])
    expect(grantTemporaryCredit.mock.calls[1]?.[2]).not.toBe(grantTemporaryCredit.mock.calls[0]?.[2])
    expect(wrapper.get('[data-testid="temporary-grant-result"]').text()).toContain(
      '2026-07-18T16:00:00Z',
    )
  })

  it('keeps the result open and shows the server UTC expiry after a temporary grant', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('0.00000001')
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    const result = wrapper.get('[data-testid="temporary-grant-result"]').text()
    expect(result).toContain('0.00')
    expect(result).not.toContain('0.00000001')
    expect(result).toContain('2026-07-17T16:00:00Z')
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toBeUndefined()
  })

  it('converts once at the legacy permanent balance API boundary', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.25000000')
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(updateBalance).toHaveBeenCalledWith(
      9,
      1.25,
      'add',
      expect.stringMatching(/^admin-balance-9-[!-~]+$/),
      '',
    )
    expect(grantTemporaryCredit).not.toHaveBeenCalled()
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('reuses the permanent balance idempotency key for an unchanged failed retry', async () => {
    updateBalance.mockRejectedValue(new Error('network error'))
    const wrapper = mountModal()
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.25')
    await wrapper.get('#balance-notes').setValue('manual')

    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(updateBalance).toHaveBeenCalledTimes(2)
    expect(updateBalance.mock.calls[0]?.[3]).toMatch(/^admin-balance-9-[!-~]+$/)
    expect(updateBalance.mock.calls[1]?.[3]).toBe(updateBalance.mock.calls[0]?.[3])
  })

  it('uses a new key when mode, amount, reason, operation, or user changes', async () => {
    updateBalance.mockRejectedValue(new Error('request failed'))
    grantTemporaryCredit.mockRejectedValue(new Error('request failed'))
    const wrapper = mountModal()
    const submit = async () => {
      await wrapper.get('#balance-form').trigger('submit')
      await flushPromises()
    }

    await wrapper.get('[data-testid="balance-amount"]').setValue('1.00')
    await wrapper.get('#balance-notes').setValue('first')
    await submit()

    await wrapper.get('[data-testid="balance-amount"]').setValue('2.00')
    await submit()

    await wrapper.get('#balance-notes').setValue('second')
    await submit()

    await wrapper.setProps({
      user: { ...user, id: 10, email: 'user10@example.com' } as any,
    })
    await wrapper.get('[data-testid="balance-amount"]').setValue('2.00')
    await wrapper.get('#balance-notes').setValue('second')
    await submit()

    await wrapper.setProps({ operation: 'subtract' })
    await wrapper.get('[data-testid="balance-amount"]').setValue('2.00')
    await wrapper.get('#balance-notes').setValue('second')
    await submit()

    const permanentKeys = updateBalance.mock.calls.map(call => call[3])
    expect(permanentKeys).toHaveLength(5)
    expect(new Set(permanentKeys).size).toBe(permanentKeys.length)

    await wrapper.setProps({ operation: 'add' })
    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('2.00')
    await wrapper.get('#balance-notes').setValue('second')
    await submit()

    const temporaryKey = grantTemporaryCredit.mock.calls[0]?.[2]
    expect(temporaryKey).toMatch(/^admin-temp-credit-10-[!-~]+$/)
    expect(permanentKeys).not.toContain(temporaryKey)
  })

  it('uses a new permanent balance key after a successful close and reopen', async () => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.25')
    await wrapper.get('#balance-notes').setValue('manual')
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    const firstKey = updateBalance.mock.calls[0]?.[3]
    await wrapper.setProps({ show: false })
    await wrapper.setProps({ show: true })
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.25')
    await wrapper.get('#balance-notes').setValue('manual')
    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(updateBalance).toHaveBeenCalledTimes(2)
    expect(updateBalance.mock.calls[1]?.[3]).not.toBe(firstKey)
  })

  it('serializes deferred permanent balance submits including Enter resubmission', async () => {
    const updateRequest = deferred<typeof user>()
    updateBalance.mockReturnValueOnce(updateRequest.promise)
    const wrapper = mountModal()
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.25')

    const form = wrapper.get('#balance-form')
    await form.trigger('submit')
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()

    await wrapper.get('[data-testid="balance-amount"]').trigger('keydown', { key: 'Enter' })
    await form.trigger('submit')
    expect(updateBalance).toHaveBeenCalledTimes(1)

    updateRequest.resolve(user)
    await flushPromises()

    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('blocks every close entry while submitting and ignores an old user session response', async () => {
    const oldRequest = deferred<typeof user>()
    const currentRequest = deferred<typeof user>()
    const nextUser = { ...user, id: 10, email: 'user10@example.com' } as const
    updateBalance
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(currentRequest.promise)
      .mockResolvedValue(nextUser)
    const wrapper = mountModal()
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.25')
    await wrapper.get('#balance-notes').setValue('same request')
    await wrapper.get('#balance-form').trigger('submit')

    const dialog = wrapper.findComponent({ name: 'BaseDialog' })
    expect(dialog.props('closeOnEscape')).toBe(false)
    expect(dialog.props('showCloseButton')).toBe(false)
    expect(wrapper.get('[data-testid="balance-cancel"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="balance-amount"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('#balance-notes').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-testid="credit-type-permanent"]').attributes('disabled')).toBeDefined()
    dialog.vm.$emit('close')
    await wrapper.vm.$nextTick()
    expect(wrapper.emitted('close')).toBeUndefined()

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ user: nextUser as any })
    await wrapper.setProps({ show: true })
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.25')
    await wrapper.get('#balance-notes').setValue('same request')
    await wrapper.get('#balance-form').trigger('submit')

    oldRequest.resolve(user)
    await flushPromises()
    expect(wrapper.emitted('success')).toBeUndefined()
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeDefined()

    currentRequest.reject(new Error('network error'))
    await flushPromises()
    expect(wrapper.get('button[type="submit"]').attributes('disabled')).toBeUndefined()
    expect(showError).toHaveBeenCalledTimes(1)

    await wrapper.get('#balance-form').trigger('submit')
    await flushPromises()

    expect(updateBalance.mock.calls.map(call => call[0])).toEqual([9, 10, 10])
    expect(updateBalance.mock.calls[1]?.[3]).not.toBe(updateBalance.mock.calls[0]?.[3])
    expect(updateBalance.mock.calls[2]?.[3]).toBe(updateBalance.mock.calls[1]?.[3])
    expect(wrapper.emitted('success')).toHaveLength(1)
    expect(wrapper.emitted('close')).toHaveLength(1)
    expect(showSuccess).toHaveBeenCalledTimes(1)
  })

  it('does not render a stale temporary grant into a reopened user session', async () => {
    const oldRequest = deferred<any>()
    const currentRequest = deferred<any>()
    const nextUser = { ...user, id: 10, email: 'user10@example.com' } as const
    grantTemporaryCredit
      .mockReturnValueOnce(oldRequest.promise)
      .mockReturnValueOnce(currentRequest.promise)
    const wrapper = mountModal()
    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.00')
    await wrapper.get('#balance-form').trigger('submit')

    await wrapper.setProps({ show: false })
    await wrapper.setProps({ user: nextUser as any })
    await wrapper.setProps({ show: true })
    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue('1.00')
    await wrapper.get('#balance-form').trigger('submit')

    oldRequest.resolve({
      temporary_credit_grant_id: 18,
      amount: '1.00000000',
      remaining_amount: '1.00000000',
      expires_at: '2026-07-17T16:00:00Z',
      notes: '',
    })
    await flushPromises()
    expect(wrapper.find('[data-testid="temporary-grant-result"]').exists()).toBe(false)
    expect(wrapper.emitted('success')).toBeUndefined()

    currentRequest.resolve({
      temporary_credit_grant_id: 19,
      amount: '1.00000000',
      remaining_amount: '1.00000000',
      expires_at: '2026-07-18T16:00:00Z',
      notes: '',
    })
    await flushPromises()

    expect(grantTemporaryCredit.mock.calls.map(call => call[0])).toEqual([9, 10])
    expect(grantTemporaryCredit.mock.calls[1]?.[2]).not.toBe(grantTemporaryCredit.mock.calls[0]?.[2])
    expect(wrapper.get('[data-testid="temporary-grant-result"]').text()).toContain(
      '2026-07-18T16:00:00Z',
    )
    expect(wrapper.emitted('success')).toHaveLength(1)
  })

  it.each(['1e-8', '1.123456789'])(
    'rejects temporary credit text outside the strict decimal contract: %s',
    async invalidAmount => {
    const wrapper = mountModal()
    await wrapper.get('[data-testid="credit-type-temporary"]').trigger('click')
    await wrapper.get('[data-testid="balance-amount"]').setValue(invalidAmount)
    await wrapper.get('#balance-form').trigger('submit')

    expect(grantTemporaryCredit).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('checkin.admin.invalidTemporaryAmount')
    },
  )

  it.each(['1e-8', '1.123456789'])(
    'rejects permanent balance text outside the same decimal contract: %s',
    async invalidAmount => {
      const wrapper = mountModal()
      await wrapper.get('[data-testid="balance-amount"]').setValue(invalidAmount)
      await wrapper.get('#balance-form').trigger('submit')

      expect(updateBalance).not.toHaveBeenCalled()
      expect(showError).toHaveBeenCalledWith('admin.users.amountRequired')
    },
  )
})
