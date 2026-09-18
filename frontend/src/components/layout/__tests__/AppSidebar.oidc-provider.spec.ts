import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const sourcePath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const source = readFileSync(sourcePath, 'utf8')

describe('AppSidebar OIDC Provider navigation', () => {
  it('adds a dedicated Provider entry and keeps it behind the existing permission filter', () => {
    expect(source).toContain("{ path: '/admin/oidc-provider', label: t('admin.oidcProvider.nav'), icon: KeyIcon }")
    expect(source).toContain('return authStore.canAccessAdminPath(item.path) ? [item] : []')
  })
})