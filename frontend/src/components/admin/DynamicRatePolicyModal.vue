<template>
  <BaseDialog :show="show" title="动态倍率政策" width="wide" @close="handleClose">
    <div v-if="group" class="space-y-4">
      <div class="flex items-center gap-2 rounded-lg bg-gray-50 px-4 py-2.5 text-sm dark:bg-dark-700">
        <Icon name="sparkles" size="sm" class="text-primary-500" />
        <span class="font-medium text-gray-900 dark:text-white">{{ group.name }}</span>
        <span class="text-gray-400">#{{ group.id }}</span>
        <span class="ml-auto text-xs text-gray-500 dark:text-gray-400">按用户 × 父组日窗口统计，仅后续请求生效</span>
      </div>

      <div
        v-if="policy.current_policy_version && policy.current_policy_version > policy.policy_version"
        class="rounded-lg bg-amber-50 px-4 py-2 text-xs text-amber-800 dark:bg-amber-900/20 dark:text-amber-200"
      >
        当前展示的是已生效历史版本 v{{ policy.policy_version }}；版本 v{{ policy.current_policy_version }} 已排期，不会在本日清零既有累计。
      </div>

      <div v-if="loading" class="py-8 text-center text-sm text-gray-500">加载中...</div>
      <template v-else>
        <label class="flex items-center gap-3 text-sm text-gray-700 dark:text-gray-300">
          <input v-model="policy.enabled" type="checkbox" class="h-4 w-4 rounded" />
          <span class="font-medium">启用动态倍率</span>
          <span class="text-xs text-gray-500">默认关闭；启用前请确认该分组所有调用模式已适配；批量图片使用钱包消费口径</span>
        </label>

        <div class="grid gap-3 md:grid-cols-3">
          <label class="text-sm">
            <span class="input-label">统计口径</span>
            <select v-model="policy.metric" class="input w-full">
              <option value="tokens_m">Token（归一化非重叠）</option>
              <option value="wallet_spend">实际钱包扣款</option>
            </select>
          </label>
          <label class="text-sm">
            <span class="input-label">时区</span>
            <input v-model.trim="policy.timezone" class="input w-full" placeholder="Asia/Shanghai" />
          </label>
          <label class="text-sm">
            <span class="input-label">每日重置</span>
            <input v-model.trim="policy.reset_time" class="input w-full" placeholder="00:00" />
          </label>
        </div>

        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="mb-2 flex items-center justify-between">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">阶梯（左闭右开）</span>
            <button type="button" class="btn btn-secondary btn-sm" @click="addTier">新增阶梯</button>
          </div>
          <div v-for="(tier, index) in policy.tiers" :key="index" class="mb-2 grid grid-cols-[1fr_1fr_auto] gap-2">
            <input v-model.trim="tier.id" class="input" placeholder="tier-id" />
            <input v-model.number="tier.threshold" type="number" min="0" step="1" class="input" placeholder="阈值" />
            <div class="flex gap-2">
              <input v-model.number="tier.factor" type="number" min="0.001" step="0.001" class="input w-24" placeholder="倍率" />
              <button type="button" class="btn btn-secondary px-2" :disabled="policy.tiers.length <= 1" @click="removeTier(index)">删除</button>
            </div>
          </div>
          <p class="text-xs text-gray-500">threshold 的 token 单位为整数 token；1M=1,000,000。wallet_spend 使用实际结算金额。</p>
        </div>

        <div class="grid gap-3 md:grid-cols-[1fr_auto] md:items-end">
          <div>
            <span class="input-label">适用模式（本轮仅已适配项可勾选）</span>
            <label class="mr-4 inline-flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
              <input v-model="textModeEnabled" type="checkbox" disabled class="h-4 w-4" /> text
            </label>
            <label class="mr-4 inline-flex items-center gap-2 text-sm"><input v-model="imageModeEnabled" type="checkbox" :disabled="policy.metric !== 'wallet_spend'" /> image</label>
            <label class="mr-4 inline-flex items-center gap-2 text-sm"><input v-model="batchModeEnabled" type="checkbox" :disabled="policy.metric !== 'wallet_spend'" /> batch_image</label>
            <span class="text-xs text-gray-500">audio / video / live / simple / count_tokens 当前拒绝启用</span>
          </div>
          <div class="rounded bg-gray-50 px-3 py-2 text-xs dark:bg-dark-700">cache: normalized</div>
        </div>

        <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex flex-wrap items-end gap-2">
            <label class="text-sm">
              <span class="input-label">预览用户 ID</span>
              <input v-model.number="previewUserId" type="number" min="1" class="input w-40" />
            </label>
            <button type="button" class="btn btn-secondary" :disabled="previewUserId <= 0 || previewing" @click="handlePreview">
              {{ previewing ? '计算中...' : '预览当前窗口' }}
            </button>
          </div>
          <div v-if="preview" class="mt-3 text-sm text-gray-700 dark:text-gray-300">
            当前累计 {{ preview.status?.current ?? 0 }}，当前档 {{ preview.status?.tier_id || '-' }}，
            动态因子 {{ preview.dynamic_factor }}，最终因子 {{ preview.final_factor }}，重置 {{ preview.window_end || '-' }}
          </div>
        </div>
      </template>

      <div class="flex justify-end gap-2 border-t border-gray-200 pt-4 dark:border-dark-600">
        <button type="button" class="btn btn-secondary" @click="handleClose">关闭</button>
        <button type="button" class="btn btn-primary" :disabled="loading || saving" @click="handleSave">
          {{ saving ? '保存中...' : '保存' }}
        </button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import type { AdminGroup } from '@/types'
import {
  getDynamicRatePolicy,
  previewDynamicRate,
  saveDynamicRatePolicy,
  type DynamicRatePolicy,
  type DynamicRatePreview,
} from '@/api/dynamicRate'

const props = defineProps<{ show: boolean; group: AdminGroup | null }>()
const emit = defineEmits<{ close: []; success: [] }>()
const appStore = useAppStore()
const loading = ref(false)
const saving = ref(false)
const previewing = ref(false)
const previewUserId = ref(0)
const preview = ref<DynamicRatePreview | null>(null)
const textModeEnabled = ref(true)
const batchModeEnabled = ref(false)
const imageModeEnabled = ref(false)

const defaultPolicy = (groupId: number): DynamicRatePolicy => ({
  group_id: groupId,
  enabled: false,
  metric: 'tokens_m',
  timezone: 'Asia/Shanghai',
  reset_time: '00:00',
  tiers: [{ id: 'tier-0', threshold: 0, factor: 1 }],
  included_modes: ['text'],
  cache_token_policy: 'normalized',
  policy_version: 0,
  effective_at: ''
})
const policy = ref<DynamicRatePolicy>(defaultPolicy(0))

const addTier = () => {
  const last = policy.value.tiers[policy.value.tiers.length - 1]
  policy.value.tiers.push({ id: `tier-${policy.value.tiers.length}`, threshold: Number(last?.threshold || 0) + 1_000_000, factor: Number(last?.factor || 1) })
}
const removeTier = (index: number) => policy.value.tiers.splice(index, 1)

watch(() => [props.show, props.group?.id] as const, async ([show, groupId]) => {
  if (!show || !groupId) return
  loading.value = true
  preview.value = null
  try {
    policy.value = await getDynamicRatePolicy(groupId)
    if (!policy.value.tiers?.length) policy.value.tiers = defaultPolicy(groupId).tiers
    batchModeEnabled.value = policy.value.included_modes.includes('batch_image')
    imageModeEnabled.value = policy.value.included_modes.includes('image')
    policy.value.included_modes = ['text', ...(policy.value.metric === 'wallet_spend' && batchModeEnabled.value ? ['batch_image' as const] : []), ...(policy.value.metric === 'wallet_spend' && imageModeEnabled.value ? ['image' as const] : [])]
    policy.value.cache_token_policy = 'normalized'
    textModeEnabled.value = true
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '加载动态倍率失败')
  } finally {
    loading.value = false
  }
}, { immediate: true })

const handlePreview = async () => {
  if (!props.group) return
  previewing.value = true
  try {
    preview.value = await previewDynamicRate(props.group.id, {
      ...policy.value,
      user_id: previewUserId.value,
      static_factor: 1,
      peak_factor: 1
    })
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '预览失败')
  } finally {
    previewing.value = false
  }
}

const handleSave = async () => {
  if (!props.group) return
  saving.value = true
  try {
    policy.value.included_modes = ['text', ...(policy.value.metric === 'wallet_spend' && batchModeEnabled.value ? ['batch_image' as const] : []), ...(policy.value.metric === 'wallet_spend' && imageModeEnabled.value ? ['image' as const] : [])]
    policy.value.cache_token_policy = 'normalized'
    policy.value = await saveDynamicRatePolicy(props.group.id, policy.value)
    appStore.showSuccess('动态倍率政策已保存')
    emit('success')
    emit('close')
  } catch (error) {
    appStore.showError(error instanceof Error ? error.message : '保存失败')
  } finally {
    saving.value = false
  }
}
const handleClose = () => emit('close')
</script>
