import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'

import UserTokenRanking from '../UserTokenRanking.vue'

const getUserBreakdown = vi.fn()

vi.mock('@/api/admin/dashboard', () => ({
  getUserBreakdown: (...args: unknown[]) => getUserBreakdown(...args),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const item = (id: number, tokens: number) => ({
  user_id: id,
  email: `u${id}@test.com`,
  requests: 1,
  input_tokens: tokens,
  output_tokens: 0,
  cache_tokens: 0,
  total_tokens: tokens,
  actual_cost: 0.5,
})

const mountRanking = (props: Record<string, unknown> = {}) =>
  mount(UserTokenRanking, {
    props: {
      startDate: '2026-07-01',
      endDate: '2026-07-08',
      filters: {},
      ...props,
    },
    global: { stubs: { Select: true, LoadingSpinner: true } },
  })

describe('UserTokenRanking', () => {
  beforeEach(() => {
    getUserBreakdown.mockReset()
    getUserBreakdown.mockResolvedValue({ users: [item(1, 100), item(2, 50)] })
  })

  it('loads on mount with shared filters and emits select-user with id + email on row click', async () => {
    const wrapper = mountRanking({ filters: { group_id: 3 }, model: 'claude-fable-5' })
    await flushPromises()

    expect(getUserBreakdown).toHaveBeenCalledWith(expect.objectContaining({
      group_id: 3,
      model: 'claude-fable-5',
      start_date: '2026-07-01',
      end_date: '2026-07-08',
      sort_by: 'total_tokens',
      limit: 50,
    }), { signal: expect.any(AbortSignal) })

    const rows = wrapper.findAll('tbody tr')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('$0.50')

    await rows[0].trigger('click')
    expect(wrapper.emitted('select-user')![0]).toEqual([1, 'u1@test.com'])
  })

  it('reloads when shared filters change', async () => {
    const wrapper = mountRanking()
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledTimes(1)

    await wrapper.setProps({ filters: { user_id: 9 } })
    await flushPromises()

    expect(getUserBreakdown).toHaveBeenCalledTimes(2)
    expect(getUserBreakdown).toHaveBeenLastCalledWith(
      expect.objectContaining({ user_id: 9 }),
      { signal: expect.any(AbortSignal) },
    )
  })

  it('defers inactive requests and loads the latest filters when activated', async () => {
    const wrapper = mountRanking({ active: false })
    await flushPromises()
    expect(getUserBreakdown).not.toHaveBeenCalled()

    await wrapper.setProps({ filters: { user_id: 9 } })
    await flushPromises()
    expect(getUserBreakdown).not.toHaveBeenCalled()

    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledTimes(1)
    expect(getUserBreakdown).toHaveBeenCalledWith(
      expect.objectContaining({ user_id: 9 }),
      { signal: expect.any(AbortSignal) },
    )
    wrapper.unmount()
  })

  it('aborts inactive requests and ignores their stale results after reactivation', async () => {
    let resolveFirst!: (value: { users: ReturnType<typeof item>[] }) => void
    getUserBreakdown.mockReturnValueOnce(new Promise((resolve) => { resolveFirst = resolve }))
    const wrapper = mountRanking()
    await flushPromises()
    const firstSignal = getUserBreakdown.mock.calls[0][1].signal as AbortSignal

    await wrapper.setProps({ active: false })
    expect(firstSignal.aborted).toBe(true)
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(getUserBreakdown).toHaveBeenCalledTimes(2)

    resolveFirst({ users: [item(99, 999)] })
    await flushPromises()
    expect(wrapper.findAll('tbody tr')).toHaveLength(2)
    expect(wrapper.text()).not.toContain('u99@test.com')
    wrapper.unmount()
  })
})
