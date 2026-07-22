<template>
  <BaseDialog :show="show" :title="plan ? t('payment.admin.editPlan') : t('payment.admin.createPlan')" width="wide" @close="emit('close')">
    <form id="plan-form" @submit.prevent="handleSavePlan" class="space-y-4">
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="input-label">{{ t('payment.admin.planName') }} <span class="text-red-500">*</span></label>
          <input v-model="planForm.name" data-test="plan-name" type="text" class="input" required />
        </div>
        <div>
          <label class="input-label">{{ t('payment.admin.benefitType') }} <span class="text-red-500">*</span></label>
          <Select v-model="planForm.benefit_type" :options="benefitTypeOptions" data-test="plan-benefit-type" />
        </div>
      </div>

      <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('payment.admin.paymentCreditType') }} <span class="text-red-500">*</span></label>
          <Select v-model="planForm.payment_credit_type" :options="creditTypeOptions" data-test="plan-payment-credit-type" />
        </div>
        <div v-if="isSub2Benefit">
          <label class="input-label">{{ t('payment.admin.group') }} <span class="text-red-500">*</span></label>
          <Select v-model="planForm.group_id" :options="groupOptions" :placeholder="t('payment.admin.selectGroup')" class="w-full" data-test="plan-group">
            <template #selected="{ option }">
              <span v-if="option?.platform" :class="platformTextClass(String(option.platform))">{{ option.label }}</span>
              <span v-else>{{ option?.label || t('payment.admin.selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <span class="flex-1 truncate text-left" :class="option.platform ? platformTextClass(String(option.platform)) : ''">{{ option.label }}</span>
              <Icon v-if="selected" name="check" size="sm" class="text-primary-500" :stroke-width="2" />
            </template>
          </Select>
        </div>
        <div v-else>
          <label class="input-label">{{ t('payment.admin.dailyTemporaryCreditAmount') }} <span class="text-red-500">*</span></label>
          <div class="relative mt-1">
            <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center font-mono text-gray-500">$</span>
            <input v-model.number="planForm.daily_temporary_credit_amount" data-test="plan-daily-temporary-credit" type="number" min="0.00000001" step="0.00000001" class="input pl-7 font-mono" required />
          </div>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.dailyTemporaryCreditHint') }}</p>
        </div>
      </div>

      <!-- Group Info Preview -->
      <div v-if="selectedGroupInfo" class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-600 dark:bg-dark-800">
        <div class="mb-2 flex items-center gap-2">
          <GroupBadge :name="selectedGroupInfo.name" :platform="selectedGroupInfo.platform" :rate-multiplier="selectedGroupInfo.rate_multiplier" />
        </div>
        <div class="grid grid-cols-2 gap-2 text-xs">
          <div><span class="text-gray-500">{{ t('payment.admin.dailyLimit') }}:</span> <span class="ml-1 font-medium text-gray-700 dark:text-gray-300">{{ selectedGroupInfo.daily_limit_usd != null ? '$' + formatMoneyDisplay(selectedGroupInfo.daily_limit_usd) : t('payment.admin.unlimited') }}</span></div>
          <div><span class="text-gray-500">{{ t('payment.admin.weeklyLimit') }}:</span> <span class="ml-1 font-medium text-gray-700 dark:text-gray-300">{{ selectedGroupInfo.weekly_limit_usd != null ? '$' + formatMoneyDisplay(selectedGroupInfo.weekly_limit_usd) : t('payment.admin.unlimited') }}</span></div>
          <div><span class="text-gray-500">{{ t('payment.admin.monthlyLimit') }}:</span> <span class="ml-1 font-medium text-gray-700 dark:text-gray-300">{{ selectedGroupInfo.monthly_limit_usd != null ? '$' + formatMoneyDisplay(selectedGroupInfo.monthly_limit_usd) : t('payment.admin.unlimited') }}</span></div>
        </div>
      </div>

      <div><label class="input-label">{{ t('payment.admin.planDescription') }} <span class="text-red-500">*</span></label><textarea v-model="planForm.description" data-test="plan-description" rows="2" class="input" required></textarea></div>
      <div class="grid grid-cols-2 gap-4">
        <div>
          <label class="input-label">{{ t('payment.admin.price') }} <span class="text-red-500">*</span></label>
          <div class="relative mt-1">
            <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center font-mono text-gray-500">$</span>
            <input v-model.number="planForm.price" data-test="plan-price" type="number" step="0.00000001" min="0.00000001" class="input pl-7 font-mono" required />
          </div>
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ creditTypeLabel(planForm.payment_credit_type) }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('payment.admin.originalPrice') }}</label>
          <div class="relative mt-1">
            <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center font-mono text-gray-500">$</span>
            <input v-model.number="planForm.original_price" type="number" step="0.00000001" min="0" class="input pl-7 font-mono" />
          </div>
        </div>
      </div>
      <div class="grid grid-cols-2 gap-4">
        <div><label class="input-label">{{ t('payment.admin.validityDays') }} <span class="text-red-500">*</span></label><input v-model.number="planForm.validity_days" data-test="plan-validity-days" type="number" min="1" class="input" required /></div>
        <div v-if="isSub2Benefit"><label class="input-label">{{ t('payment.admin.validityUnit') }} <span class="text-red-500">*</span></label><Select v-model="planForm.validity_unit" :options="validityUnitOptions" /></div>
        <div v-else class="flex items-end pb-2 text-sm text-gray-500 dark:text-gray-400">{{ t('payment.admin.dailyTemporaryCreditHint') }}</div>
      </div>
      <div><label class="input-label">{{ t('payment.admin.sortOrder') }}</label><input v-model.number="planForm.sort_order" type="number" min="0" class="input" /></div>
      <div>
        <label class="input-label">{{ t('payment.admin.features') }}</label>
        <textarea v-model="planFeaturesText" rows="3" class="input" :placeholder="t('payment.admin.featuresPlaceholder')"></textarea>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.featuresHint') }}</p>
      </div>
      <details class="rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-600" data-test="plan-advanced-settings">
        <summary class="cursor-pointer text-sm font-medium text-gray-800 dark:text-gray-200">
          {{ t('payment.admin.advancedSettings') }}
        </summary>
        <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <label for="plan-daily-purchase-limit" class="input-label">{{ t('payment.admin.dailyPurchaseLimit') }}</label>
            <input
              id="plan-daily-purchase-limit"
              v-model.number="planForm.daily_purchase_limit"
              data-test="plan-daily-purchase-limit"
              type="number"
              min="0"
              step="1"
              class="input"
              :placeholder="t('payment.admin.purchaseLimitPlaceholder')"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.dailyPurchaseLimitHint') }}</p>
          </div>
          <div>
            <label for="plan-total-purchase-limit" class="input-label">{{ t('payment.admin.totalPurchaseLimit') }}</label>
            <input
              id="plan-total-purchase-limit"
              v-model.number="planForm.total_purchase_limit"
              data-test="plan-total-purchase-limit"
              type="number"
              min="0"
              step="1"
              class="input"
              :placeholder="t('payment.admin.purchaseLimitPlaceholder')"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('payment.admin.totalPurchaseLimitHint') }}</p>
          </div>
        </div>
      </details>
      <div class="flex items-center gap-3">
        <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('payment.admin.forSale') }}</label>
        <button
          type="button"
          :class="[
            'relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
            planForm.for_sale ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'
          ]"
          @click="planForm.for_sale = !planForm.for_sale"
        >
          <span :class="[
            'pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out',
            planForm.for_sale ? 'translate-x-5' : 'translate-x-0'
          ]" />
        </button>
      </div>
    </form>
    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" @click="emit('close')" class="btn btn-secondary">{{ t('common.cancel') }}</button>
        <button type="submit" form="plan-form" :disabled="saving" class="btn btn-primary">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { AdminPaymentConfig } from '@/api/admin/payment'
import { extractApiErrorMessage } from '@/utils/apiError'
import type { CreditType, SubscriptionBenefitType, SubscriptionPlan } from '@/types/payment'
import type { AdminGroup } from '@/types'
import { formatMoneyDisplay } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import { platformTextClass } from '@/utils/platformColors'

const props = defineProps<{
  show: boolean
  plan: SubscriptionPlan | null
  groups: AdminGroup[]
  paymentConfig?: AdminPaymentConfig | null
}>()

const emit = defineEmits<{
  close: []
  saved: []
}>()

const { t } = useI18n()
const appStore = useAppStore()

const saving = ref(false)
const planForm = reactive({
  name: '',
  group_id: null as number | null,
  benefit_type: 'sub2' as SubscriptionBenefitType,
  payment_credit_type: 'permanent' as CreditType,
  daily_temporary_credit_amount: 0,
  description: '',
  price: 0,
  original_price: 0,
  currency: '',
  validity_days: 30,
  validity_unit: 'days',
  sort_order: 0,
  for_sale: true,
  daily_purchase_limit: 0,
  total_purchase_limit: 0,
})
const planFeaturesText = ref('')
const isSub2Benefit = computed(() => planForm.benefit_type === 'sub2')

const benefitTypeOptions = computed(() => [
  { value: 'sub2', label: t('payment.admin.benefitSub2') },
  { value: 'daily_temporary_credit', label: t('payment.admin.benefitDailyTemporaryCredit') },
])

const creditTypeOptions = computed(() => [
  { value: 'permanent', label: t('commerce.creditType.permanent') },
  { value: 'temporary', label: t('commerce.creditType.temporary') },
])

const validityUnitOptions = computed(() => [
  { value: 'days', label: t('payment.admin.days') },
  { value: 'weeks', label: t('payment.admin.weeks') },
  { value: 'months', label: t('payment.admin.months') },
])

function normalizeSub2ValidityUnit(unit?: string): string {
  switch (unit) {
    case 'week':
    case 'weeks':
      return 'weeks'
    case 'month':
    case 'months':
      return 'months'
    default:
      return 'days'
  }
}

const groupOptions = computed(() =>
  props.groups
    .filter(g => g.subscription_type === 'subscription')
    .map(g => ({
      value: g.id,
      label: `${g.name} — ${g.platform} (${g.rate_multiplier}x)`,
      platform: g.platform,
    })),
)

const selectedGroupInfo = computed(() => {
  if (!planForm.group_id) return null
  return props.groups.find(g => g.id === planForm.group_id) || null
})

function creditTypeLabel(type: CreditType): string {
  return t(`commerce.creditType.${type}`)
}

watch(() => planForm.benefit_type, (benefitType) => {
  if (benefitType === 'daily_temporary_credit') {
    planForm.group_id = 0
    planForm.validity_unit = 'day'
  } else if (planForm.group_id === 0) {
    planForm.group_id = null
  }
})

// Reset form when dialog opens
watch(() => props.show, (visible) => {
  if (!visible) return
  if (props.plan) {
    const benefitType = props.plan.benefit_type ?? 'sub2'
    Object.assign(planForm, { name: props.plan.name, group_id: props.plan.group_id, benefit_type: benefitType, payment_credit_type: props.plan.payment_credit_type ?? 'permanent', daily_temporary_credit_amount: props.plan.daily_temporary_credit_amount ?? 0, description: props.plan.description, price: props.plan.price, original_price: props.plan.original_price || 0, currency: props.plan.currency || '', validity_days: props.plan.validity_days, validity_unit: benefitType === 'sub2' ? normalizeSub2ValidityUnit(props.plan.validity_unit) : 'day', sort_order: props.plan.sort_order || 0, for_sale: props.plan.for_sale, daily_purchase_limit: props.plan.daily_purchase_limit ?? 0, total_purchase_limit: props.plan.total_purchase_limit ?? 0 })
    planFeaturesText.value = (props.plan.features || []).join('\n')
  } else {
    Object.assign(planForm, { name: '', group_id: null, benefit_type: 'sub2', payment_credit_type: 'permanent', daily_temporary_credit_amount: 0, description: '', price: 0, original_price: 0, currency: '', validity_days: 30, validity_unit: 'days', sort_order: 0, for_sale: true, daily_purchase_limit: 0, total_purchase_limit: 0 })
    planFeaturesText.value = ''
  }
})

/** Build request payload with snake_case keys matching backend JSON tags */
function buildPlanPayload() {
  const features = planFeaturesText.value.split('\n').map(f => f.trim()).filter(Boolean).join('\n')
  return {
    name: planForm.name,
    group_id: isSub2Benefit.value ? planForm.group_id : 0,
    benefit_type: planForm.benefit_type,
    payment_credit_type: planForm.payment_credit_type,
    daily_temporary_credit_amount: isSub2Benefit.value ? 0 : planForm.daily_temporary_credit_amount,
    description: planForm.description,
    price: planForm.price,
    original_price: planForm.original_price || 0,
    currency: planForm.currency.trim().toUpperCase(),
    validity_days: planForm.validity_days,
    validity_unit: isSub2Benefit.value ? planForm.validity_unit : 'day',
    sort_order: planForm.sort_order,
    for_sale: planForm.for_sale,
    daily_purchase_limit: normalizedPurchaseLimit(planForm.daily_purchase_limit),
    total_purchase_limit: normalizedPurchaseLimit(planForm.total_purchase_limit),
    features,
  }
}

function validPurchaseLimit(value: unknown): boolean {
  return value === '' || (typeof value === 'number' && Number.isInteger(value) && value >= 0)
}

function normalizedPurchaseLimit(value: unknown): number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : 0
}

async function handleSavePlan() {
  if (isSub2Benefit.value && !planForm.group_id) {
    appStore.showError(t('payment.admin.groupRequired'))
    return
  }
  if (!planForm.price || planForm.price <= 0) {
    appStore.showError(t('payment.admin.priceRequired'))
    return
  }
  if (!planForm.validity_days || planForm.validity_days < 1) {
    appStore.showError(t('payment.admin.validityDaysRequired'))
    return
  }
  if (!isSub2Benefit.value && (!Number.isFinite(planForm.daily_temporary_credit_amount) || planForm.daily_temporary_credit_amount <= 0)) {
    appStore.showError(t('payment.admin.dailyTemporaryCreditAmountRequired'))
    return
  }
  if (!validPurchaseLimit(planForm.daily_purchase_limit) || !validPurchaseLimit(planForm.total_purchase_limit)) {
    appStore.showError(t('payment.admin.purchaseLimitInvalid'))
    return
  }
  saving.value = true
  try {
    const data = buildPlanPayload()
    if (props.plan) { await adminPaymentAPI.updatePlan(props.plan.id, data) }
    else { await adminPaymentAPI.createPlan(data) }
    appStore.showSuccess(t('common.saved'))
    emit('close')
    emit('saved')
  } catch (err: unknown) { appStore.showError(extractApiErrorMessage(err, t('common.error'))) }
  finally { saving.value = false }
}
</script>
