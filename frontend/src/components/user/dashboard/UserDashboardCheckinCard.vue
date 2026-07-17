<template>
  <section class="card" data-test="dashboard-checkin-card">
    <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('checkin.title') }}</h2>
    </div>

    <div v-if="loading" class="flex min-h-36 items-center justify-center">
      <Icon name="refresh" size="md" class="animate-spin text-primary-600" />
    </div>

    <div v-else-if="status" class="space-y-4 p-5">
      <div class="flex items-center justify-between gap-3">
        <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('checkin.title') }}</span>
        <span data-test="today-status" class="rounded-md bg-primary-50 px-2 py-1 text-xs font-medium text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
          {{ todayStatus }}
        </span>
      </div>

      <div class="grid grid-cols-2 gap-3">
        <div class="min-w-0 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.currentStreak') }}</p>
          <p data-test="dashboard-streak" class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">
            {{ t('checkin.streakDays', { count: status.current_streak_day }) }}
          </p>
        </div>
        <div class="min-w-0 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.nextReward') }}</p>
          <p data-test="dashboard-reward" class="mt-1 min-w-0 max-w-full break-all text-sm font-semibold leading-5 text-primary-600 dark:text-primary-400">
            {{ formatCredit(status.next_reward_amount) }}
          </p>
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.rewardDay', { day: status.next_reward_day }) }}</p>
        </div>
        <div class="min-w-0 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.temporaryCreditAvailable') }}</p>
          <p data-test="dashboard-temporary-credit" class="mt-1 min-w-0 max-w-full break-all text-sm font-semibold leading-5 text-emerald-600 dark:text-emerald-400">
            {{ formatCredit(status.temporary_credit_available) }}
          </p>
        </div>
        <div class="min-w-0 rounded-lg bg-gray-50 p-3 dark:bg-dark-800">
          <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('checkin.expiresAt') }}</p>
          <p
            data-test="dashboard-earliest-expiry"
            :data-expires-at="status.temporary_credit_earliest_expires_at || undefined"
            class="mt-1 text-sm font-medium text-gray-900 dark:text-white"
          >
            {{ formatExpiry(status.temporary_credit_earliest_expires_at) }}
          </p>
        </div>
      </div>

      <RouterLink
        data-test="checkin-details-link"
        to="/check-in"
        class="inline-flex w-full items-center justify-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-700"
      >
        {{ t('checkin.title') }}
        <Icon name="chevronRight" size="sm" />
      </RouterLink>
    </div>

    <div v-else class="min-h-36 p-5">
      <RouterLink
        data-test="checkin-details-link"
        to="/check-in"
        class="inline-flex w-full items-center justify-center gap-2 rounded-lg border border-gray-200 px-3 py-2 text-sm font-medium text-gray-700 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-gray-200 dark:hover:bg-dark-700"
      >
        {{ t('checkin.title') }}
        <Icon name="chevronRight" size="sm" />
      </RouterLink>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { getCheckinStatus, type CheckinStatus } from '@/api/checkin'
import Icon from '@/components/icons/Icon.vue'
import { formatDecimalAmount } from '@/utils/format'

const props = defineProps<{
  status?: CheckinStatus | null
  loading?: boolean
}>()
const { t } = useI18n()
const localStatus = ref<CheckinStatus | null>(null)
const localLoading = ref(true)
const status = computed(() => props.status === undefined ? localStatus.value : props.status)
const loading = computed(() => props.loading ?? localLoading.value)

const todayStatus = computed(() => {
  if (!status.value?.enabled) return t('checkin.disabled')
  return status.value.today_checked_in ? t('checkin.checkedIn') : t('checkin.checkIn')
})

async function loadStatus() {
  localLoading.value = true
  try {
    localStatus.value = await getCheckinStatus(getBeijingMonth())
  } catch (error) {
    console.error('Failed to load dashboard check-in summary:', error)
  } finally {
    localLoading.value = false
  }
}

function getBeijingMonth(): string {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
  }).formatToParts(new Date())
  const values = Object.fromEntries(parts.filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  return `${values.year}-${values.month}`
}

function formatCredit(value: string): string {
  return `$${formatDecimalAmount(value)}`
}

function formatExpiry(value: string | null): string {
  if (!value) return '-'
  const expiry = new Date(value)
  if (Number.isNaN(expiry.getTime())) return value
  return new Intl.DateTimeFormat(undefined, {
    timeZone: 'Asia/Shanghai',
    month: 'numeric',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(expiry)
}

onMounted(() => {
  if (props.status === undefined) void loadStatus()
})
</script>
