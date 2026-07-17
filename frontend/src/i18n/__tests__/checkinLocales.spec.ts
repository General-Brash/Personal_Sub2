import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('check-in locales', () => {
  it('exposes the user-facing check-in copy in English', () => {
    const messages = en as Record<string, any>

    expect(messages.checkin).toMatchObject({
      title: 'Daily Check-in',
      checkIn: 'Check in',
      previousMonth: 'Previous month',
      nextMonth: 'Next month',
    })
    expect(messages.checkin.admin.temporaryCredit).toBe('Temporary credit')
    expect(messages.checkin.admin.actualExpiresAt).toContain('UTC')
    expect(messages.checkin.admin.invalidMaxRewardDay).toContain('1 and 365')
    expect(messages.nav.checkIn).toBe('Daily Check-in')
  })

  it('exposes the user-facing check-in copy in Chinese', () => {
    const messages = zh as Record<string, any>

    expect(messages.checkin).toMatchObject({
      title: '每日签到',
      checkIn: '立即签到',
      previousMonth: '上个月',
      nextMonth: '下个月',
    })
    expect(messages.checkin.admin.temporaryCredit).toBe('临时额度')
    expect(messages.checkin.admin.expiresAt).toBe('次日北京时间 00:00 过期')
    expect(messages.checkin.admin.invalidMaxRewardDay).toContain('1 到 365')
    expect(messages.nav.checkIn).toBe('每日签到')
  })
})
