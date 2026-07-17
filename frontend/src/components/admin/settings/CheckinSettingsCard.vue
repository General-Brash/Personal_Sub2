<template>
  <section class="card" data-testid="checkin-settings-card">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('checkin.admin.settingsTitle') }}
      </h2>
    </div>

    <div v-if="loading" class="flex min-h-40 items-center justify-center p-6">
      <Icon name="refresh" size="md" class="animate-spin text-primary-500" />
    </div>

    <div
      v-else-if="loadError"
      class="flex min-h-40 flex-col items-center justify-center gap-3 p-6 text-center"
      role="alert"
      data-testid="checkin-settings-error"
    >
      <p class="text-sm text-red-600 dark:text-red-400">
        {{ t('checkin.admin.failedToLoadSettings') }}
      </p>
      <button type="button" class="btn btn-secondary" @click="loadSettings">
        <Icon name="refresh" size="sm" />
        {{ t('checkin.admin.retry') }}
      </button>
    </div>

    <fieldset v-else :disabled="saving" class="space-y-5 p-6">
      <div class="flex items-center justify-between gap-4">
        <label
          for="daily-checkin-enabled"
          class="text-sm font-medium text-gray-700 dark:text-gray-300"
        >
          {{ t('checkin.admin.enabled') }}
        </label>
        <Toggle
          id="daily-checkin-enabled"
          v-model="form.enabled"
          :aria-label="t('checkin.admin.enabled')"
        />
      </div>

      <div class="max-w-xs">
        <label for="daily-checkin-max-reward-day" class="input-label">
          {{ t('checkin.admin.maxRewardDay') }}
        </label>
        <input
          id="daily-checkin-max-reward-day"
          v-model.number="form.max_reward_day"
          data-testid="checkin-max-reward-day"
          type="number"
          min="1"
          :max="MAX_REWARD_DAY"
          step="1"
          required
          class="input"
          @keydown.enter.prevent="saveSettings"
        />
      </div>

      <div>
        <h3 class="mb-3 text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('checkin.admin.rewardTiers') }}
        </h3>
        <div class="overflow-hidden rounded-lg border border-gray-200 dark:border-dark-600">
          <div
            v-for="tier in visibleRewardTiers"
            :key="tier.day"
            class="grid min-h-14 grid-cols-[minmax(7rem,0.7fr)_minmax(10rem,1fr)] items-center gap-4 border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700"
          >
            <label
              :for="`daily-checkin-tier-${tier.day}`"
              class="text-sm text-gray-700 dark:text-gray-300"
            >
              {{ t('checkin.admin.tierDay', { day: tier.day }) }}
            </label>
            <input
              :id="`daily-checkin-tier-${tier.day}`"
              v-model="tier.amount"
              :data-testid="`checkin-tier-${tier.day}`"
              type="text"
              inputmode="decimal"
              autocomplete="off"
              required
              class="input font-mono"
              @keydown.enter.prevent="saveSettings"
            />
          </div>
        </div>
      </div>

      <div class="flex justify-end">
        <button
          type="button"
          class="btn btn-primary"
          :disabled="saving"
          data-testid="save-checkin-settings"
          @click="saveSettings"
        >
          <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
          {{ saving ? t('common.saving') : t('common.save') }}
        </button>
      </div>
    </fieldset>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { CheckinSettings } from '@/api/admin/settings'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'

const amountPattern = /^(?:0|[1-9][0-9]{0,11})(?:\.[0-9]{1,8})?$/
const MAX_REWARD_DAY = 365

const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(true)
const saving = ref(false)
const loadError = ref(false)
const form = reactive<CheckinSettings>({
  enabled: false,
  max_reward_day: 7,
  reward_tiers: Array.from({ length: 7 }, (_, index) => ({
    day: index + 1,
    amount: '1.00000000',
  })),
})

const visibleRewardTiers = computed(() => {
  if (!isValidMaxRewardDay(form.max_reward_day)) return form.reward_tiers
  return form.reward_tiers.slice(0, form.max_reward_day)
})

watch(
  () => form.max_reward_day,
  (value) => {
    if (!isValidMaxRewardDay(value)) return

    while (form.reward_tiers.length < value) {
      form.reward_tiers.push({
        day: form.reward_tiers.length + 1,
        amount: '1.00000000',
      })
    }
    form.reward_tiers.forEach((tier, index) => {
      tier.day = index + 1
    })
  },
  { flush: 'sync' },
)

function applySettings(settings: CheckinSettings) {
  form.enabled = settings.enabled
  form.max_reward_day = settings.max_reward_day
  form.reward_tiers = settings.reward_tiers.map((tier) => ({
    day: tier.day,
    amount: tier.amount,
  }))
}

function isValidPositiveAmount(value: string): boolean {
  if (!amountPattern.test(value)) return false
  const [integer, fraction = ''] = value.split('.')
  return integer !== '0' || /[1-9]/.test(fraction)
}

function isValidMaxRewardDay(value: unknown): value is number {
  return (
    typeof value === 'number' &&
    Number.isInteger(value) &&
    value >= 1 &&
    value <= MAX_REWARD_DAY
  )
}

async function loadSettings() {
  loading.value = true
  loadError.value = false
  try {
    applySettings(await adminAPI.settings.getCheckinSettings())
  } catch (error) {
    console.error('Failed to load daily check-in settings:', error)
    loadError.value = true
  } finally {
    loading.value = false
  }
}

async function saveSettings() {
  if (saving.value) return

  if (!isValidMaxRewardDay(form.max_reward_day)) {
    appStore.showError(t('checkin.admin.invalidMaxRewardDay'))
    return
  }
  const rewardTiers = form.reward_tiers.slice(0, form.max_reward_day)
  if (
    rewardTiers.length !== form.max_reward_day ||
    rewardTiers.some(
      (tier, index) => tier.day !== index + 1 || !isValidPositiveAmount(tier.amount),
    )
  ) {
    appStore.showError(t('checkin.admin.invalidRewardAmount'))
    return
  }

  saving.value = true
  try {
    const saved = await adminAPI.settings.updateCheckinSettings({
      enabled: form.enabled,
      max_reward_day: form.max_reward_day,
      reward_tiers: rewardTiers.map((tier) => ({
        day: tier.day,
        amount: tier.amount,
      })),
    })
    applySettings(saved)
    appStore.showSuccess(t('checkin.admin.settingsSaved'))
  } catch (error) {
    console.error('Failed to save daily check-in settings:', error)
    appStore.showError(t('checkin.admin.failedToSaveSettings'))
  } finally {
    saving.value = false
  }
}

onMounted(loadSettings)
</script>
