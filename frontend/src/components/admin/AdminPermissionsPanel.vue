<template>
  <BaseDialog :show="show" title="管理员权限" width="wide" @close="$emit('close')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2 text-sm">
        <p class="text-gray-500 dark:text-dark-400">显式 deny 优先；空 scope 不再视为无限权限，全局授权必须使用 {&quot;*&quot;:&quot;*&quot;}。</p>
        <span :class="canWrite ? 'text-emerald-600' : 'text-amber-600'" class="font-medium">
          {{ canWrite ? '可写' : '只读' }}
        </span>
      </div>
      <div v-if="!canWrite" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        {{ readOnlyReason }}
      </div>
      <label v-if="canWrite" class="block text-sm">
        <span class="mb-1 block text-gray-600 dark:text-dark-300">操作原因（审计必填）</span>
        <input v-model.trim="reason" data-testid="permission-reason" class="input w-full" placeholder="说明本次授权/撤销原因" />
      </label>
      <div v-if="loading" class="py-8 text-center text-sm text-gray-500">加载中...</div>
      <div v-else class="max-h-[60vh] overflow-y-auto">
        <table class="min-w-full text-sm">
          <thead>
            <tr class="border-b text-left text-gray-500">
              <th class="py-2">权限</th>
              <th class="py-2">效果</th>
              <th class="py-2">范围</th>
              <th class="py-2">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in catalog" :key="item.permission" class="border-b border-gray-100 dark:border-dark-700">
              <td class="py-2 pr-3">
                <div class="font-medium text-gray-900 dark:text-white">{{ item.permission }}</div>
                <div class="text-xs text-gray-500">{{ item.description }}{{ item.sensitive ? ' · 敏感' : '' }}</div>
              </td>
              <td class="py-2"><span class="badge">{{ grantOf(item.permission)?.effect || '未授予' }}</span></td>
              <td class="py-2 pr-3">
                <input
                  v-if="canWrite"
                  v-model.trim="scopeDrafts[item.permission]"
                  class="input w-full min-w-48 font-mono text-xs"
                  :aria-label="`${item.permission} scope`"
                />
                <code v-else class="text-xs">{{ JSON.stringify(grantOf(item.permission)?.scope || {}) }}</code>
              </td>
              <td class="space-x-2 whitespace-nowrap py-2">
                <button :disabled="!canWrite || !reason" class="btn btn-secondary" @click="toggle(item.permission, 'allow')">Allow</button>
                <button :disabled="!canWrite || !reason" class="btn btn-secondary" @click="toggle(item.permission, 'deny')">Deny</button>
                <button :disabled="!canWrite || !reason || !grantOf(item.permission)" class="btn btn-secondary text-red-600" @click="revoke(item.permission)">撤销</button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  getUserAdminPermissions,
  listAdminPermissionCatalog,
  grantUserAdminPermission,
  revokeUserAdminPermission,
  type AdminPermissionDefinition,
  type AdminUserPermissionState,
} from '@/api/adminPermissions'

const props = defineProps<{ show: boolean; userId?: number }>()
const emit = defineEmits<{ close: []; changed: [] }>()

const loading = ref(false)
const catalog = ref<AdminPermissionDefinition[]>([])
const state = ref<AdminUserPermissionState | null>(null)
const scopeDrafts = reactive<Record<string, string>>({})
const reason = ref('')
const canWrite = computed(() => state.value?.capabilities?.can_write === true)
const readOnlyReason = computed(() => {
  const capability = state.value?.capabilities
  if (!capability || capability.mode !== 'enforce') return '权限 enforce 未开启，当前仅允许查看，所有授权与撤销保持只读。'
  return capability.deny_reason === 'permission_denied' ? '当前管理员没有 security.permissions.grant 权限。' : '当前操作不可写。'
})
const grantOf = (permission: string) => state.value?.grants.find(item => item.permission === permission)

function defaultScope(permission: string) {
  return JSON.stringify(grantOf(permission)?.scope || { '*': '*' })
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
    reason.value = ''
    for (const item of items) scopeDrafts[item.permission] = defaultScope(item.permission)
  } finally {
    loading.value = false
  }
}

async function toggle(permission: string, effect: 'allow' | 'deny') {
  if (!canWrite.value || !props.userId || !reason.value) return
  let scope: Record<string, unknown> = {}
  if (effect === 'allow') {
    try {
      scope = JSON.parse(scopeDrafts[permission] || '')
    } catch {
      window.alert('scope 必须是有效 JSON；全局授权请使用 {"*":"*"}。')
      return
    }
  }
  await grantUserAdminPermission(props.userId, { permission, effect, scope, reason: reason.value })
  emit('changed')
  await load()
}

async function revoke(permission: string) {
  if (!canWrite.value || !props.userId || !reason.value) return
  await revokeUserAdminPermission(props.userId, permission, reason.value)
  emit('changed')
  await load()
}

watch(() => [props.show, props.userId], load, { immediate: true })
</script>
