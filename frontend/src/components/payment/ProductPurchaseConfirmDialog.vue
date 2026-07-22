<template>
  <BaseDialog
    :show="show"
    :title="t('payment.purchaseConfirm.title')"
    width="narrow"
    :close-on-escape="!submitting"
    @close="requestClose"
  >
    <div class="min-w-0 space-y-5" data-test="purchase-confirm-dialog">
      <div class="min-w-0">
        <div class="flex min-w-0 flex-wrap items-start justify-between gap-3">
          <div class="min-w-0">
            <p class="break-words text-base font-semibold text-gray-900 dark:text-white">{{ productName }}</p>
            <p v-if="description" class="mt-1 break-words text-sm text-gray-500 dark:text-gray-400">{{ description }}</p>
          </div>
          <PurchaseLimitBadge :limits="limits" />
        </div>
      </div>

      <dl class="divide-y divide-gray-100 rounded-lg border border-gray-200 px-4 dark:divide-dark-700 dark:border-dark-600">
        <div class="flex min-w-0 items-start justify-between gap-4 py-3">
          <dt class="shrink-0 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.purchaseConfirm.paymentMethod') }}</dt>
          <dd class="min-w-0 break-words text-right text-sm font-medium text-gray-900 dark:text-white">{{ paymentMethod }}</dd>
        </div>
        <div class="flex min-w-0 items-start justify-between gap-4 py-3">
          <dt class="shrink-0 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.purchaseConfirm.expectedSpend') }}</dt>
          <dd class="min-w-0 break-words text-right text-sm font-semibold text-gray-900 dark:text-white" data-test="purchase-confirm-spend">{{ expectedSpend }}</dd>
        </div>
        <div class="flex min-w-0 items-start justify-between gap-4 py-3">
          <dt class="shrink-0 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.purchaseConfirm.expectedReceive') }}</dt>
          <dd class="min-w-0 max-w-[60%] break-words text-right text-sm font-semibold text-emerald-700 dark:text-emerald-300" data-test="purchase-confirm-receive">{{ expectedReceive }}</dd>
        </div>
      </dl>

      <div class="rounded-lg bg-gray-50 px-4 py-3 dark:bg-dark-800">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('payment.purchaseConfirm.remainingTitle') }}</p>
        <div v-if="limitItems.length" class="mt-2 grid gap-2 sm:grid-cols-2">
          <div v-for="item in limitItems" :key="item.scope" class="flex items-center justify-between gap-3 text-sm">
            <span class="text-gray-500 dark:text-gray-400">{{ t(`payment.purchaseLimit.${item.scope}`) }}</span>
            <span class="font-mono font-semibold text-gray-900 dark:text-white" :data-test="`purchase-confirm-remaining-${item.scope}`">{{ item.remaining }}</span>
          </div>
        </div>
        <p v-else class="mt-1 text-sm font-medium text-gray-900 dark:text-white">{{ t('payment.purchaseLimit.unlimited') }}</p>
        <p v-if="limitItems.some(item => item.scope === 'daily')" class="mt-2 text-xs text-gray-500 dark:text-gray-400">
          {{ t('payment.purchaseLimit.dailyResetHint') }}
        </p>
      </div>
    </div>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <button type="button" class="btn btn-secondary" :disabled="submitting" @click="requestClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary inline-flex min-w-28 items-center justify-center"
          data-test="purchase-confirm-submit"
          :disabled="submitting || exhausted"
          @click="emit('confirm')"
        >
          {{ submitting ? t('common.processing') : t('payment.purchaseConfirm.confirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import PurchaseLimitBadge from '@/components/payment/PurchaseLimitBadge.vue'
import type { PurchaseLimitFields } from '@/types/payment'
import { getPurchaseLimitItems } from '@/utils/purchaseLimits'

const props = defineProps<{
  show: boolean
  productName: string
  description?: string
  paymentMethod: string
  expectedSpend: string
  expectedReceive: string
  limits: PurchaseLimitFields
  submitting?: boolean
}>()

const emit = defineEmits<{
  close: []
  confirm: []
}>()

const { t } = useI18n()
const limitItems = computed(() => getPurchaseLimitItems(props.limits))
const exhausted = computed(() => limitItems.value.some((item) => item.exhausted))

function requestClose(): void {
  if (!props.submitting) emit('close')
}
</script>
