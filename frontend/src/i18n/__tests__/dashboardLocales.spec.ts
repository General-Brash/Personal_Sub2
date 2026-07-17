import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import zh from '../locales/zh'

describe('dashboard locales', () => {
  it('exposes the balance breakdown copy in English', () => {
    const messages = en as Record<string, any>

    expect(messages.dashboard.permanentBalance).toBe('Permanent balance')
    expect(messages.dashboard.temporaryCredit).toBe('Temporary credit')
  })

  it('exposes the balance breakdown copy in Chinese', () => {
    const messages = zh as Record<string, any>

    expect(messages.dashboard.permanentBalance).toBe('永久余额')
    expect(messages.dashboard.temporaryCredit).toBe('临时额度')
  })
})
