<template>
  <BaseDialog
    :show="show"
    :title="operation === 'add' ? t('admin.users.deposit') : t('admin.users.withdraw')"
    width="narrow"
    @close="emit('close')"
  >
    <form
      v-if="user && !grantResult"
      id="balance-form"
      class="space-y-5"
      @submit.prevent="handleBalanceSubmit"
    >
      <div class="flex items-center gap-3 rounded-lg bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100">
          <span class="text-lg font-medium text-primary-700">
            {{ user.email.charAt(0).toUpperCase() }}
          </span>
        </div>
        <div class="min-w-0 flex-1">
          <p class="truncate font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
          <p class="text-sm text-gray-500">
            {{ t('admin.users.currentBalance') }}: ${{ formatDecimalAmount(user.balance) }}
          </p>
        </div>
      </div>

      <div v-if="operation === 'add'">
        <span class="input-label">{{ t('checkin.admin.creditType') }}</span>
        <div
          class="grid h-10 grid-cols-2 overflow-hidden rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-700"
          role="group"
          :aria-label="t('checkin.admin.creditType')"
        >
          <button
            type="button"
            data-testid="credit-type-permanent"
            :aria-pressed="creditType === 'permanent'"
            :class="[
              'text-sm font-medium transition-colors',
              creditType === 'permanent'
                ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white'
                : 'text-gray-500 dark:text-gray-400',
            ]"
            @click="creditType = 'permanent'"
          >
            {{ t('checkin.admin.permanentCredit') }}
          </button>
          <button
            type="button"
            data-testid="credit-type-temporary"
            :aria-pressed="creditType === 'temporary'"
            :class="[
              'text-sm font-medium transition-colors',
              creditType === 'temporary'
                ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white'
                : 'text-gray-500 dark:text-gray-400',
            ]"
            @click="creditType = 'temporary'"
          >
            {{ t('checkin.admin.temporaryCredit') }}
          </button>
        </div>
      </div>

      <div>
        <label for="balance-amount" class="input-label">
          {{ operation === 'add' ? t('admin.users.depositAmount') : t('admin.users.withdrawAmount') }}
        </label>
        <div class="flex gap-2">
          <div class="relative flex-1">
            <div class="absolute left-3 top-1/2 -translate-y-1/2 font-medium text-gray-500">$</div>
            <input
              id="balance-amount"
              v-model="form.amount"
              data-testid="balance-amount"
              type="text"
              inputmode="decimal"
              autocomplete="off"
              required
              class="input pl-8"
            />
          </div>
          <button
            v-if="operation === 'subtract'"
            type="button"
            class="btn btn-secondary whitespace-nowrap"
            @click="fillAllBalance"
          >
            {{ t('admin.users.withdrawAll') }}
          </button>
        </div>
      </div>

      <div
        v-if="operation === 'add' && creditType === 'temporary'"
        class="flex gap-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200"
        data-testid="temporary-expiry-notice"
      >
        <Icon name="clock" size="sm" class="mt-0.5 flex-shrink-0" />
        <span>{{ t('checkin.admin.expiresAt') }}</span>
      </div>

      <div>
        <label for="balance-notes" class="input-label">{{ t('admin.users.notes') }}</label>
        <textarea id="balance-notes" v-model="form.notes" rows="3" class="input" />
      </div>

      <div
        v-if="operation !== 'add' || creditType === 'permanent'"
        class="rounded-lg border border-blue-200 bg-blue-50 p-4 dark:border-blue-800 dark:bg-blue-950"
      >
        <div class="flex items-center justify-between text-sm">
          <span class="text-gray-700 dark:text-gray-300">{{ t('admin.users.newBalance') }}:</span>
          <span class="font-bold text-gray-900 dark:text-gray-100">
            ${{ formatDecimalAmount(calculateNewBalance()) }}
          </span>
        </div>
      </div>
    </form>

    <div
      v-else-if="user && grantResult"
      class="space-y-4"
      data-testid="temporary-grant-result"
    >
      <div class="flex items-center gap-3 text-emerald-700 dark:text-emerald-400">
        <Icon name="check" size="lg" />
        <p class="font-medium">{{ t('checkin.admin.grantSucceeded') }}</p>
      </div>
      <dl class="divide-y divide-gray-100 rounded-lg border border-gray-200 dark:divide-dark-700 dark:border-dark-600">
        <div class="flex items-center justify-between gap-4 px-4 py-3">
          <dt class="text-sm text-gray-500">{{ t('checkin.admin.amount') }}</dt>
          <dd class="font-mono text-sm font-medium text-gray-900 dark:text-white">
            {{ formatDecimalAmount(grantResult.amount) }}
          </dd>
        </div>
        <div class="flex items-start justify-between gap-4 px-4 py-3">
          <dt class="text-sm text-gray-500">{{ t('checkin.admin.actualExpiresAt') }}</dt>
          <dd class="break-all text-right font-mono text-sm text-gray-900 dark:text-white">
            <time :datetime="grantResult.expires_at">{{ grantResult.expires_at }}</time>
          </dd>
        </div>
      </dl>
    </div>

    <template #footer>
      <div v-if="grantResult" class="flex justify-end">
        <button type="button" class="btn btn-primary" @click="emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
      <div v-else class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('close')">
          {{ t('common.cancel') }}
        </button>
        <button
          type="submit"
          form="balance-form"
          :disabled="submitting || !form.amount"
          class="btn"
          :class="operation === 'add' ? 'bg-emerald-600 text-white' : 'btn-danger'"
        >
          {{ submitting ? t('common.saving') : t('common.confirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI, type GrantTemporaryCreditResult } from '@/api/admin'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatDecimalAmount } from '@/utils/format'

type CreditType = 'permanent' | 'temporary'

const amountPattern = /^(?:0|[1-9][0-9]{0,11})(?:\.[0-9]{1,8})?$/

const props = defineProps<{
  show: boolean
  user: AdminUser | null
  operation: 'add' | 'subtract'
}>()
const emit = defineEmits<{
  (event: 'close'): void
  (event: 'success'): void
}>()
const { t } = useI18n()
const appStore = useAppStore()

const submitting = ref(false)
const creditType = ref<CreditType>('permanent')
const idempotencyKey = ref('')
const grantResult = ref<GrantTemporaryCreditResult | null>(null)
const form = reactive({ amount: '', notes: '' })

watch(
  () => props.show,
  (visible) => {
    if (!visible) return
    form.amount = ''
    form.notes = ''
    creditType.value = 'permanent'
    idempotencyKey.value = ''
    grantResult.value = null
  },
)

watch(
  [() => form.amount, () => form.notes, creditType],
  () => {
    idempotencyKey.value = ''
  },
)

function fillAllBalance() {
  if (!props.user) return
  form.amount = new Intl.NumberFormat('en-US', {
    useGrouping: false,
    maximumFractionDigits: 8,
  }).format(props.user.balance)
}

function calculateNewBalance(): number {
  if (!props.user) return 0
  const amount = Number(form.amount)
  if (!Number.isFinite(amount)) return props.user.balance
  const result = props.operation === 'add'
    ? props.user.balance + amount
    : props.user.balance - amount
  return Math.abs(result) < 1e-10 ? 0 : result
}

function isValidAmount(value: string): boolean {
  if (!amountPattern.test(value)) return false
  const [integer, fraction = ''] = value.split('.')
  return integer !== '0' || /[1-9]/.test(fraction)
}

function createIdempotencyKey(userID: number): string {
  const uuid = globalThis.crypto?.randomUUID?.()
  if (uuid) return `admin-temp-credit-${userID}-${uuid}`
  return `admin-temp-credit-${userID}-${Date.now()}-${Math.random().toString(36).slice(2)}`
}

async function handleBalanceSubmit() {
  if (submitting.value) return
  if (!props.user) return

  if (props.operation === 'add' && creditType.value === 'temporary') {
    if (!isValidAmount(form.amount)) {
      appStore.showError(t('checkin.admin.invalidTemporaryAmount'))
      return
    }

    submitting.value = true
    try {
      if (!idempotencyKey.value) {
        idempotencyKey.value = createIdempotencyKey(props.user.id)
      }
      grantResult.value = await adminAPI.users.grantTemporaryCredit(
        props.user.id,
        { amount: form.amount, notes: form.notes },
        idempotencyKey.value,
      )
      appStore.showSuccess(t('checkin.admin.grantSucceeded'))
      emit('success')
    } catch (error: any) {
      console.error('Failed to grant temporary credit:', error)
      appStore.showError(error?.message || t('common.error'))
    } finally {
      submitting.value = false
    }
    return
  }

  if (!isValidAmount(form.amount)) {
    appStore.showError(t('admin.users.amountRequired'))
    return
  }
  const amount = Number(form.amount)
  if (props.operation === 'subtract' && amount > props.user.balance) {
    appStore.showError(t('admin.users.insufficientBalance'))
    return
  }

  submitting.value = true
  try {
    await adminAPI.users.updateBalance(props.user.id, amount, props.operation, form.notes)
    appStore.showSuccess(t('common.success'))
    emit('success')
    emit('close')
  } catch (error: any) {
    console.error('Failed to update balance:', error)
    appStore.showError(error?.response?.data?.detail || error?.message || t('common.error'))
  } finally {
    submitting.value = false
  }
}
</script>
