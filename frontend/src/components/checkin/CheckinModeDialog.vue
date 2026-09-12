<template>
  <BaseDialog :show="show" title="选择签到方式" width="wide" close-on-click-outside @close="$emit('close')">
    <div class="space-y-4">
      <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 text-sm text-gray-600 dark:border-dark-600 dark:bg-dark-700 dark:text-gray-300">
        本期基础奖励 <span class="font-semibold text-emerald-600">{{ formatCredit(baseReward) }}</span>，
        永久基础奖励 <span class="font-semibold text-indigo-600">{{ formatCredit(permanentReward) }}</span>。
        选择后本周期不可更换。
      </div>
      <button
        data-test="checkin-mode-direct"
        type="button"
        class="flex w-full items-start justify-between gap-4 rounded-xl border border-gray-200 p-4 text-left transition hover:border-primary-400 hover:bg-primary-50/50 dark:border-dark-600 dark:hover:bg-dark-700"
        @click="$emit('select', 'direct')"
      >
        <span>
          <span class="block font-semibold text-gray-900 dark:text-white">直接领取</span>
          <span class="mt-1 block text-sm text-gray-500 dark:text-gray-400">不参与随机，永久基础奖励照常发放。</span>
        </span>
        <span class="shrink-0 font-mono font-semibold text-emerald-600">{{ formatCredit(baseReward) }}</span>
      </button>
      <button
        data-test="checkin-mode-normal"
        type="button"
        :disabled="!normal.enabled"
        class="flex w-full items-start justify-between gap-4 rounded-xl border border-gray-200 p-4 text-left transition hover:border-primary-400 hover:bg-primary-50/50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-dark-600 dark:hover:bg-dark-700"
        @click="$emit('select', 'normal')"
      >
        <span>
          <span class="block font-semibold text-gray-900 dark:text-white">普通博弈</span>
          <span class="mt-1 block text-sm text-gray-500 dark:text-gray-400">
            服务端安全随机 {{ formatMultiplier(normal.min_bps) }}–{{ formatMultiplier(normal.max_bps) }}
          </span>
        </span>
        <span class="shrink-0 text-xs font-medium text-primary-600">{{ normal.enabled ? '可参与' : '未启用' }}</span>
      </button>
      <button
        data-test="checkin-mode-super"
        type="button"
        :disabled="!superMode.enabled || !canAffordSuper"
        class="flex w-full items-start justify-between gap-4 rounded-xl border border-amber-200 p-4 text-left transition hover:border-amber-400 hover:bg-amber-50/50 disabled:cursor-not-allowed disabled:opacity-50 dark:border-amber-900/60 dark:hover:bg-amber-950/20"
        @click="$emit('select', 'super')"
      >
        <span>
          <span class="block font-semibold text-gray-900 dark:text-white">超级博弈</span>
          <span class="mt-1 block text-sm text-gray-500 dark:text-gray-400">
            随机 {{ formatMultiplier(superMode.min_bps) }}–{{ formatMultiplier(superMode.max_bps) }}，签到前永久余额扣
            {{ formatCredit(superMode.cost) }}
          </span>
        </span>
        <span class="shrink-0 text-xs font-medium text-amber-600">{{ !superMode.enabled ? '未启用' : canAffordSuper ? '可参与' : '余额不足' }}</span>
      </button>
      <p class="text-xs leading-5 text-gray-500 dark:text-gray-400">
        自动签到开启时不可选择普通或超级；若已开启，请先关闭自动签到。
      </p>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import BaseDialog from '@/components/common/BaseDialog.vue'
import type { CheckinMode } from '@/api/checkin'
import { formatDecimalAmount } from '@/utils/format'

interface RandomPolicy {
  enabled: boolean
  min_bps: number
  max_bps: number
}
interface SuperPolicy extends RandomPolicy {
  cost: string
}

defineProps<{
  show: boolean
  baseReward: string
  permanentReward: string
  permanentBalance: string
  normal: RandomPolicy
  superMode: SuperPolicy
  canAffordSuper: boolean
}>()

defineEmits<{
  (event: 'select', mode: Exclude<CheckinMode, 'direct-auto'>): void
  (event: 'close'): void
}>()

function formatCredit(value: string): string {
  return `$${formatDecimalAmount(value)}`
}
function formatMultiplier(bps: number): string {
  return `${(bps / 10000).toFixed(4).replace(/0+$/, '').replace(/\.$/, '')}×`
}
</script>
