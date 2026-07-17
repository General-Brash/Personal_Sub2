import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

const { getCheckinSettings, updateCheckinSettings, showError, showSuccess } = vi.hoisted(() => ({
  getCheckinSettings: vi.fn(),
  updateCheckinSettings: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    settings: { getCheckinSettings, updateCheckinSettings },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, showSuccess }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string, params?: Record<string, string | number>) =>
        params ? `${key}:${String(params.day ?? '')}` : key,
    }),
  }
})

import CheckinSettingsCard from '../CheckinSettingsCard.vue'

const settings = {
  enabled: false,
  max_reward_day: 2,
  reward_tiers: [
    { day: 1, amount: '0.00000001' },
    { day: 2, amount: '1.25000000' },
  ],
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

describe('CheckinSettingsCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    getCheckinSettings.mockResolvedValue(settings)
    updateCheckinSettings.mockImplementation(async (payload) => payload)
  })

  it('loads the enabled flag, maximum tier, and exact amount strings', async () => {
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    expect(getCheckinSettings).toHaveBeenCalledTimes(1)
    expect((wrapper.get('[data-testid="checkin-max-reward-day"]').element as HTMLInputElement).value).toBe('2')
    expect((wrapper.get('[data-testid="checkin-tier-1"]').element as HTMLInputElement).value).toBe('0.00000001')
    expect((wrapper.get('[data-testid="checkin-tier-2"]').element as HTMLInputElement).value).toBe('1.25000000')
  })

  it('keeps every reward amount as a string when extending and saving complete tiers', async () => {
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    await wrapper.get('[data-testid="checkin-max-reward-day"]').setValue('3')
    await wrapper.get('[data-testid="checkin-tier-3"]').setValue('0.00000101')
    await wrapper.get('[data-testid="save-checkin-settings"]').trigger('click')
    await flushPromises()

    expect(updateCheckinSettings).toHaveBeenCalledWith({
      enabled: false,
      max_reward_day: 3,
      reward_tiers: [
        { day: 1, amount: '0.00000001' },
        { day: 2, amount: '1.25000000' },
        { day: 3, amount: '0.00000101' },
      ],
    })
    expect(typeof updateCheckinSettings.mock.calls[0]?.[0]?.reward_tiers[2]?.amount).toBe('string')
    expect(showSuccess).toHaveBeenCalledWith('checkin.admin.settingsSaved')
  })

  it('accepts and saves a complete 365-day reward policy', async () => {
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    const maxRewardDayInput = wrapper.get('[data-testid="checkin-max-reward-day"]')
    expect(maxRewardDayInput.attributes('max')).toBe('365')

    await maxRewardDayInput.setValue('365')
    expect(wrapper.findAll('[data-testid^="checkin-tier-"]')).toHaveLength(365)
    expect((wrapper.get('[data-testid="checkin-tier-365"]').element as HTMLInputElement).value).toBe(
      '1.00000000',
    )

    await wrapper.get('[data-testid="save-checkin-settings"]').trigger('click')
    await flushPromises()

    expect(updateCheckinSettings).toHaveBeenCalledTimes(1)
    const payload = updateCheckinSettings.mock.calls[0]?.[0]
    expect(payload.max_reward_day).toBe(365)
    expect(payload.reward_tiers).toHaveLength(365)
    expect(payload.reward_tiers[364]).toEqual({ day: 365, amount: '1.00000000' })
  })

  it('preserves hidden tier amounts while editing 365 down to 36 and back to 364', async () => {
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    const maxRewardDayInput = wrapper.get('[data-testid="checkin-max-reward-day"]')
    await maxRewardDayInput.setValue('365')
    await wrapper.get('[data-testid="checkin-tier-37"]').setValue('0.00000037')
    await wrapper.get('[data-testid="checkin-tier-364"]').setValue('0.00000364')
    await wrapper.get('[data-testid="checkin-tier-365"]').setValue('0.00000365')

    await maxRewardDayInput.setValue('36')
    expect(wrapper.findAll('[data-testid^="checkin-tier-"]')).toHaveLength(36)
    expect(wrapper.find('[data-testid="checkin-tier-37"]').exists()).toBe(false)

    await maxRewardDayInput.setValue('364')
    expect(wrapper.findAll('[data-testid^="checkin-tier-"]')).toHaveLength(364)
    expect((wrapper.get('[data-testid="checkin-tier-37"]').element as HTMLInputElement).value).toBe(
      '0.00000037',
    )
    expect((wrapper.get('[data-testid="checkin-tier-364"]').element as HTMLInputElement).value).toBe(
      '0.00000364',
    )

    await wrapper.get('[data-testid="save-checkin-settings"]').trigger('click')
    await flushPromises()

    const payload = updateCheckinSettings.mock.calls[0]?.[0]
    expect(payload.max_reward_day).toBe(364)
    expect(payload.reward_tiers).toHaveLength(364)
    expect(payload.reward_tiers.every((tier, index) => tier.day === index + 1)).toBe(true)
    expect(payload.reward_tiers[36]).toEqual({ day: 37, amount: '0.00000037' })
    expect(payload.reward_tiers[363]).toEqual({ day: 364, amount: '0.00000364' })
    expect(payload.reward_tiers).not.toContainEqual({ day: 365, amount: '0.00000365' })
  })

  it('does not expand or truncate reward tiers for a huge maximum reward day', async () => {
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    const maxRewardDayInput = wrapper.get('[data-testid="checkin-max-reward-day"]')
    await maxRewardDayInput.setValue('1000000')

    expect((maxRewardDayInput.element as HTMLInputElement).value).toBe('1000000')
    expect(wrapper.findAll('[data-testid^="checkin-tier-"]')).toHaveLength(2)
  })

  it('rejects 366 before calling the API', async () => {
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    await wrapper.get('[data-testid="checkin-max-reward-day"]').setValue('366')
    await wrapper.get('[data-testid="save-checkin-settings"]').trigger('click')

    expect(updateCheckinSettings).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('checkin.admin.invalidMaxRewardDay')
  })

  it('rejects non-contract amount text before calling the API', async () => {
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    await wrapper.get('[data-testid="checkin-tier-1"]').setValue('1e-8')
    await wrapper.get('[data-testid="save-checkin-settings"]').trigger('click')

    expect(updateCheckinSettings).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('checkin.admin.invalidRewardAmount')
  })

  it('serializes deferred save attempts and freezes inputs until the response settles', async () => {
    const saveRequest = deferred<typeof settings>()
    updateCheckinSettings.mockReturnValueOnce(saveRequest.promise)
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    await wrapper.get('[data-testid="save-checkin-settings"]').trigger('click')

    const fieldset = wrapper.get('fieldset')
    const tierInput = wrapper.get('[data-testid="checkin-tier-1"]')
    expect(fieldset.attributes('disabled')).toBeDefined()
    expect((tierInput.element as HTMLInputElement).matches(':disabled')).toBe(true)

    await tierInput.trigger('keydown', { key: 'Enter' })
    await wrapper.get('[data-testid="checkin-max-reward-day"]').trigger('keydown', { key: 'Enter' })
    await wrapper.get('[data-testid="save-checkin-settings"]').trigger('click')
    expect(updateCheckinSettings).toHaveBeenCalledTimes(1)

    saveRequest.resolve(settings)
    await flushPromises()

    expect(fieldset.attributes('disabled')).toBeUndefined()
    expect(showSuccess).toHaveBeenCalledTimes(1)
  })

  it('shows a stable error state and retries loading', async () => {
    getCheckinSettings
      .mockRejectedValueOnce(new Error('offline'))
      .mockResolvedValueOnce(settings)
    const wrapper = mount(CheckinSettingsCard)
    await flushPromises()

    expect(wrapper.find('[data-testid="checkin-settings-error"]').exists()).toBe(true)
    await wrapper.get('[data-testid="checkin-settings-error"] button').trigger('click')
    await flushPromises()

    expect(getCheckinSettings).toHaveBeenCalledTimes(2)
    expect(wrapper.find('[data-testid="checkin-settings-error"]').exists()).toBe(false)
  })
})
