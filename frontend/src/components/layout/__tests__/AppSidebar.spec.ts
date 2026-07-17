import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../AppSidebar.vue')
const componentSource = readFileSync(componentPath, 'utf8')
const stylePath = resolve(dirname(fileURLToPath(import.meta.url)), '../../../style.css')
const styleSource = readFileSync(stylePath, 'utf8')

describe('AppSidebar custom SVG styles', () => {
  it('does not override uploaded SVG fill or stroke colors', () => {
    expect(componentSource).toContain('.sidebar-svg-icon {')
    expect(componentSource).toContain('color: currentColor;')
    expect(componentSource).toContain('display: block;')
    expect(componentSource).not.toContain('stroke: currentColor;')
    expect(componentSource).not.toContain('fill: none;')
  })
})

describe('AppSidebar scroll position persistence', () => {
  it('binds a template ref to the sidebar nav element', () => {
    expect(componentSource).toContain('ref="sidebarNavRef"')
    expect(componentSource).toContain('sidebar-nav')
  })

  it('declares sidebarNavRef in script setup', () => {
    expect(componentSource).toContain("const sidebarNavRef = ref<HTMLElement | null>(null)")
  })

  it('saves scroll position on beforeUnmount', () => {
    expect(componentSource).toContain('onBeforeUnmount')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('sidebarNavRef.value.scrollTop')
  })

  it('restores scroll position on mount', () => {
    expect(componentSource).toContain('onMounted')
    expect(componentSource).toContain('appStore.sidebarScrollTop')
    expect(componentSource).toContain('nextTick')
  })
})

describe('AppSidebar header styles', () => {
  it('does not clip the version badge dropdown', () => {
    const sidebarHeaderBlockMatch = styleSource.match(/\.sidebar-header\s*\{[\s\S]*?\n {2}\}/)
    const sidebarBrandBlockMatch = componentSource.match(/\.sidebar-brand\s*\{[\s\S]*?\n\}/)

    expect(sidebarHeaderBlockMatch).not.toBeNull()
    expect(sidebarBrandBlockMatch).not.toBeNull()
    expect(sidebarHeaderBlockMatch?.[0]).not.toContain('@apply overflow-hidden;')
    expect(sidebarBrandBlockMatch?.[0]).not.toContain('overflow: hidden;')
  })
})

describe('AppSidebar daily check-in navigation', () => {
  it('adds daily check-in to the shared user navigation builder for users and administrators', () => {
    expect(componentSource).toContain("{ path: '/check-in', label: t('checkin.title'), icon: GiftIcon }")
    expect(componentSource).toContain('buildSelfNavItems(true)')
    expect(componentSource).toContain('buildSelfNavItems(false)')
  })
})

describe('AppSidebar page visibility navigation', () => {
  it('binds user entries to their opt-out page flags', () => {
    expect(componentSource).toContain('const flagUserChannelStatus = makeSidebarFlag(FeatureFlags.userChannelStatus)')
    expect(componentSource).toContain('const flagUserSubscriptions = makeSidebarFlag(FeatureFlags.userSubscriptions)')
    expect(componentSource).toContain('const flagVisibleChannelStatus = () => flagChannelMonitor() && flagUserChannelStatus()')
    expect(componentSource).toContain("{ path: '/monitor', label: t('nav.channelStatus'), icon: SignalIcon, featureFlag: flagVisibleChannelStatus }")
    expect(componentSource).toContain("{ path: '/subscriptions', label: t('nav.mySubscriptions'), icon: CreditCardIcon, hideInSimpleMode: true, featureFlag: flagUserSubscriptions }")
  })

  it('hides the whole channel group and the promo-code entry for administrators', () => {
    expect(componentSource).toContain('const flagAdminChannelManagement = makeSidebarFlag(FeatureFlags.adminChannelManagement)')
    expect(componentSource).toContain('const flagAdminPromoCodes = makeSidebarFlag(FeatureFlags.adminPromoCodes)')

    const channelGroupStart = componentSource.indexOf("path: '/admin/channels'")
    const channelGroupEnd = componentSource.indexOf("path: '/admin/subscriptions'", channelGroupStart)
    const channelGroup = componentSource.slice(channelGroupStart, channelGroupEnd)
    expect(channelGroup).toContain('featureFlag: flagAdminChannelManagement')
    expect(channelGroup).toContain("path: '/admin/channels/monitor'")
    expect(channelGroup).toContain('featureFlag: flagChannelMonitor')

    expect(componentSource).toContain("{ path: '/admin/promo-codes', label: t('nav.promoCodes'), icon: GiftIcon, hideInSimpleMode: true, featureFlag: flagAdminPromoCodes }")
  })
})
