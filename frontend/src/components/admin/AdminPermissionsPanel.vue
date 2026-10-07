<template>
  <BaseDialog :show="show" title="管理员权限" width="wide" @close="$emit('close')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2 text-sm">
        <p class="text-gray-500 dark:text-dark-400">显式 deny 优先；空 scope 不再视为无限权限，全局授权必须使用 {&quot;*&quot;:&quot;*&quot;}。</p>
        <span :class="canEditAny ? 'text-emerald-600' : 'text-amber-600'" class="font-medium">
          {{ canEditAny ? '可写' : '只读' }}
        </span>
      </div>
      <div
        v-if="modeDisabled"
        data-testid="permission-mode-disabled-notice"
        class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200"
      >
        当前 ADMIN_PERMISSIONS_MODE=disabled，除 OIDC 外的授权记录暂不生效
      </div>
      <div v-if="!canWrite" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        {{ readOnlyReason }}
      </div>
      <div
        v-if="isSelf"
        data-testid="permission-self-hint"
        class="rounded-lg bg-blue-50 p-3 text-sm text-blue-800 dark:bg-blue-900/20 dark:text-blue-200"
      >
        {{ isSelfSuperAdmin ? '这是你自己的账号：超管默认拥有除 OIDC 外的全部权限，只能为自己授予 / 撤销 OIDC 权限。' : '不能修改自己的管理员权限。' }}
      </div>
      <label v-if="canEditAny" class="block text-sm">
        <span class="mb-1 block text-gray-600 dark:text-dark-300">操作原因（审计必填）</span>
        <input v-model.trim="reason" data-testid="permission-reason" class="input w-full" placeholder="说明本次授权/撤销原因" :disabled="busy" />
      </label>
      <div v-if="busy && progress.total > 0" data-testid="permission-batch-progress" class="text-sm text-gray-600 dark:text-dark-300">
        进度 {{ progress.done }} / {{ progress.total }}
      </div>
      <div
        v-if="notice"
        data-testid="permission-batch-notice"
        class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200"
      >
        {{ notice }}
      </div>
      <div
        v-if="failures.length"
        data-testid="permission-batch-failures"
        class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-300"
      >
        <p>以下 {{ failures.length }} 项失败：</p>
        <ul class="mt-1 list-disc space-y-0.5 pl-5">
          <li v-for="failure in failures" :key="failure.permission">{{ failure.permission }}：{{ failure.message }}</li>
        </ul>
      </div>
      <div v-if="loading" class="py-8 text-center text-sm text-gray-500">加载中...</div>
      <div v-else class="max-h-[60vh] space-y-3 overflow-y-auto">
        <section
          v-for="section in sections"
          :key="section.resource"
          :data-testid="`permission-section-${section.resource}`"
          class="rounded-lg border border-gray-200 dark:border-dark-700"
        >
          <div class="flex flex-wrap items-center justify-between gap-2 px-3 py-2">
            <button
              type="button"
              class="flex items-center gap-2 text-left"
              :data-testid="`permission-section-toggle-${section.resource}`"
              :aria-expanded="expanded[section.resource] === true"
              @click="expanded[section.resource] = !expanded[section.resource]"
            >
              <Icon
                name="chevronDown"
                size="sm"
                :class="['text-gray-400 transition-transform', expanded[section.resource] ? '' : '-rotate-90']"
              />
              <span class="font-medium text-gray-900 dark:text-white">{{ section.title }}</span>
              <span class="text-xs text-gray-500 dark:text-dark-400">
                已允许 {{ section.allowed }} / 总数 {{ section.items.length }}{{ section.denied > 0 ? ` · 拒绝 ${section.denied}` : '' }}
              </span>
            </button>
            <div class="space-x-2 whitespace-nowrap">
              <button
                v-for="action in BATCH_ACTIONS"
                :key="action"
                type="button"
                :data-testid="`permission-batch-${action}-${section.resource}`"
                :disabled="!canBatch(section, action)"
                :class="['btn btn-secondary', action === 'revoke' ? 'text-red-600' : '']"
                @click="requestBatch(section, action)"
              >
                {{ BATCH_LABELS[action] }}
              </button>
            </div>
          </div>
          <p v-if="section.resource === 'oidc'" data-testid="permission-oidc-note" class="px-3 pb-2 text-xs text-gray-500 dark:text-dark-400">
            OIDC 权限必须逐条显式授予，不接受通配，也不给超管自动放行；超管可为自己授予（需二次验证）。
          </p>
          <table v-if="expanded[section.resource]" class="min-w-full text-sm">
            <thead>
              <tr class="border-y text-left text-gray-500 dark:border-dark-700">
                <th class="px-3 py-2">权限</th>
                <th class="py-2">效果</th>
                <th class="py-2">范围</th>
                <th class="py-2">操作</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="item in section.items" :key="item.permission" class="border-b border-gray-100 last:border-b-0 dark:border-dark-700">
                <td class="px-3 py-2">
                  <div class="font-medium text-gray-900 dark:text-white">{{ item.permission }}</div>
                  <div class="text-xs text-gray-500">{{ item.description }}{{ item.sensitive ? ' · 敏感' : '' }}</div>
                </td>
                <td class="py-2"><span class="badge">{{ grantOf(item.permission)?.effect || '未授予' }}</span></td>
                <td class="py-2 pr-3">
                  <input
                    v-if="canEditSection(section.resource)"
                    v-model.trim="scopeDrafts[item.permission]"
                    class="input w-full min-w-48 font-mono text-xs"
                    :aria-label="`${item.permission} scope`"
                  />
                  <code v-else class="text-xs">{{ JSON.stringify(grantOf(item.permission)?.scope || {}) }}</code>
                </td>
                <td class="space-x-2 whitespace-nowrap py-2 pr-3">
                  <button :disabled="!canOperate(section.resource)" class="btn btn-secondary" @click="toggle(item, 'allow')">Allow</button>
                  <button :disabled="!canOperate(section.resource)" class="btn btn-secondary" @click="toggle(item, 'deny')">Deny</button>
                  <button :disabled="!canOperate(section.resource) || !grantOf(item.permission)" class="btn btn-secondary text-red-600" @click="revoke(item)">撤销</button>
                </td>
              </tr>
            </tbody>
          </table>
        </section>
      </div>
    </div>

    <ConfirmDialog
      :show="pendingBatch !== null"
      :title="confirmTitle"
      :message="confirmMessage"
      confirm-text="确认执行"
      cancel-text="取消"
      :danger="pendingBatch?.action === 'allow'"
      @confirm="confirmBatch"
      @cancel="pendingBatch = null"
    >
      <div v-if="pendingSensitive.length" class="text-sm text-gray-700 dark:text-gray-300">
        <p>本批包含以下敏感权限：</p>
        <ul class="mt-1 list-disc pl-5 font-mono text-xs text-red-600 dark:text-red-400">
          <li v-for="permission in pendingSensitive" :key="permission">{{ permission }}</li>
        </ul>
      </div>
    </ConfirmDialog>
  </BaseDialog>

  <!-- 授权/撤销接口始终要求 step-up：后端返回 STEP_UP_REQUIRED 时弹出 TOTP 验证并重试 -->
  <TotpStepUpDialog :controller="stepUp" />
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'
import { useAuthStore } from '@/stores/auth'
import {
  getUserAdminPermissions,
  listAdminPermissionCatalog,
  grantUserAdminPermission,
  revokeUserAdminPermission,
  type AdminPermissionDefinition,
  type AdminPermissionGrant,
  type AdminUserPermissionState,
} from '@/api/adminPermissions'

type BatchAction = 'allow' | 'deny' | 'revoke'

interface PermissionSection {
  resource: string
  title: string
  items: AdminPermissionDefinition[]
  allowed: number
  denied: number
  targets: Record<BatchAction, AdminPermissionDefinition[]>
}

// 板块顺序与中文名；目录里未登记的 resource 追加在末尾（“其他 · xxx”），保证后端新增权限不会漏显示。
const SECTION_TITLES = new Map<string, string>([
  ['users', '用户'], ['groups', '分组'], ['accounts', '账号'], ['channels', '渠道'], ['models', '模型'],
  ['subscriptions', '订阅'], ['payment', '支付'], ['bank', '银行'], ['mall', '商城'], ['affiliates', '分销'],
  ['invites', '邀请'], ['checkin', '签到'], ['announcements', '公告'], ['pages', '页面'], ['plugins', '插件'],
  ['ops', '运维'], ['audit', '审计'], ['compliance', '合规'], ['system', '系统'], ['security', '安全'], ['oidc', 'OIDC'],
])
const BATCH_ACTIONS: BatchAction[] = ['allow', 'deny', 'revoke']
const BATCH_LABELS: Record<BatchAction, string> = { allow: '全部允许', deny: '全部拒绝', revoke: '全部撤销' }

const props = defineProps<{ show: boolean; userId?: number }>()
const emit = defineEmits<{ close: []; changed: [] }>()

const authStore = useAuthStore()
const stepUp = useStepUp()
const loading = ref(false)
const busy = ref(false)
const catalog = ref<AdminPermissionDefinition[]>([])
const state = ref<AdminUserPermissionState | null>(null)
const scopeDrafts = reactive<Record<string, string>>({})
const reason = ref('')
const expanded = ref<Record<string, boolean>>({})
const progress = reactive({ done: 0, total: 0 })
const failures = ref<Array<{ permission: string; message: string }>>([])
const notice = ref('')
const pendingBatch = ref<{ title: string; action: BatchAction; items: AdminPermissionDefinition[] } | null>(null)

const canWrite = computed(() => state.value?.capabilities?.can_write === true)
const modeDisabled = computed(() => state.value?.capabilities?.mode === 'disabled')
const readOnlyReason = computed(() => {
  const capability = state.value?.capabilities
  if (!capability || typeof capability.can_write !== 'boolean') return '写入能力未知；请重新读取，不会将缺失能力视为已获得权限。'
  return capability.deny_reason === 'permission_denied' ? '当前管理员没有 security.permissions.grant 权限。' : '当前操作不可写。'
})
const isSelf = computed(() => !!props.userId && props.userId === authStore.user?.id)
const isSelfSuperAdmin = computed(() => isSelf.value && authStore.user?.role === 'super_admin')
// 自己的账号：后端只放行超管为自己授予/撤销 oidc.*，其他自授一律拒绝，普通管理员不能改自己。
const canEditAny = computed(() => canWrite.value && (!isSelf.value || isSelfSuperAdmin.value))
const canEditSection = (resource: string) => canEditAny.value && (!isSelf.value || resource === 'oidc')
const canOperate = (resource: string) => canEditSection(resource) && !!reason.value && !busy.value
const canBatch = (section: PermissionSection, action: BatchAction) => canOperate(section.resource) && section.targets[action].length > 0

// 后端 grants 为空时会省略该字段
const grantMap = computed(() => new Map((state.value?.grants ?? []).map(grant => [grant.permission, grant])))
const grantOf = (permission: string) => grantMap.value.get(permission)

const isGlobalScope = (scope?: Record<string, unknown>) => !!scope && Object.keys(scope).length === 1 && scope['*'] === '*'
const isEmptyScope = (scope?: Record<string, unknown>) => !scope || Object.keys(scope).length === 0

// 批量操作只提交会产生变化的项
function needsChange(action: BatchAction, grant?: AdminPermissionGrant) {
  if (action === 'allow') return !(grant?.effect === 'allow' && isGlobalScope(grant.scope))
  if (action === 'deny') return !(grant?.effect === 'deny' && isEmptyScope(grant.scope))
  return !!grant
}

const sections = computed<PermissionSection[]>(() => {
  const byResource = new Map<string, AdminPermissionDefinition[]>()
  for (const item of catalog.value) {
    const items = byResource.get(item.resource)
    if (items) items.push(item)
    else byResource.set(item.resource, [item])
  }
  const known = [...SECTION_TITLES.keys()].filter(resource => byResource.has(resource))
  const unknown = [...byResource.keys()].filter(resource => !SECTION_TITLES.has(resource)).sort()
  return [...known, ...unknown].map(resource => {
    const items = byResource.get(resource) ?? []
    const targetsOf = (action: BatchAction) => items.filter(item => needsChange(action, grantOf(item.permission)))
    return {
      resource,
      title: SECTION_TITLES.get(resource) ?? `其他 · ${resource}`,
      items,
      allowed: items.filter(item => grantOf(item.permission)?.effect === 'allow').length,
      denied: items.filter(item => grantOf(item.permission)?.effect === 'deny').length,
      targets: { allow: targetsOf('allow'), deny: targetsOf('deny'), revoke: targetsOf('revoke') },
    }
  })
})

const pendingSensitive = computed(() => (pendingBatch.value?.items ?? []).filter(item => item.sensitive).map(item => item.permission))
const confirmTitle = computed(() => (pendingBatch.value ? `确认${BATCH_LABELS[pendingBatch.value.action]}` : ''))
const confirmMessage = computed(() => {
  const pending = pendingBatch.value
  return pending ? `将对「${pending.title}」板块的 ${pending.items.length} 项权限执行「${BATCH_LABELS[pending.action]}」，确认继续？` : ''
})

function defaultScope(permission: string) {
  return JSON.stringify(grantOf(permission)?.scope || { '*': '*' })
}

function errorMessage(error: unknown) {
  return (error as { message?: string } | null)?.message || '未知错误'
}

function stepUpNotice(error: unknown, remaining: number) {
  if (isStepUpCancelled(error)) return `已取消二次验证，剩余 ${remaining} 项未执行。`
  return stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
    ? '管理 API Key 无法执行此操作，请使用已通过二次验证的管理员会话。'
    : '此操作需要开启二次验证（TOTP），请先在个人资料中启用。'
}

function resetFeedback() {
  notice.value = ''
  failures.value = []
  progress.done = 0
  progress.total = 0
}

async function load() {
  if (!props.show || !props.userId) return
  loading.value = true
  try {
    const [items, nextState] = await Promise.all([
      listAdminPermissionCatalog(),
      getUserAdminPermissions(props.userId),
    ])
    catalog.value = items
    state.value = nextState
    for (const item of items) scopeDrafts[item.permission] = defaultScope(item.permission)
  } catch (error) {
    notice.value = `加载权限失败：${errorMessage(error)}`
  } finally {
    loading.value = false
  }
}

async function refreshSelf() {
  try {
    await authStore.refreshUser()
  } catch {
    // 菜单权限刷新失败不影响本次授权结果，定时刷新会补上
  }
}

// 每轮写操作结束：有成功项才通知父组件（改的是自己时同步刷新 OIDC 菜单），并只重新加载一次
async function finishMutation(userId: number, changed: boolean) {
  try {
    if (changed) {
      emit('changed')
      if (userId === authStore.user?.id) void refreshSelf()
    }
    await load()
  } finally {
    busy.value = false
  }
}

async function runSingle(permission: string, request: (userId: number) => Promise<void>) {
  const userId = props.userId
  if (!userId || busy.value) return
  resetFeedback()
  busy.value = true
  let changed = false
  try {
    await stepUp.run(() => request(userId))
    changed = true
  } catch (error) {
    notice.value = isStepUpCancelled(error) || isStepUpBlocked(error)
      ? stepUpNotice(error, 1)
      : `${permission}：${errorMessage(error)}`
  } finally {
    await finishMutation(userId, changed)
  }
}

async function toggle(item: AdminPermissionDefinition, effect: 'allow' | 'deny') {
  if (!canOperate(item.resource)) return
  let scope: Record<string, unknown> = {}
  if (effect === 'allow') {
    try {
      scope = JSON.parse(scopeDrafts[item.permission] || '')
    } catch {
      window.alert('scope 必须是有效 JSON；全局授权请使用 {"*":"*"}。')
      return
    }
  }
  const currentReason = reason.value
  await runSingle(item.permission, userId => grantUserAdminPermission(userId, { permission: item.permission, effect, scope, reason: currentReason }))
}

async function revoke(item: AdminPermissionDefinition) {
  if (!canOperate(item.resource) || !grantOf(item.permission)) return
  const currentReason = reason.value
  await runSingle(item.permission, userId => revokeUserAdminPermission(userId, item.permission, currentReason))
}

async function runBatch(action: BatchAction, items: AdminPermissionDefinition[]) {
  const userId = props.userId
  const batchReason = reason.value
  if (!userId || !batchReason || busy.value || items.length === 0) return
  resetFeedback()
  busy.value = true
  progress.total = items.length
  let changed = false
  try {
    // 逐条顺序提交，不并发：首条触发二次验证后，后续请求在有效期内直接通过
    for (let index = 0; index < items.length; index++) {
      if (!props.show || props.userId !== userId) break // 面板已关闭或切换了用户，不再继续发请求
      const permission = items[index].permission
      try {
        await stepUp.run(() => action === 'revoke'
          ? revokeUserAdminPermission(userId, permission, batchReason)
          : grantUserAdminPermission(userId, { permission, effect: action, scope: action === 'allow' ? { '*': '*' } : {}, reason: batchReason }))
        changed = true
      } catch (error) {
        if (isStepUpCancelled(error) || isStepUpBlocked(error)) {
          notice.value = stepUpNotice(error, items.length - index)
          break
        }
        failures.value.push({ permission, message: errorMessage(error) })
      }
      progress.done = index + 1
    }
  } finally {
    await finishMutation(userId, changed)
  }
}

function requestBatch(section: PermissionSection, action: BatchAction) {
  if (!canBatch(section, action)) return
  const items = section.targets[action]
  // 含敏感权限，或安全 / OIDC 板块（无论是否敏感）的批量操作需要二次确认
  if (section.resource === 'security' || section.resource === 'oidc' || items.some(item => item.sensitive)) {
    pendingBatch.value = { title: section.title, action, items }
    return
  }
  void runBatch(action, items)
}

function confirmBatch() {
  const pending = pendingBatch.value
  pendingBatch.value = null
  if (pending) void runBatch(pending.action, pending.items)
}

// 只在打开面板或切换用户时清空原因与反馈；操作后的刷新保留原因，方便连续批量操作
watch(() => [props.show, props.userId], () => {
  reason.value = ''
  expanded.value = {}
  pendingBatch.value = null
  resetFeedback()
  void load()
}, { immediate: true })
</script>
