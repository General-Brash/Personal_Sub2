import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { getTemporaryCredits, getUserBalanceHistory } = vi.hoisted(() => ({
  getTemporaryCredits: vi.fn(),
  getUserBalanceHistory: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    users: { getTemporaryCredits, getUserBalanceHistory },
  },
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
    props: ['show', 'title', 'width'],
    emits: ['close'],
    template: '<div v-if="show"><slot /><slot name="footer" /></div>',
  },
}))

import UserBalanceHistoryModal from '../UserBalanceHistoryModal.vue'

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

const auditItem = {
  id: 18,
  user_id: 9,
  source: 'admin_grant',
  checkin_id: null,
  amount: '1.25000000',
  remaining_amount: '0.25000000',
  available_at: '2026-07-16T00:00:00Z',
  expires_at: '2026-07-17T16:00:00Z',
  status: 'active' as const,
  notes: 'support credit',
  granted_by: 3,
  created_at: '2026-07-16T03:00:00Z',
  updated_at: '2026-07-16T04:00:00Z',
}

function deferred<T>() {
  let resolve!: (value: T | PromiseLike<T>) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((promiseResolve, promiseReject) => {
    resolve = promiseResolve
    reject = promiseReject
  })
  return { promise, resolve, reject }
}

async function mountAndOpen() {
  const wrapper = mount(UserBalanceHistoryModal, {
    props: { show: false, user: user as any },
    global: { stubs: { Icon: true, Select: true } },
  })
  await wrapper.setProps({ show: true })
  await flushPromises()
  return wrapper
}

describe('UserBalanceHistoryModal', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getTemporaryCredits.mockResolvedValue({
      items: [auditItem],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getUserBalanceHistory.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 15,
      pages: 1,
      total_recharged: 0,
    })
  })

  it('opens on the temporary audit and renders all frozen audit fields', async () => {
    const wrapper = await mountAndOpen()

    expect(getTemporaryCredits).toHaveBeenCalledWith(9, 1, 20)
    expect(getUserBalanceHistory).not.toHaveBeenCalled()
    const audit = wrapper.get('[data-testid="temporary-credit-audit-list"]').text()
    expect(audit).toContain('checkin.admin.sourceAdminGrant')
    expect(audit).toContain('1.25')
    expect(audit).toContain('0.25')
    expect(audit).not.toContain('1.25000000')
    expect(audit).toContain('support credit')
    expect(audit).toContain('#3')
    expect(audit).toContain('#18')
    expect(wrapper.get('[data-testid="temporary-credit-status-18"]').text()).toContain('checkin.admin.temporaryStatus.active')
    expect(audit).toContain('checkin.admin.availableAtLabel')
  })

  it('renders all four authoritative temporary-credit statuses without deriving them client-side', async () => {
    const statuses = ['unused', 'active', 'depleted', 'expired'] as const
    getTemporaryCredits.mockResolvedValue({
      items: statuses.map((status, index) => ({
        ...auditItem,
        id: 30 + index,
        status,
      })),
      total: statuses.length,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountAndOpen()

    for (const [index, status] of statuses.entries()) {
      expect(wrapper.get(`[data-testid="temporary-credit-status-${30 + index}"]`).text()).toContain(
        `checkin.admin.temporaryStatus.${status}`,
      )
    }
  })

  it('localizes bank-generated temporary-credit sources', async () => {
    getTemporaryCredits.mockResolvedValue({
      items: [
        { ...auditItem, id: 40, source: 'bank_advance' },
        { ...auditItem, id: 41, source: 'bank_exchange' },
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = await mountAndOpen()
    const audit = wrapper.get('[data-testid="temporary-credit-audit-list"]').text()

    expect(audit).toContain('checkin.admin.sourceBankAdvance')
    expect(audit).toContain('checkin.admin.sourceBankExchange')
    expect(audit).not.toContain('bank_advance')
    expect(audit).not.toContain('bank_exchange')
  })

  it.each([
    ['mall_product', 'checkin.admin.sourceMallProduct'],
    ['subscription', 'checkin.admin.sourceSubscription'],
  ])('localizes %s temporary-credit source', async (source, key) => {
    getTemporaryCredits.mockResolvedValue({
      items: [{ ...auditItem, id: source === 'mall_product' ? 42 : 43, source }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = await mountAndOpen()
    expect(wrapper.get('[data-testid="temporary-credit-audit-list"]').text()).toContain(key)
    expect(wrapper.get('[data-testid="temporary-credit-audit-list"]').text()).not.toContain(source)
  })

  it('keeps permanent history on a separate request and view', async () => {
    getUserBalanceHistory.mockResolvedValueOnce({
      items: [{
        id: 27,
        code: 'balance-code',
        type: 'admin_balance',
        value: 1.235,
        status: 'used',
        used_by: 9,
        used_at: '2026-07-16T05:00:00Z',
        created_at: '2026-07-16T05:00:00Z',
        group_id: null,
        validity_days: 0,
        notes: 'rounded display',
      }],
      total: 1,
      page: 1,
      page_size: 15,
      pages: 1,
      total_recharged: 2.345,
    })
    const wrapper = await mountAndOpen()
    await wrapper.get('[data-testid="history-view-permanent"]').trigger('click')
    await flushPromises()

    expect(getUserBalanceHistory).toHaveBeenCalledWith(9, 1, 15, undefined)
    expect(wrapper.find('[data-testid="temporary-credit-audit-list"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('+$1.24')
    expect(wrapper.text()).toContain('$2.35')
  })

  it('paginates temporary audit using the dedicated endpoint', async () => {
    getTemporaryCredits.mockResolvedValue({
      items: [auditItem],
      total: 21,
      page: 1,
      page_size: 20,
      pages: 2,
    })
    const wrapper = await mountAndOpen()
    const next = wrapper.findAll('button').find((button) => button.text() === 'pagination.next')
    expect(next).toBeDefined()
    await next?.trigger('click')
    await flushPromises()

    expect(getTemporaryCredits).toHaveBeenLastCalledWith(9, 2, 20)
  })

  it('restores the temporary audit page after switching views', async () => {
    getTemporaryCredits.mockImplementation(async (_userID, page) => ({
      items: [{ ...auditItem, id: page === 1 ? 18 : 19 }],
      total: 21,
      page,
      page_size: 20,
      pages: 2,
    }))
    const wrapper = await mountAndOpen()

    const next = wrapper.findAll('button').find((button) => button.text() === 'pagination.next')
    await next?.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('2 / 2')

    await wrapper.get('[data-testid="history-view-permanent"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-view-temporary"]').trigger('click')
    await flushPromises()

    expect(getUserBalanceHistory).toHaveBeenCalledWith(9, 1, 15, undefined)
    expect(getTemporaryCredits).toHaveBeenLastCalledWith(9, 2, 20)
    expect(wrapper.text()).toContain('2 / 2')
  })

  it('restores the permanent history page after switching views', async () => {
    getUserBalanceHistory.mockImplementation(async (_userID, page) => ({
      items: [],
      total: 31,
      page,
      page_size: 15,
      pages: 3,
      total_recharged: 0,
    }))
    const wrapper = await mountAndOpen()

    await wrapper.get('[data-testid="history-view-permanent"]').trigger('click')
    await flushPromises()
    const next = wrapper.findAll('button').find((button) => button.text() === 'pagination.next')
    await next?.trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('2 / 3')

    await wrapper.get('[data-testid="history-view-temporary"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-view-permanent"]').trigger('click')
    await flushPromises()

    expect(getTemporaryCredits).toHaveBeenLastCalledWith(9, 1, 20)
    expect(getUserBalanceHistory).toHaveBeenLastCalledWith(9, 2, 15, undefined)
    expect(wrapper.text()).toContain('2 / 3')
  })

  it('discards an older response when the same temporary page is requested again', async () => {
    getTemporaryCredits.mockResolvedValueOnce({
      items: [auditItem],
      total: 21,
      page: 1,
      page_size: 20,
      pages: 2,
    })
    const olderPageTwo = deferred<{
      items: (typeof auditItem)[]
      total: number
      page: number
      page_size: number
      pages: number
    }>()
    const newerPageTwo = deferred<{
      items: (typeof auditItem)[]
      total: number
      page: number
      page_size: number
      pages: number
    }>()
    getTemporaryCredits
      .mockReturnValueOnce(olderPageTwo.promise)
      .mockReturnValueOnce(newerPageTwo.promise)
    const wrapper = await mountAndOpen()

    const next = wrapper.findAll('button').find((button) => button.text() === 'pagination.next')
    await next?.trigger('click')
    expect(getTemporaryCredits).toHaveBeenLastCalledWith(9, 2, 20)

    await wrapper.get('[data-testid="history-view-permanent"]').trigger('click')
    await flushPromises()
    await wrapper.get('[data-testid="history-view-temporary"]').trigger('click')
    expect(getTemporaryCredits).toHaveBeenLastCalledWith(9, 2, 20)
    expect(wrapper.find('[data-testid="history-loading"]').exists()).toBe(true)

    newerPageTwo.resolve({
      items: [{ ...auditItem, id: 20, notes: 'new page two' }],
      total: 41,
      page: 2,
      page_size: 20,
      pages: 3,
    })
    await flushPromises()

    expect(wrapper.get('[data-testid="temporary-credit-audit-list"]').text()).toContain('new page two')
    expect(wrapper.text()).toContain('2 / 3')
    expect(wrapper.find('[data-testid="history-loading"]').exists()).toBe(false)

    olderPageTwo.resolve({
      items: [{ ...auditItem, id: 19, notes: 'old page two' }],
      total: 21,
      page: 2,
      page_size: 20,
      pages: 2,
    })
    await flushPromises()

    const audit = wrapper.get('[data-testid="temporary-credit-audit-list"]').text()
    expect(audit).toContain('new page two')
    expect(audit).not.toContain('old page two')
    expect(wrapper.text()).toContain('2 / 3')
    expect(wrapper.find('[data-testid="history-loading"]').exists()).toBe(false)
  })

  it('isolates loading and errors when view requests settle out of order', async () => {
    const permanentRequest = deferred<{
      items: never[]
      total: number
      page: number
      page_size: number
      pages: number
      total_recharged: number
    }>()
    const temporaryRequest = deferred<{
      items: (typeof auditItem)[]
      total: number
      page: number
      page_size: number
      pages: number
    }>()
    const wrapper = await mountAndOpen()
    getUserBalanceHistory.mockReturnValueOnce(permanentRequest.promise)
    getTemporaryCredits.mockReturnValueOnce(temporaryRequest.promise)

    await wrapper.get('[data-testid="history-view-permanent"]').trigger('click')
    await wrapper.get('[data-testid="history-view-temporary"]').trigger('click')
    permanentRequest.reject(new Error('permanent history failed'))
    await flushPromises()

    expect(wrapper.find('[data-testid="history-error"]').exists()).toBe(false)
    expect(wrapper.find('[data-testid="history-loading"]').exists()).toBe(true)

    temporaryRequest.resolve({
      items: [auditItem],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    await flushPromises()

    expect(wrapper.find('[data-testid="history-loading"]').exists()).toBe(false)
    expect(wrapper.get('[data-testid="temporary-credit-audit-list"]').text()).toContain('support credit')
  })

  it('renders a stable empty state', async () => {
    getTemporaryCredits.mockResolvedValue({
      items: [], total: 0, page: 1, page_size: 20, pages: 0,
    })
    const wrapper = await mountAndOpen()

    expect(wrapper.get('[data-testid="history-empty"]').text()).toContain('checkin.admin.noTemporaryCredits')
  })

  it('renders an error state and retries the same audit page', async () => {
    getTemporaryCredits
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce({ items: [auditItem], total: 1, page: 1, page_size: 20, pages: 1 })
    const wrapper = await mountAndOpen()

    expect(wrapper.find('[data-testid="history-error"]').exists()).toBe(true)
    await wrapper.get('[data-testid="history-error"] button').trigger('click')
    await flushPromises()

    expect(getTemporaryCredits).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="history-error"]').exists()).toBe(false)
  })
})
