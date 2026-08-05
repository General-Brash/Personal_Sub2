import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const dir = dirname(fileURLToPath(import.meta.url))
const headerSource = readFileSync(resolve(dir, '../AppHeader.vue'), 'utf8')
const authStoreSource = readFileSync(resolve(dir, '../../../stores/auth.ts'), 'utf8')

describe('AppHeader balance display', () => {
  it('uses separate permanent and current temporary credit fields with translation keys', () => {
    expect(headerSource).toContain("t('common.permanentBalance')")
    expect(headerSource).toContain("t('common.temporaryCreditAvailable')")
    expect(headerSource).toContain("t('common.frozenBalance')")
    expect(headerSource).toContain("t('common.totalBalance')")
    expect(headerSource).toContain("temporaryCreditAvailable = computed(() => Number(user.value?.temporary_credit_available || 0))")
    expect(headerSource).not.toContain('?'.repeat(4))
    expect(headerSource).not.toContain('?'.repeat(3))
  })

  it('keeps the required colors, two-decimal formatting, and frozen total semantics', () => {
    expect(headerSource).toContain('text-indigo-600 dark:text-indigo-400')
    expect(headerSource).toContain('text-emerald-600 dark:text-emerald-400')
    expect(headerSource).toContain('return `$${value.toFixed(2)}`')
    expect(headerSource).toContain('const totalBalance = computed(() => availableBalance.value + frozenBalance.value)')
  })

  it('reuses the auth store refresh interval without adding a header timer', () => {
    expect(authStoreSource).toContain('const AUTO_REFRESH_INTERVAL = 60 * 1000')
    expect(headerSource).not.toMatch(/setInterval|refreshUser/)
  })
})