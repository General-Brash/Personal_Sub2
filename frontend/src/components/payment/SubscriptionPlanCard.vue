<template>
  <div
    :class="[
      'group relative flex flex-col overflow-hidden rounded-2xl border transition-all',
      purchaseDisabled ? 'opacity-65' : 'hover:-translate-y-0.5 hover:shadow-xl',
      borderClass,
      'bg-white dark:bg-dark-800',
    ]"
  >
    <!-- Colored top accent bar -->
    <div :class="['h-1.5', accentClass]" />

    <div class="flex flex-1 flex-col p-4">
      <!-- Header: product identity + purchase limit -->
      <div class="mb-3 flex items-start justify-between gap-2">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <h3 class="truncate text-base font-bold text-gray-900 dark:text-white">{{ plan.name }}</h3>
            <span :class="['shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium', badgeLightClass]">
              {{ pLabel }}
            </span>
          </div>
          <p v-if="plan.description" class="mt-0.5 text-xs leading-relaxed text-gray-500 dark:text-dark-400 line-clamp-2">
            {{ plan.description }}
          </p>
        </div>
        <PurchaseLimitBadge :limits="plan" />
      </div>

      <div v-if="isDailyTemporaryCredit" class="mb-3 grid grid-cols-3 gap-2 rounded-lg bg-cyan-50 px-3 py-2 text-xs dark:bg-cyan-950/30">
        <div>
          <span class="block text-cyan-700/70 dark:text-cyan-300/70">{{ t('commerce.subscription.dailyAmount') }}</span>
          <strong class="mt-0.5 block font-mono text-sm text-cyan-800 dark:text-cyan-200">${{ formatMoneyDisplay(dailyCreditAmount) }}</strong>
        </div>
        <div>
          <span class="block text-cyan-700/70 dark:text-cyan-300/70">{{ t('commerce.subscription.duration') }}</span>
          <strong class="mt-0.5 block text-sm text-cyan-800 dark:text-cyan-200">{{ t('commerce.subscription.daysCount', { days: plan.validity_days }) }}</strong>
        </div>
        <div>
          <span class="block text-cyan-700/70 dark:text-cyan-300/70">{{ t('commerce.subscription.totalAmount') }}</span>
          <strong class="mt-0.5 block font-mono text-sm text-cyan-800 dark:text-cyan-200">${{ formatMoneyDisplay(dailyCreditTotal) }}</strong>
        </div>
        <p class="col-span-3 border-t border-cyan-200/70 pt-2 text-[11px] text-cyan-700 dark:border-cyan-800 dark:text-cyan-300">
          {{ t('commerce.subscription.renewalExtends') }}
        </p>
      </div>

      <!-- Legacy Sub2 group quota info -->
      <div v-else class="mb-3 grid grid-cols-2 gap-x-3 gap-y-1 rounded-lg bg-gray-50 px-3 py-2 text-xs dark:bg-dark-700/50">
        <div class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.rate') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ rateDisplay }}</span>
        </div>
        <div v-if="hasPeakRate" class="col-span-2 flex items-center justify-between gap-2">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.peakRate') }}</span>
          <span class="text-right font-medium text-amber-700 dark:text-amber-300">{{ peakRateDisplay }}</span>
        </div>
        <div v-if="plan.daily_limit_usd != null" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.dailyLimit') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">${{ formatMoneyDisplay(plan.daily_limit_usd) }}</span>
        </div>
        <div v-if="plan.weekly_limit_usd != null" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.weeklyLimit') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">${{ formatMoneyDisplay(plan.weekly_limit_usd) }}</span>
        </div>
        <div v-if="plan.monthly_limit_usd != null" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.monthlyLimit') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">${{ formatMoneyDisplay(plan.monthly_limit_usd) }}</span>
        </div>
        <div v-if="plan.daily_limit_usd == null && plan.weekly_limit_usd == null && plan.monthly_limit_usd == null" class="flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.quota') }}</span>
          <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('payment.planCard.unlimited') }}</span>
        </div>
        <div v-if="modelScopeLabels.length > 0" class="col-span-2 flex items-center justify-between">
          <span class="text-gray-400 dark:text-dark-500">{{ t('payment.planCard.models') }}</span>
          <div class="flex flex-wrap justify-end gap-1">
            <span v-for="scope in modelScopeLabels" :key="scope"
              class="rounded bg-gray-200/80 px-1.5 py-0.5 text-[10px] font-medium text-gray-600 dark:bg-dark-600 dark:text-gray-300">
              {{ scope }}
            </span>
          </div>
        </div>
      </div>

      <!-- Features list (compact) -->
      <div v-if="plan.features.length > 0" class="mb-3 space-y-1">
        <div v-for="feature in plan.features" :key="feature" class="flex items-start gap-1.5">
          <svg :class="['mt-0.5 h-3.5 w-3.5 flex-shrink-0', iconClass]" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M4.5 12.75l6 6 9-13.5" />
          </svg>
          <span class="text-xs text-gray-600 dark:text-gray-300">{{ feature }}</span>
        </div>
      </div>

      <div class="flex-1" />

      <div class="mb-3 flex items-end justify-between gap-3 border-t border-gray-100 pt-3 dark:border-dark-700">
        <div class="min-w-0">
          <p class="text-[11px] text-gray-400 dark:text-dark-500">{{ t('payment.purchaseCard.expectedReceive') }}</p>
          <p :class="['mt-0.5 break-words text-lg font-bold', textClass]">{{ expectedReceive }}</p>
        </div>
        <div class="shrink-0 text-right">
          <p class="text-[11px] text-gray-400 dark:text-dark-500">{{ t('payment.purchaseCard.expectedSpend') }}</p>
          <div class="mt-0.5 flex items-baseline justify-end gap-1">
            <span class="text-xs text-gray-400 dark:text-dark-500">$</span>
            <span class="text-sm font-semibold text-gray-700 dark:text-gray-200">{{ formatMoneyDisplay(plan.price) }}</span>
            <span class="text-[10px] font-medium text-gray-400 dark:text-dark-500">{{ paymentCreditLabel }}</span>
          </div>
          <div v-if="plan.original_price" class="mt-0.5 flex items-center justify-end gap-1.5">
            <span class="text-[10px] text-gray-400 line-through dark:text-dark-500">${{ formatMoneyDisplay(plan.original_price) }} {{ paymentCreditLabel }}</span>
            <span :class="['rounded px-1 py-0.5 text-[10px] font-semibold', discountClass]">{{ discountText }}</span>
          </div>
        </div>
      </div>

      <!-- Subscribe Button -->
      <button
        type="button"
        :class="['w-full rounded-xl py-2.5 text-sm font-semibold transition-all active:scale-[0.98]', btnClass]"
        :disabled="purchaseDisabled"
        :aria-disabled="purchaseDisabled"
        data-test="subscription-plan-select"
        @click="emit('select', plan)"
      >
        {{ purchaseDisabledText || (isRenewal ? t('payment.renewNow') : t('payment.subscribeNow')) }}
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SubscriptionPlan } from '@/types/payment'
import type { UserSubscription } from '@/types'
import { useAppStore } from '@/stores/app'
import { formatMoneyDisplay, multiplyDecimalAmount } from '@/utils/format'
import { isPurchaseLimitExhausted } from '@/utils/purchaseLimits'
import { subscriptionValidityDays } from '@/components/payment/paymentFlow'
import PurchaseLimitBadge from '@/components/payment/PurchaseLimitBadge.vue'
import { hasPeakRate as groupHasPeakRate, formatPeakRateWindow, serverTimezoneLabel } from '@/utils/peak-rate'
import {
  platformAccentBarClass,
  platformBadgeLightClass,
  platformBorderClass,
  platformTextClass,
  platformIconClass,
  platformButtonClass,
  platformDiscountClass,
  platformLabel,
} from '@/utils/platformColors'

const props = defineProps<{ plan: SubscriptionPlan; activeSubscriptions?: UserSubscription[]; disabledReason?: string }>()
const emit = defineEmits<{ select: [plan: SubscriptionPlan] }>()
const { t } = useI18n()

const isDailyTemporaryCredit = computed(() => props.plan.benefit_type === 'daily_temporary_credit')
const platform = computed(() => isDailyTemporaryCredit.value ? '' : (props.plan.group_platform || ''))
const isRenewal = computed(() =>
  props.activeSubscriptions?.some(s => s.group_id === props.plan.group_id && s.status === 'active') ?? false
)

// Derived color classes from central config
const accentClass = computed(() => platformAccentBarClass(platform.value))
const borderClass = computed(() => platformBorderClass(platform.value))
const badgeLightClass = computed(() => platformBadgeLightClass(platform.value))
const textClass = computed(() => platformTextClass(platform.value))
const iconClass = computed(() => platformIconClass(platform.value))
const isExhausted = computed(() => isPurchaseLimitExhausted(props.plan))
const purchaseDisabled = computed(() => isExhausted.value || Boolean(props.disabledReason))
const purchaseDisabledText = computed(() => props.disabledReason || (isExhausted.value ? t('payment.purchaseLimit.exhausted') : ''))
const btnClass = computed(() => purchaseDisabled.value
  ? 'cursor-not-allowed bg-gray-200 text-gray-500 dark:bg-dark-600 dark:text-gray-400'
  : platformButtonClass(platform.value))
const discountClass = computed(() => platformDiscountClass(platform.value))
const pLabel = computed(() => isDailyTemporaryCredit.value
  ? t('commerce.subscription.dailyTemporaryBadge')
  : platformLabel(platform.value))
const paymentCreditLabel = computed(() => t(`commerce.creditType.${props.plan.payment_credit_type === 'temporary' ? 'temporary' : 'permanent'}`))
const dailyCreditAmount = computed(() => props.plan.daily_temporary_credit_amount ?? 0)
const dailyCreditTotal = computed(() => multiplyDecimalAmount(dailyCreditAmount.value, props.plan.validity_days))

const discountText = computed(() => {
  if (!props.plan.original_price || props.plan.original_price <= 0) return ''
  const pct = Math.round((1 - props.plan.price / props.plan.original_price) * 100)
  return pct > 0 ? `-${pct}%` : ''
})

const rateDisplay = computed(() => {
  const rate = props.plan.rate_multiplier ?? 1
  return `×${Number(rate.toPrecision(10))}`
})

const appStore = useAppStore()

const hasPeakRate = computed(() => groupHasPeakRate(props.plan))

const peakRateDisplay = computed(() => {
  return formatPeakRateWindow(props.plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
})

const MODEL_SCOPE_LABELS: Record<string, string> = {
  claude: 'Claude',
  gemini_text: 'Gemini',
  gemini_image: 'Imagen',
}

const modelScopeLabels = computed(() => {
  if (platform.value !== 'antigravity') return []
  const scopes = props.plan.supported_model_scopes
  if (!scopes || scopes.length === 0) return []
  return scopes.map(s => MODEL_SCOPE_LABELS[s] || s)
})

const validitySuffix = computed(() => {
  return `${subscriptionValidityDays(props.plan)}${t('payment.days')}`
})

const expectedReceive = computed(() => isDailyTemporaryCredit.value
  ? `$${formatMoneyDisplay(dailyCreditTotal.value)} ${t('commerce.creditType.temporary')}`
  : validitySuffix.value)
</script>
