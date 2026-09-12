<template>
  <section class="card p-5" data-test="bank-exchange-expiry-settings">
    <div class="flex flex-wrap items-start justify-between gap-3">
      <div>
        <h3 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('bank.adminExchangeExpiry.title') }}
        </h3>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t('bank.adminExchangeExpiry.description') }}
        </p>
      </div>
      <span class="rounded-full bg-gray-100 px-2.5 py-1 text-xs font-medium text-gray-600 dark:bg-dark-700 dark:text-gray-300">
        {{ t('bank.adminExchangeExpiry.policyVersion', { version: policy?.policy_version ?? '-' }) }}
      </span>
    </div>

    <div v-if="loading" class="mt-5 text-sm text-gray-500">
      {{ t('bank.adminExchangeExpiry.loading') }}
    </div>
    <div v-else-if="loadFailed" class="mt-5 rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-200">
      <p>{{ t('bank.adminExchangeExpiry.loadFailed') }}</p>
      <button type="button" class="btn btn-secondary mt-3" @click="load">
        {{ t('bank.actions.refresh') }}
      </button>
    </div>
    <form v-else class="mt-5 space-y-5" @submit.prevent="save">
      <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
        <input v-model="form.enabled" type="checkbox" class="mt-1 h-4 w-4 rounded border-gray-300 text-primary-600">
        <span>
          <span class="block text-sm font-medium text-gray-900 dark:text-white">{{ t('bank.adminExchangeExpiry.enabled') }}</span>
          <span class="mt-1 block text-xs text-gray-500 dark:text-gray-400">{{ t('bank.adminExchangeExpiry.enabledHint') }}</span>
        </span>
      </label>

      <div class="grid gap-4 sm:grid-cols-2">
        <label class="block">
          <span class="input-label">{{ t('bank.adminExchangeExpiry.feeBps') }}</span>
          <input
            v-model.number="form.fee_bps"
            data-test="bank-exchange-expiry-fee-bps"
            type="number"
            min="0"
            max="10000"
            step="1"
            class="input mt-1 font-mono"
            :disabled="saving"
          >
          <span class="input-hint mt-1.5 block">{{ t('bank.adminExchangeExpiry.feeBpsHint', { rate: feeRate }) }}</span>
        </label>
        <label class="block">
          <span class="input-label">{{ t('bank.adminExchangeExpiry.localTime') }}</span>
          <input
            v-model="form.local_time"
            data-test="bank-exchange-expiry-local-time"
            type="time"
            class="input mt-1"
            :disabled="saving"
          >
          <span class="input-hint mt-1.5 block">{{ t('bank.adminExchangeExpiry.localTimeHint', { timezone: policy?.timezone ?? 'Asia/Shanghai' }) }}</span>
        </label>
      </div>

      <div class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-950 dark:border-amber-800 dark:bg-amber-900/20 dark:text-amber-100">
        <p class="font-medium">{{ t('bank.adminExchangeExpiry.previewTitle') }}</p>
        <ul class="mt-2 space-y-1.5 text-xs">
          <li>{{ t('bank.adminExchangeExpiry.defaultFeePreview', { bps: policy?.default_fee_bps ?? 1000, rate: defaultFeeRate }) }}</li>
          <li>{{ t('bank.adminExchangeExpiry.expiryPreview', { timezone: policy?.timezone ?? 'Asia/Shanghai', time: form.local_time || '00:00' }) }}</li>
          <li>{{ t('bank.adminExchangeExpiry.newGrantsOnlyPreview') }}</li>
          <li>{{ t('bank.adminExchangeExpiry.independentPreview') }}</li>
        </ul>
      </div>

      <div v-if="error" class="rounded-lg bg-red-50 p-3 text-sm text-red-700 dark:bg-red-900/20 dark:text-red-200" role="alert">
        {{ error }}
      </div>
      <div v-if="saved" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-200">
        {{ t('bank.adminExchangeExpiry.saved') }}
      </div>

      <div class="flex justify-end">
        <button type="submit" class="btn btn-primary" :disabled="saving" data-test="bank-exchange-expiry-save">
          {{ saving ? t('bank.actions.saving') : t('bank.actions.save') }}
        </button>
      </div>
    </form>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getBankExchangeExpirySettings,
  updateBankExchangeExpirySettings,
  type BankExchangeExpiryPolicy,
} from '@/api/bank'

const { t } = useI18n()
const loading = ref(true)
const saving = ref(false)
const loadFailed = ref(false)
const error = ref('')
const saved = ref(false)
const policy = ref<BankExchangeExpiryPolicy | null>(null)
const form = reactive({
  enabled: false,
  fee_bps: 1000,
  local_time: '00:00',
})

const feeRate = computed(() => formatFeeRate(form.fee_bps))
const defaultFeeRate = computed(() => formatFeeRate(policy.value?.default_fee_bps ?? 1000))

function formatFeeRate(feeBps: number): string {
  if (!Number.isFinite(feeBps) || feeBps < 0 || feeBps > 10000) return '-'
  return `${(feeBps / 100).toFixed(2)}%`
}

function createIdempotencyKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `bank-exchange-expiry-${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function applyPolicy(next: BankExchangeExpiryPolicy): void {
  policy.value = next
  form.enabled = next.enabled
  form.fee_bps = next.fee_bps
  form.local_time = next.local_time || '00:00'
}

async function load(): Promise<void> {
  loading.value = true
  loadFailed.value = false
  error.value = ''
  saved.value = false
  try {
    applyPolicy(await getBankExchangeExpirySettings())
  } catch (err) {
    loadFailed.value = true
    error.value = err instanceof Error ? err.message : t('bank.adminExchangeExpiry.loadFailed')
  } finally {
    loading.value = false
  }
}

async function save(): Promise<void> {
  if (saving.value || !policy.value) return
  error.value = ''
  saved.value = false
  if (!Number.isInteger(form.fee_bps) || form.fee_bps < 0 || form.fee_bps > 10000) {
    error.value = t('bank.adminExchangeExpiry.invalidFee')
    return
  }
  if (!/^(?:[01]\d|2[0-3]):[0-5]\d$/.test(form.local_time)) {
    error.value = t('bank.adminExchangeExpiry.invalidLocalTime')
    return
  }
  saving.value = true
  try {
    const updated = await updateBankExchangeExpirySettings({
      ...policy.value,
      enabled: form.enabled,
      fee_bps: form.fee_bps,
      fee_rate: formatFeeRate(form.fee_bps),
      local_time: form.local_time,
    }, createIdempotencyKey())
    applyPolicy(updated)
    saved.value = true
  } catch (err) {
    error.value = err instanceof Error ? err.message : t('bank.adminExchangeExpiry.saveFailed')
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>
