<template>
  <div v-if="showPrompt" role="status" data-test="checkin-entry-trigger" class="fixed bottom-5 right-5 z-40 max-w-sm rounded-xl border border-gray-200 bg-white p-4 shadow-lg dark:border-dark-600 dark:bg-dark-800">
    <p v-if="autoFailure" data-test="auto-checkin-error" class="text-sm font-medium text-red-700 dark:text-red-300">
      {{ autoFailure }}
    </p>
    <p v-else class="text-sm font-medium text-gray-900 dark:text-white">
      {{ needsConsent ? t('checkin.auto.consentRequired') : t('checkin.auto.pending') }}
    </p>
    <div class="mt-3 flex flex-wrap justify-end gap-2">
      <button
        v-if="autoFailure && canRetry"
        type="button"
        data-test="auto-checkin-retry"
        class="btn btn-secondary"
        :disabled="retrying"
        @click="retryAutomaticCheckin"
      >
        {{ retrying ? t('checkin.auto.retrying') : t('checkin.auto.retry') }}
      </button>
      <button type="button" class="btn btn-secondary" @click="dismiss">{{ t('checkin.auto.dismiss') }}</button>
      <RouterLink to="/check-in" class="btn btn-primary" @click="dismiss">{{ t('checkin.checkIn') }}</RouterLink>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import { autoCheckIn, getCheckinPreference, getCheckinStatus, type CheckinResult } from '@/api/checkin'

const props = defineProps<{ userId: number }>()
const emit = defineEmits<{ (event: 'completed', result: CheckinResult): void; (event: 'error', error: unknown): void }>()
const { t } = useI18n()

const MAX_AUTO_CHECKIN_ATTEMPTS = 2
const showPrompt = ref(false)
const needsConsent = ref(false)
const autoFailure = ref('')
const retrying = ref(false)
const autoAttempts = ref(0)
const canRetry = computed(() => autoAttempts.value < MAX_AUTO_CHECKIN_ATTEMPTS)
let promptKey = ''
let autoRequestStorageKey = ''
let autoRequestKey = ''
let autoRequestPeriod = ''
let channel: BroadcastChannel | null = null
let active = true
let runInFlight = false

function createRequestKey(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  return `${Date.now()}-${Math.random().toString(16).slice(2)}`
}

function isPromptDismissed(key: string): boolean {
  if (!key) return false
  try {
    return sessionStorage.getItem(key) === 'dismissed'
  } catch {
    return false
  }
}

function readStoredAttempts(key: string): number {
  try {
    const attempts = Number(sessionStorage.getItem(`${key}:attempts`))
    return Number.isInteger(attempts) && attempts >= 0 ? attempts : 0
  } catch {
    return 0
  }
}

function storeAttempts(): void {
  if (!autoRequestStorageKey) return
  try {
    sessionStorage.setItem(`${autoRequestStorageKey}:attempts`, String(autoAttempts.value))
  } catch {
    // Storage is an optional retry cache, never the authority for awards.
  }
}

function getOrCreateRequestKey(period: string): string {
  if (autoRequestPeriod !== period) {
    autoRequestPeriod = period
    autoRequestStorageKey = `daily-checkin-auto:${props.userId}:${period}`
    autoRequestKey = ''
    autoAttempts.value = readStoredAttempts(autoRequestStorageKey)
    autoFailure.value = ''
  }
  if (autoRequestKey) return autoRequestKey

  const generated = createRequestKey()
  try {
    autoRequestKey = sessionStorage.getItem(autoRequestStorageKey) || generated
    sessionStorage.setItem(autoRequestStorageKey, autoRequestKey)
  } catch {
    autoRequestKey = generated
  }
  return autoRequestKey
}

function dismiss() {
  showPrompt.value = false
  if (promptKey) {
    try { sessionStorage.setItem(promptKey, 'dismissed') } catch { /* optional cache */ }
  }
}

function clearPrompt() {
  showPrompt.value = false
  needsConsent.value = false
  autoFailure.value = ''
}

function completedInAnotherView() {
  clearPrompt()
}

async function attemptAutomaticCheckin(): Promise<void> {
  if (!active || runInFlight) return
  runInFlight = true
  retrying.value = true
  try {
    const status = await getCheckinStatus()
    if (!active) return
    if (!status.enabled || status.today_checked_in || !status.business_period_id) {
      clearPrompt()
      return
    }

    promptKey = `checkin-prompt:${props.userId}:${status.business_period_id}`
    const preference = await getCheckinPreference()
    if (!active) return
    if (!preference.auto_enabled || !preference.consent_valid) {
      autoFailure.value = ''
      needsConsent.value = preference.auto_enabled === true
      showPrompt.value = !isPromptDismissed(promptKey) && window.location.pathname !== '/check-in'
      return
    }

    needsConsent.value = false
    const requestKey = getOrCreateRequestKey(status.business_period_id)
    if (autoAttempts.value >= MAX_AUTO_CHECKIN_ATTEMPTS) {
      autoFailure.value = t('checkin.auto.failed')
      showPrompt.value = true
      return
    }

    autoAttempts.value += 1
    storeAttempts()
    const result = await autoCheckIn(requestKey)
    if (!active) return
    autoFailure.value = ''
    showPrompt.value = false
    channel?.postMessage({ period: result.business_period_id })
    emit('completed', result)
  } catch (error) {
    if (!active) return
    needsConsent.value = false
    autoFailure.value = t('checkin.auto.failed')
    showPrompt.value = true
    emit('error', error)
  } finally {
    runInFlight = false
    retrying.value = false
  }
}

function retryAutomaticCheckin(): void {
  void attemptAutomaticCheckin()
}

onBeforeUnmount(() => {
  active = false
  channel?.close()
  window.removeEventListener('personal-checkin-completed', completedInAnotherView)
})

onMounted(() => {
  window.addEventListener('personal-checkin-completed', completedInAnotherView)
  if (typeof BroadcastChannel !== 'undefined') {
    channel = new BroadcastChannel(`personal-checkin:${props.userId}`)
    channel.onmessage = () => clearPrompt()
  }
  void attemptAutomaticCheckin()
})
</script>
