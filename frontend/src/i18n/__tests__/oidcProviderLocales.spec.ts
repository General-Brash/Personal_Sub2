import { describe, expect, it } from 'vitest'
import en from '../locales/en'
import zh from '../locales/zh'
import enOidcProvider from '../locales/en/admin/oidcProvider'
import zhOidcProvider from '../locales/zh/admin/oidcProvider'

function leafKeys(value: unknown, prefix = ''): string[] {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return [prefix]
  return Object.entries(value).flatMap(([key, child]) => leafKeys(child, prefix ? `${prefix}.${key}` : key))
}

describe('OIDC Provider admin locales', () => {
  it('is registered in both admin locale indexes', () => {
    expect((en as { admin: { oidcProvider?: unknown } }).admin.oidcProvider).toBeDefined()
    expect((zh as { admin: { oidcProvider?: unknown } }).admin.oidcProvider).toBeDefined()
  })

  it('keeps the English and Chinese Provider key shapes aligned', () => {
    expect(leafKeys(enOidcProvider)).toEqual(leafKeys(zhOidcProvider))
  })
})