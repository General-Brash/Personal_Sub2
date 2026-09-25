<template>
  <template v-if="shouldRender">
    <button
      type="button"
      class="flex items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-sm font-medium text-gray-600 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:text-dark-400 dark:hover:bg-dark-800 dark:hover:text-white"
      :aria-label="t('quickJump.title')"
      @click="open = true"
    >
      <Icon name="link" size="sm" />
      <span class="hidden sm:inline">{{ t('quickJump.title') }}</span>
    </button>

    <BaseDialog
      :show="open"
      :title="t('quickJump.title')"
      width="normal"
      close-on-click-outside
      @close="open = false"
    >
      <div v-if="visibleItems.length > 0" class="grid grid-cols-2 gap-3 sm:grid-cols-3">
        <a
          v-for="item in visibleItems"
          :key="item.id"
          :href="item.href"
          target="_blank"
          rel="noopener noreferrer"
          class="flex flex-col items-center gap-2 rounded-xl border border-gray-200 p-4 text-center transition-colors hover:border-primary-400 hover:bg-primary-50 dark:border-dark-600 dark:hover:border-primary-500 dark:hover:bg-dark-700"
          @click="open = false"
        >
          <span
            v-if="item.iconSvg"
            class="h-6 w-6 text-gray-600 dark:text-dark-300 [&>svg]:h-full [&>svg]:w-full"
            v-html="item.iconSvg"
          ></span>
          <Icon v-else name="externalLink" size="md" class="text-gray-600 dark:text-dark-300" />
          <span class="line-clamp-2 break-all text-sm font-medium text-gray-800 dark:text-gray-200">
            {{ item.label }}
          </span>
        </a>
      </div>
      <p v-else class="py-6 text-center text-sm text-gray-500 dark:text-dark-400">
        {{ t('quickJump.empty') }}
      </p>

      <template v-if="isAdmin" #footer>
        <button type="button" class="btn btn-secondary" @click="goToSettings">
          <Icon name="cog" size="sm" />
          <span>{{ t('quickJump.manage') }}</span>
        </button>
      </template>
    </BaseDialog>
  </template>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { useAdminSettingsStore } from '@/stores/adminSettings'
import { sanitizeSvg } from '@/utils/sanitize'
import { sanitizeUrl } from '@/utils/url'
import { FeatureFlags, isFeatureFlagEnabled } from '@/utils/featureFlags'
import type { QuickJumpItem } from '@/types'

const { t } = useI18n()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const adminSettingsStore = useAdminSettingsStore()

const open = ref(false)
const isAdmin = computed(() => authStore.isAdmin)
const enabled = computed(() => isFeatureFlagEnabled(FeatureFlags.quickJump))

/**
 * 公开配置只下发 visibility=user 的条目（服务端已过滤），仅管理员可见的条目
 * 需要从管理端设置里取，语义与侧边栏自定义菜单一致。
 */
const rawItems = computed<QuickJumpItem[]>(() => {
  const publicItems = (appStore.cachedPublicSettings?.quick_jump_items ?? []).filter(
    (item) => item.visibility === 'user'
  )
  if (!isAdmin.value) return publicItems
  const adminItems = adminSettingsStore.quickJumpItems.filter((item) => item.visibility === 'admin')
  return [...publicItems, ...adminItems]
})

// 渲染前统一消毒：URL 只放行 http(s)（无效则丢弃该条），图标 SVG 过消毒后才允许 v-html
const visibleItems = computed(() =>
  rawItems.value
    .map((item) => ({
      id: item.id,
      label: item.label,
      href: sanitizeUrl(item.url),
      iconSvg: item.icon_svg ? sanitizeSvg(item.icon_svg) : '',
      sortOrder: item.sort_order ?? 0
    }))
    .filter((item) => item.href !== '')
    .sort((a, b) => a.sortOrder - b.sortOrder)
)

// 开关关闭或没有当前用户可见的安全条目时，顶栏入口完全不渲染。
const shouldRender = computed(() => enabled.value && visibleItems.value.length > 0)

function goToSettings() {
  open.value = false
  void router.push({ path: '/admin/settings', query: { tab: 'general', focus: 'quick-jump' } })
}
</script>
