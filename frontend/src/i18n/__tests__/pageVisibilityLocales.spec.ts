import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('page visibility locales', () => {
  it('exposes complete English copy', () => {
    const messages = en as Record<string, any>

    expect(messages.admin.settings.features.pageVisibility).toMatchObject({
      title: 'Page Visibility',
      userPages: 'User Pages',
      adminPages: 'Administrator Pages',
    })
    expect(messages.admin.settings.features.pageVisibility.userChannelStatus.label).toBe('Channel Status')
    expect(messages.admin.settings.features.pageVisibility.userSubscriptions.label).toBe('My Subscriptions')
    expect(messages.admin.settings.features.pageVisibility.adminPromoCodes.label).toBe('Promo Code Management')
    expect(messages.admin.settings.features.pageVisibility.adminChannelManagement.label).toBe('Channel Management')
    expect(messages.common.pageDisabledByAdmin).toBe('This page has been disabled by an administrator.')
  })

  it('exposes complete Chinese copy', () => {
    const messages = zh as Record<string, any>

    expect(messages.admin.settings.features.pageVisibility).toMatchObject({
      title: '页面可见性',
      userPages: '用户页面',
      adminPages: '管理员页面',
    })
    expect(messages.admin.settings.features.pageVisibility.userChannelStatus.label).toBe('渠道状态')
    expect(messages.admin.settings.features.pageVisibility.userSubscriptions.label).toBe('我的订阅')
    expect(messages.admin.settings.features.pageVisibility.adminPromoCodes.label).toBe('优惠码管理')
    expect(messages.admin.settings.features.pageVisibility.adminChannelManagement.label).toBe('渠道管理')
    expect(messages.common.pageDisabledByAdmin).toBe('该页面已由管理员关闭')
  })
})
