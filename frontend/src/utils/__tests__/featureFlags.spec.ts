import { beforeEach, describe, expect, it, vi } from 'vitest'

const appStore = vi.hoisted(() => ({
  cachedPublicSettings: null as Record<string, boolean> | null,
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => appStore,
}))

import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'

const pageVisibilityFlags = [
  FeatureFlags.userChannelStatus,
  FeatureFlags.mall,
  FeatureFlags.userSubscriptions,
  FeatureFlags.adminSubscriptions,
  FeatureFlags.adminPromoCodes,
  FeatureFlags.adminChannelManagement,
]

describe('page visibility feature flags', () => {
  beforeEach(() => {
    appStore.cachedPublicSettings = null
  })

  it('uses the exact backend keys and opt-out semantics', () => {
    expect(pageVisibilityFlags.map(({ key, mode }) => ({ key, mode }))).toEqual([
      { key: 'user_channel_status_enabled', mode: 'opt-out' },
      { key: 'mall_enabled', mode: 'opt-out' },
      { key: 'user_subscriptions_enabled', mode: 'opt-out' },
      { key: 'admin_subscriptions_enabled', mode: 'opt-out' },
      { key: 'admin_promo_codes_enabled', mode: 'opt-out' },
      { key: 'admin_channel_management_enabled', mode: 'opt-out' },
    ])
  })

  it.each(pageVisibilityFlags)('$key defaults to enabled before settings load', (flag) => {
    expect(isFeatureFlagEnabled(flag)).toBe(true)
  })

  it.each(pageVisibilityFlags)('$key is disabled only by an explicit false', (flag) => {
    appStore.cachedPublicSettings = { [flag.key]: false }

    expect(isFeatureFlagEnabled(flag)).toBe(false)
  })
})
