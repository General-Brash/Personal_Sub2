import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const sourcePath = resolve(dirname(fileURLToPath(import.meta.url)), '../auth.ts')
const source = readFileSync(sourcePath, 'utf8')

describe('OIDC Provider frontend permission mapping', () => {
  it('maps the Provider page through canAccessAdminPath and the dedicated read permissions', () => {
    expect(source).toContain("['/admin/oidc-provider', ['oidc.provider.read', 'oidc.clients.read', 'oidc.consents.read', 'oidc.keys.read', 'oidc.audit.read']]")
    expect(source).toContain('return match?.[1].some(canAdmin) === true')
  })
})