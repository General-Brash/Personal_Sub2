<template>
  <BaseDialog :show="show" title="消费权益" width="wide" @close="$emit('close')">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2 text-sm text-gray-500 dark:text-dark-400">
        <span>目标 {{ targets.length }} 人；下方显示首位用户当前权益。手工分组和有效订阅不会被批量升降级删除。</span>
        <span :class="canWrite ? 'text-emerald-600' : 'text-amber-600'" class="font-medium">{{ canWrite ? '可写' : '只读' }}</span>
      </div>
      <div v-if="!canWrite" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        {{ readOnlyReason }}
      </div>
      <div v-if="loading" class="py-8 text-center text-sm text-gray-500">加载中...</div>
      <template v-else-if="entitlement">
        <div class="grid gap-3 text-sm sm:grid-cols-3">
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">当前层级</span><div class="font-semibold">{{ entitlement.tier }}</div></div>
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">版本</span><div class="font-semibold">{{ entitlement.version }}</div></div>
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">Premium 政策</span><div class="font-semibold">{{ premiumEnabled ? '已启用' : '未启用' }}</div></div>
        </div>

        <div v-if="canWrite" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex flex-wrap gap-2">
            <button
              v-for="tier in tiers"
              :key="tier"
              :disabled="tier === 'premium' && !premiumEnabled"
              class="btn"
              :class="pendingTier === tier ? 'btn-primary' : 'btn-secondary'"
              @click="preview(tier)"
            >
              切换为 {{ tier }}
            </button>
          </div>
          <label class="mt-3 block text-sm">
            <span class="mb-1 block text-gray-600 dark:text-dark-300">原始原因（审计必填）</span>
            <input v-model.trim="reason" data-testid="entitlement-reason" class="input w-full" placeholder="例如：活动授予 / 用户申诉调整" />
          </label>
          <div v-if="previewResult" class="mt-3 rounded-lg bg-blue-50 p-3 text-sm dark:bg-blue-900/20">
            <div>影响 {{ previewResult.affected_user_ids.length }} 人，已在目标层级 {{ previewResult.already_at_tier.length }} 人。</div>
            <div class="mt-1 text-xs">
              新增组 {{ previewResult.granted_group_ids?.join(', ') || '无' }}；撤销组 {{ previewResult.revoked_group_ids?.join(', ') || '无' }}。
              手工分组和有效订阅均保留。
            </div>
          </div>
          <button class="btn btn-primary mt-3" :disabled="applying || !pendingTier || !reason || !previewResult" @click="apply">
            {{ applying ? '应用中...' : '确认应用' }}
          </button>
        </div>

        <div class="text-sm">
          <div class="font-medium text-gray-900 dark:text-white">来源解释</div>
          <ul class="mt-2 space-y-1 text-gray-600 dark:text-dark-300">
            <li v-for="source in entitlement.sources" :key="`${source.source}-${source.tier}-${source.group_id ?? 'tier'}`">
              {{ source.explain }} · {{ source.tier }}<span v-if="source.group_id"> · group {{ source.group_id }}</span><span v-if="source.rate"> · {{ source.rate }}x</span>
            </li>
            <li v-if="entitlement.sources.length === 0">默认 standard</li>
          </ul>
        </div>
        <div class="grid gap-3 text-sm sm:grid-cols-2">
          <div><span class="text-gray-500">手工分组：</span>{{ entitlement.manual_groups.join(', ') || '无' }}</div>
          <div><span class="text-gray-500">有效订阅组：</span>{{ entitlement.subscription_groups.join(', ') || '无' }}</div>
        </div>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  getEntitlementCatalog,
  getUserEntitlement,
  previewEntitlementChange,
  applyEntitlementChange,
  type EntitlementChangePreview,
  type EntitlementTier,
  type UserEntitlement,
} from '@/api/adminEntitlements'

const props = defineProps<{ show: boolean; userId?: number; userIds?: number[] }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const targets = computed(() => props.userIds?.length ? props.userIds : props.userId ? [props.userId] : [])

const loading = ref(false)
const applying = ref(false)
const entitlement = ref<UserEntitlement | null>(null)
const premiumEnabled = ref(false)
const tiers: EntitlementTier[] = ['standard', 'premium']
const pendingTier = ref<EntitlementTier | null>(null)
const previewResult = ref<EntitlementChangePreview | null>(null)
const reason = ref('')
const requestId = ref('')
const canWrite = computed(() => entitlement.value?.capabilities?.can_write === true)
const readOnlyReason = computed(() => {
  const capability = entitlement.value?.capabilities
  if (!capability || capability.mode !== 'enforce') return '权限 enforce 未开启，当前仅允许查看权益，不提供升降级写入。'
  return capability.deny_reason === 'permission_denied' ? '当前管理员没有 users.entitlement.manage 权限。' : '当前操作不可写。'
})

async function load() {
  if (!props.show || !targets.value.length) return
  loading.value = true
  try {
    const [next, catalog] = await Promise.all([
      getUserEntitlement(targets.value[0]),
      getEntitlementCatalog(),
    ])
    entitlement.value = next
    premiumEnabled.value = catalog.tiers.find(item => item.tier === 'premium')?.enabled === true
    pendingTier.value = null
    previewResult.value = null
    reason.value = ''
    requestId.value = ''
  } finally {
    loading.value = false
  }
}

async function preview(tier: EntitlementTier) {
  if (!canWrite.value || !targets.value.length) return
  pendingTier.value = tier
  requestId.value = crypto.randomUUID()
  previewResult.value = await previewEntitlementChange(targets.value, tier)
}

async function apply() {
  if (!canWrite.value || !targets.value.length || !pendingTier.value || !reason.value || !requestId.value) return
  applying.value = true
  try {
    await applyEntitlementChange(targets.value, pendingTier.value, reason.value, requestId.value)
    emit('changed')
    await load()
  } finally {
    applying.value = false
  }
}

watch(() => [props.show, props.userId, props.userIds], load, { immediate: true })
</script>
