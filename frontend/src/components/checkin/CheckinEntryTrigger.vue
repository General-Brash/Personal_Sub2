<template>
  <div v-if="showPrompt" role="status" class="fixed bottom-5 right-5 z-40 max-w-sm rounded-xl border border-gray-200 bg-white p-4 shadow-lg dark:border-dark-600 dark:bg-dark-800">
    <p class="text-sm font-medium text-gray-900 dark:text-white">{{ needsConsent ? '自动签到规则已更新，请重新确认手续费。' : '本周期尚未签到，可选择直接领取或已启用的博弈模式。' }}</p>
    <div class="mt-3 flex justify-end gap-2">
      <button type="button" class="btn btn-secondary" @click="dismiss">稍后</button>
      <RouterLink to="/check-in" class="btn btn-primary" @click="dismiss">前往签到</RouterLink>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { autoCheckIn, getCheckinPreference, getCheckinStatus, type CheckinResult } from '@/api/checkin'

const props = defineProps<{ userId: number }>()
const emit = defineEmits<{ (event: 'completed', result: CheckinResult): void; (event: 'error', error: unknown): void }>()
const showPrompt = ref(false), needsConsent = ref(false)
let promptKey = ''
let channel: BroadcastChannel | null = null
let active = true
function dismiss() {
  showPrompt.value = false
  if (promptKey) { try { sessionStorage.setItem(promptKey, 'dismissed') } catch { /* optional cache */ } }
}
function completedInAnotherView() { showPrompt.value = false }

onBeforeUnmount(() => { active = false; channel?.close(); window.removeEventListener('personal-checkin-completed', completedInAnotherView) })

onMounted(async () => {
  window.addEventListener('personal-checkin-completed', completedInAnotherView)
  if (typeof BroadcastChannel !== 'undefined') {
    channel = new BroadcastChannel(`personal-checkin:${props.userId}`)
    channel.onmessage = () => { showPrompt.value = false }
  }
  try {
    // Use the server's business month/window; the browser's UTC date is not a
    // reliable period at Shanghai month boundaries.
    const status = await getCheckinStatus()
    if (!active || !status.enabled || status.today_checked_in || !status.business_period_id) return
    const preference = await getCheckinPreference()
    if (!active) return
    if (!preference.auto_enabled || !preference.consent_valid) {
      needsConsent.value = preference.auto_enabled === true
      promptKey = `checkin-prompt:${props.userId}:${status.business_period_id}`
      let dismissed = false
      try { dismissed = sessionStorage.getItem(promptKey) === 'dismissed' } catch { /* optional cache */ }
      if (!dismissed && window.location.pathname !== '/check-in') showPrompt.value = true
      return
    }
    const storageKey = `daily-checkin-auto:${props.userId}:${status.business_period_id}`
    let requestKey: string = crypto.randomUUID()
    try {
      requestKey = sessionStorage.getItem(storageKey) || requestKey
      sessionStorage.setItem(storageKey, requestKey)
    } catch {
      // Storage is an optional retry cache, never the authority for awards.
    }
    if (!active) return
    const result = await autoCheckIn(requestKey)
    if (active) { channel?.postMessage({ period: result.business_period_id }); emit('completed', result) }
  } catch (error) {
    if (active) emit('error', error)
  }
})
</script>
