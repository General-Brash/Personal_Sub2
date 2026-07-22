<template>
  <div
    v-if="items.length"
    class="flex max-w-full shrink-0 flex-col items-end gap-1"
    :aria-label="t('payment.purchaseLimit.ariaLabel')"
    data-test="purchase-limit-badges"
  >
    <span
      v-for="item in items"
      :key="item.scope"
      class="inline-flex whitespace-nowrap rounded px-1.5 py-0.5 text-[10px] font-semibold ring-1 ring-inset"
      :class="item.exhausted
        ? 'bg-red-50 text-red-700 ring-red-200 dark:bg-red-900/20 dark:text-red-300 dark:ring-red-800'
        : 'bg-amber-50 text-amber-700 ring-amber-200 dark:bg-amber-900/20 dark:text-amber-300 dark:ring-amber-800'"
      :title="limitTitle(item)"
      :data-test="`purchase-limit-${item.scope}`"
    >
      <template v-if="items.length > 1">{{ t(`payment.purchaseLimit.${item.scope}`) }} </template>{{ item.used }}/{{ item.limit }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PurchaseLimitFields } from '@/types/payment'
import { getPurchaseLimitItems, type PurchaseLimitItem } from '@/utils/purchaseLimits'

const props = defineProps<{ limits: PurchaseLimitFields }>()
const { t } = useI18n()
const items = computed(() => getPurchaseLimitItems(props.limits))

function limitTitle(item: PurchaseLimitItem): string {
  return t('payment.purchaseLimit.detail', {
    scope: t(`payment.purchaseLimit.${item.scope}`),
    used: item.used,
    limit: item.limit,
    remaining: item.remaining,
  })
}
</script>
