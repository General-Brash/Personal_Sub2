import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

import en from '@/i18n/locales/en'
import zh from '@/i18n/locales/zh'

const here = dirname(fileURLToPath(import.meta.url))
const read = (path: string) => readFileSync(resolve(here, path), 'utf8')

describe('secondary review integration surface', () => {
  it('registers an admin-only risk-control route and navigation child', () => {
    const router = read('../../../router/index.ts')
    const routeStart = router.indexOf("path: '/admin/secondary-review'")
    const route = router.slice(routeStart, router.indexOf("path: '/admin/usage'", routeStart))
    expect(routeStart).toBeGreaterThan(-1)
    expect(route).toContain('requiresAuth: true')
    expect(route).toContain('requiresAdmin: true')
    expect(route).toContain('requiresRiskControl: true')

    const sidebar = read('../../../components/layout/AppSidebar.vue')
    const group = sidebar.slice(sidebar.indexOf("path: '/admin/security-audit'"), sidebar.indexOf("path: '/admin/redeem'"))
    expect(group).toContain("path: '/admin/secondary-review'")
    expect(group).toContain("t('nav.secondaryReview')")
  })

  it('keeps Chinese and English locale contracts symmetric', () => {
    expect(Object.keys(zh.admin.secondaryReview)).toEqual(Object.keys(en.admin.secondaryReview))
    expect(zh.nav.secondaryReview).toBeTruthy()
    expect(en.nav.secondaryReview).toBeTruthy()
  })
})
