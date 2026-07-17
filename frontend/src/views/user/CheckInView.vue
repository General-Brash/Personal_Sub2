<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('checkin.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('checkin.description') }}</p>
        </div>
        <button
          v-if="status?.enabled"
          data-test="check-in-button"
          type="button"
          class="inline-flex min-h-10 items-center justify-center gap-2 rounded-lg bg-primary-600 px-4 py-2 text-sm font-medium text-white transition-colors hover:bg-primary-700 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="!canCheckIn"
          @click="handleCheckIn"
        >
          <Icon name="gift" size="sm" />
          {{ checkInButtonLabel }}
        </button>
      </div>

      <div v-if="loading" class="card flex min-h-48 items-center justify-center">
        <Icon name="refresh" size="lg" class="animate-spin text-primary-600" />
      </div>

      <div v-else-if="loadFailed && !status" class="card flex min-h-48 flex-col items-center justify-center gap-3 p-6 text-center">
        <p class="text-sm text-gray-600 dark:text-gray-300">{{ t('checkin.failedToLoad') }}</p>
        <button
          type="button"
          class="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700"
          :aria-label="t('checkin.failedToLoad')"
          @click="loadStatus()"
        >
          <Icon name="refresh" size="sm" />
        </button>
      </div>

      <template v-else-if="status">
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-5">
          <div class="card min-w-0 p-4">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('checkin.currentStreak') }}</p>
            <p data-test="current-streak" class="mt-2 text-2xl font-bold text-gray-900 dark:text-white">
              {{ t('checkin.streakDays', { count: status.current_streak_day }) }}
            </p>
          </div>
          <div class="card p-4">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('checkin.nextReward') }}</p>
            <p data-test="next-reward" class="mt-2 min-w-0 break-all text-lg font-semibold text-primary-600 dark:text-primary-400">
              {{ t('checkin.rewardDay', { day: status.next_reward_day }) }} / {{ formatCredit(status.next_reward_amount) }}
            </p>
          </div>
          <div class="card min-w-0 p-4">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('checkin.temporaryCreditAvailable') }}</p>
            <p data-test="temporary-credit" class="mt-2 min-w-0 break-all text-lg font-semibold text-emerald-600 dark:text-emerald-400">
              {{ formatCredit(status.temporary_credit_available) }}
            </p>
          </div>
          <div class="card min-w-0 p-4">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('checkin.expiresAt') }}</p>
            <p class="mt-2 text-sm font-semibold text-gray-900 dark:text-white">
              {{ formatExpiry(status.temporary_credit_earliest_expires_at) }}
            </p>
          </div>
          <div class="card p-4">
            <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ t('checkin.monthlyRewardTotal') }}</p>
            <p data-test="monthly-reward-total" class="mt-2 min-w-0 break-all text-lg font-semibold text-gray-900 dark:text-white">
              {{ formatCredit(status.monthly_reward_total) }}
            </p>
          </div>
        </div>

        <section class="card overflow-hidden">
          <div class="flex items-center justify-between border-b border-gray-100 px-5 py-4 dark:border-dark-700">
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('checkin.calendar') }}</h2>
            <div class="flex items-center gap-2">
              <button
                data-test="previous-month-button"
                type="button"
                class="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700"
                :aria-label="t('checkin.previousMonth')"
                :title="t('checkin.previousMonth')"
                @click="changeMonth(-1)"
              >
                <Icon name="chevronLeft" size="sm" />
              </button>
              <span data-test="current-month" class="min-w-20 text-center text-sm text-gray-500 dark:text-gray-400">
                {{ currentMonth }}
              </span>
              <button
                data-test="next-month-button"
                type="button"
                class="inline-flex h-9 w-9 items-center justify-center rounded-lg border border-gray-200 text-gray-600 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:text-gray-300 dark:hover:bg-dark-700"
                :aria-label="t('checkin.nextMonth')"
                :title="t('checkin.nextMonth')"
                @click="changeMonth(1)"
              >
                <Icon name="chevronRight" size="sm" />
              </button>
            </div>
          </div>
          <div class="grid grid-cols-7 gap-px bg-gray-100 dark:bg-dark-700">
            <div
              v-for="cell in calendarCells"
              :key="cell.index"
              data-test="calendar-cell"
              :data-date="cell.date || undefined"
              class="min-h-20 min-w-0 bg-white p-2 dark:bg-dark-800"
              :class="cell.entry ? 'ring-1 ring-inset ring-primary-500/50' : ''"
            >
              <div v-if="cell.day" class="flex h-full min-w-0 flex-col justify-between gap-2">
                <span class="text-sm font-medium text-gray-800 dark:text-gray-100">{{ cell.day }}</span>
                <span
                  v-if="cell.entry"
                  data-test="calendar-reward"
                  class="block w-full min-w-0 max-w-full break-all rounded-md bg-primary-50 px-1 py-0.5 text-[10px] font-medium leading-tight text-primary-700 dark:bg-primary-900/30 dark:text-primary-300"
                >
                  {{ formatCredit(cell.entry.reward_amount) }}
                </span>
              </div>
            </div>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { checkIn, getCheckinStatus, type CheckinCalendarEntry, type CheckinResult, type CheckinStatus } from '@/api/checkin'
import { useAppStore } from '@/stores/app'
import { formatDecimalAmount } from '@/utils/format'

interface CalendarCell {
  index: number
  day: number | null
  date: string | null
  entry?: CheckinCalendarEntry
}

const idempotencyKeyStorage = 'daily-checkin-idempotency-key'
const idempotencyDateStorage = 'daily-checkin-idempotency-date'

const { t } = useI18n()
const appStore = useAppStore()
const status = ref<CheckinStatus | null>(null)
const loading = ref(true)
const loadFailed = ref(false)
const submitting = ref(false)
const currentMonth = ref(getBeijingDate().slice(0, 7))
let latestStatusRequest = 0

const canCheckIn = computed(() => Boolean(status.value?.enabled && !status.value.today_checked_in && !submitting.value))
const checkInButtonLabel = computed(() => {
  if (submitting.value) return t('checkin.checkingIn')
  if (!status.value?.enabled) return t('checkin.disabled')
  if (status.value.today_checked_in) return t('checkin.checkedIn')
  return t('checkin.checkIn')
})

const calendarEntries = computed(() => {
  const entries = new Map<string, CheckinCalendarEntry>()
  for (const entry of status.value?.calendar ?? []) entries.set(entry.checkin_date, entry)
  return entries
})

const calendarCells = computed<CalendarCell[]>(() => {
  const [year, month] = currentMonth.value.split('-').map(Number)
  if (!Number.isInteger(year) || !Number.isInteger(month) || month < 1 || month > 12) {
    return Array.from({ length: 42 }, (_, index) => ({ index, day: null, date: null }))
  }

  const firstWeekday = new Date(Date.UTC(year, month - 1, 1)).getUTCDay()
  const daysInMonth = new Date(Date.UTC(year, month, 0)).getUTCDate()

  return Array.from({ length: 42 }, (_, index) => {
    const day = index - firstWeekday + 1
    if (day < 1 || day > daysInMonth) return { index, day: null, date: null }

    const date = `${currentMonth.value}-${String(day).padStart(2, '0')}`
    return { index, day, date, entry: calendarEntries.value.get(date) }
  })
})

async function loadStatus(month = currentMonth.value) {
  const request = ++latestStatusRequest
  if (!status.value) loading.value = true
  if (month === currentMonth.value) loadFailed.value = false
  try {
    const nextStatus = await getCheckinStatus(month)
    if (request !== latestStatusRequest || month !== currentMonth.value) return
    status.value = nextStatus
  } catch (error) {
    if (request !== latestStatusRequest || month !== currentMonth.value) return
    console.error('Failed to load daily check-in status:', error)
    loadFailed.value = true
  } finally {
    if (request === latestStatusRequest) loading.value = false
  }
}

function changeMonth(offset: number) {
  const [year, month] = currentMonth.value.split('-').map(Number)
  const target = new Date(Date.UTC(year, month - 1 + offset, 1))
  currentMonth.value = `${target.getUTCFullYear()}-${String(target.getUTCMonth() + 1).padStart(2, '0')}`
  void loadStatus(currentMonth.value)
}

async function handleCheckIn() {
  if (!canCheckIn.value) return

  submitting.value = true
  try {
    const result = await checkIn(getOrCreateIdempotencyKey())
    applyCheckinResult(result)
    appStore.showSuccess(t(result.already_checked_in ? 'checkin.alreadyCheckedIn' : 'checkin.checkInSucceeded'))
    await loadStatus()
  } catch (error) {
    console.error('Failed to complete daily check-in:', error)
    appStore.showError(t('checkin.failedToCheckIn'))
  } finally {
    submitting.value = false
  }
}

function applyCheckinResult(result: CheckinResult) {
  if (!status.value) return

  const updatedCalendar = result.checkin_date.startsWith(currentMonth.value)
    ? [
        ...status.value.calendar.filter((entry) => entry.checkin_date !== result.checkin_date),
        {
          checkin_date: result.checkin_date,
          streak_day: result.streak_day,
          reward_day: result.reward_day,
          reward_amount: result.reward_amount,
        },
      ].sort((left, right) => left.checkin_date.localeCompare(right.checkin_date))
    : status.value.calendar

  status.value = {
    ...status.value,
    today_checked_in: true,
    current_streak_day: result.streak_day,
    calendar: updatedCalendar,
  }
}

function getOrCreateIdempotencyKey(): string {
  const businessDate = getBeijingDate()
  const storedDate = localStorage.getItem(idempotencyDateStorage)
  const storedKey = localStorage.getItem(idempotencyKeyStorage)
  if (storedDate === businessDate && storedKey) return storedKey

  const randomPart = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(36).slice(2)}`
  const key = `check-in-${businessDate}-${randomPart}`
  localStorage.setItem(idempotencyDateStorage, businessDate)
  localStorage.setItem(idempotencyKeyStorage, key)
  return key
}

function getBeijingDate(): string {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: 'Asia/Shanghai',
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
  }).formatToParts(new Date())
  const values = Object.fromEntries(parts.filter((part) => part.type !== 'literal').map((part) => [part.type, part.value]))
  return `${values.year}-${values.month}-${values.day}`
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
  void loadStatus()
})
</script>
