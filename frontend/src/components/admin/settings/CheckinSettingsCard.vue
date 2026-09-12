<template>
  <section :class="{ card: props.showHeader }" data-testid="checkin-settings-card">
    <div
      v-if="props.showHeader"
      class="border-b border-gray-100 px-6 py-4 dark:border-dark-700"
    >
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

    <fieldset
      v-else
      :disabled="saving"
      :class="['space-y-5', props.showHeader ? 'p-6' : 'p-0']"
    >
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

      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label for="daily-checkin-refresh-time" class="input-label">刷新时间（Asia/Shanghai）</label>
          <input id="daily-checkin-refresh-time" v-model="form.refresh_time" data-testid="checkin-refresh-time" type="time" step="60" required class="input" />
        </div>
        <div>
          <label for="daily-checkin-auto-fee" class="input-label">自动签到手续费（bps）</label>
          <input id="daily-checkin-auto-fee" v-model.number="form.auto_fee_bps" data-testid="checkin-auto-fee-bps" type="number" min="0" max="10000" step="1" required class="input" />
        </div>
      </div>

      <div class="grid gap-4 rounded-lg border border-gray-200 p-4 sm:grid-cols-2 dark:border-dark-600">
        <div class="space-y-3">
          <div class="flex items-center justify-between gap-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">普通博弈</span>
            <Toggle v-model="form.normal.enabled" data-testid="checkin-normal-enabled" aria-label="普通博弈" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="input-label">最小倍率 bps<input v-model.number="form.normal.min_bps" data-testid="checkin-normal-min-bps" type="number" min="1" :max="form.normal.max_bps" step="1" required class="input mt-1" /></label>
            <label class="input-label">最大倍率 bps<input v-model.number="form.normal.max_bps" data-testid="checkin-normal-max-bps" type="number" :min="form.normal.min_bps" max="1000000" step="1" required class="input mt-1" /></label>
          </div>
        </div>
        <div class="space-y-3">
          <div class="flex items-center justify-between gap-3">
            <span class="text-sm font-medium text-gray-700 dark:text-gray-300">超级博弈</span>
            <Toggle v-model="form.super.enabled" data-testid="checkin-super-enabled" aria-label="超级博弈" />
          </div>
          <div class="grid grid-cols-2 gap-3">
            <label class="input-label">最小倍率 bps<input v-model.number="form.super.min_bps" data-testid="checkin-super-min-bps" type="number" min="1" :max="form.super.max_bps" step="1" required class="input mt-1" /></label>
            <label class="input-label">最大倍率 bps<input v-model.number="form.super.max_bps" data-testid="checkin-super-max-bps" type="number" :min="form.super.min_bps" max="1000000" step="1" required class="input mt-1" /></label>
          </div>
          <label class="input-label">永久成本<input v-model="form.super.cost" data-testid="checkin-super-cost" type="text" inputmode="decimal" required class="input mt-1 font-mono" @blur="form.super.cost = formatEditableAmount(form.super.cost)" /></label>
        </div>
        <div class="flex items-center justify-between gap-3 sm:col-span-2">
          <span class="text-sm font-medium text-gray-700 dark:text-gray-300">随机功能已完成合规复核</span>
          <Toggle v-model="form.reviewed" data-testid="checkin-reviewed" aria-label="随机功能已完成合规复核" />
        </div>
      </div>

      <p data-testid="checkin-policy-effective-preview" class="rounded-lg bg-gray-50 p-3 text-sm text-gray-600 dark:bg-dark-800 dark:text-gray-300">
        {{ effectivePreview }}
      </p>

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
        <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
          <div class="min-w-[34rem]">
            <div class="grid grid-cols-[minmax(6rem,0.6fr)_minmax(10rem,1fr)_minmax(10rem,1fr)] items-center gap-4 border-b border-gray-200 bg-gray-50 px-4 py-2 text-xs font-medium text-gray-500 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-400">
              <span>{{ t('checkin.admin.rewardDayColumn') }}</span>
              <span>{{ t('checkin.admin.temporaryCredit') }}</span>
              <span>{{ t('checkin.admin.permanentCredit') }}</span>
            </div>
            <div
              v-for="tier in visibleRewardTiers"
              :key="tier.day"
              class="grid min-h-14 grid-cols-[minmax(6rem,0.6fr)_minmax(10rem,1fr)_minmax(10rem,1fr)] items-center gap-4 border-b border-gray-100 px-4 py-3 last:border-b-0 dark:border-dark-700"
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
              @blur="tier.amount = formatEditableAmount(tier.amount)"
              @keydown.enter.prevent="saveSettings"
            />
            <input
              :id="`daily-checkin-permanent-tier-${tier.day}`"
              v-model="tier.permanent_amount"
              :data-testid="`checkin-permanent-tier-${tier.day}`"
              :aria-label="`${t('checkin.admin.permanentCredit')} ${t('checkin.admin.tierDay', { day: tier.day })}`"
              type="text"
              inputmode="decimal"
              autocomplete="off"
              required
              class="input font-mono"
              @blur="tier.permanent_amount = formatEditableAmount(tier.permanent_amount)"
              @keydown.enter.prevent="saveSettings"
            />
            </div>
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
import { getCheckinSettingsV2, updateCheckinSettingsV2, type CheckinAdminSettings } from '@/api/checkin'
import { useAppStore } from '@/stores/app'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'

const amountPattern = /^(?:0|[1-9][0-9]{0,11})(?:\.[0-9]{1,8})?$/
const MAX_REWARD_DAY = 365

const { t } = useI18n()
const appStore = useAppStore()

const props = withDefaults(defineProps<{
  showHeader?: boolean
}>(), {
  showHeader: true,
})

type CheckinSettingsForm = CheckinAdminSettings

const loading = ref(true)
const saving = ref(false)
const loadError = ref(false)
const form = reactive<CheckinSettingsForm>({
  enabled: false,
  max_reward_day: 7,
  reward_tiers: Array.from({ length: 7 }, (_, index) => ({
    day: index + 1,
    amount: '1.00',
    permanent_amount: '0.00',
  })),
  version: '',
  refresh_time: '00:00',
  auto_fee_bps: 500,
  reviewed: false,
  normal: { enabled: false, min_bps: 10000, max_bps: 10000 },
  super: { enabled: false, min_bps: 10000, max_bps: 10000, cost: '0.00000000' },
  pending_refresh: null,
  next_reset_at: null,
})

const effectivePreview = computed(() => {
  if (form.pending_refresh) {
    return `刷新时间将在 ${formatDateTime(form.pending_refresh.effective_at)} 对新周期生效`
  }
  return form.next_reset_at
    ? `当前规则持续至 ${formatDateTime(form.next_reset_at)}，届时按刷新时间切换`
    : '刷新时间变更会在当前周期结束后的下一边界生效'
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
        amount: '1.00',
        permanent_amount: '0.00',
      })
    }
    form.reward_tiers.forEach((tier, index) => {
      tier.day = index + 1
    })
  },
  { flush: 'sync' },
)

function applySettings(settings: CheckinAdminSettings) {
  form.enabled = settings.enabled
  form.max_reward_day = settings.max_reward_day
  form.reward_tiers = settings.reward_tiers.map((tier) => ({
    day: tier.day,
    amount: formatEditableAmount(tier.amount),
    permanent_amount: formatEditableAmount(tier.permanent_amount || '0.00000000'),
  }))
  form.version = settings.version
  form.refresh_time = settings.refresh_time || '00:00'
  form.auto_fee_bps = settings.auto_fee_bps ?? 0
  form.reviewed = Boolean(settings.reviewed)
  const normal = settings.normal ?? { enabled: false, min_bps: 10000, max_bps: 10000 }
  const superMode = settings.super ?? { enabled: false, min_bps: 10000, max_bps: 10000, cost: '0.00000000' }
  form.normal = { enabled: Boolean(normal.enabled), min_bps: normal.min_bps ?? 10000, max_bps: normal.max_bps ?? 10000 }
  form.super = { ...superMode, enabled: Boolean(superMode.enabled), min_bps: superMode.min_bps ?? 10000, max_bps: superMode.max_bps ?? 10000, cost: formatEditableAmount(superMode.cost || '0.00000000') }
  form.pending_refresh = settings.pending_refresh ?? null
  form.next_reset_at = settings.next_reset_at ?? null
}

function formatDateTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

/** Keep a readable two-place default without rounding away sub-cent precision. */
function formatEditableAmount(value: string): string {
  const normalized = value.trim()
  if (!amountPattern.test(normalized)) return value
  const [integer, fraction = ''] = normalized.split('.')
  const significantFraction = fraction.replace(/0+$/, '')
  return `${integer}.${significantFraction.padEnd(2, '0')}`
}

function isValidPositiveAmount(value: string): boolean {
  if (!amountPattern.test(value)) return false
  const [integer, fraction = ''] = value.split('.')
  return integer !== '0' || /[1-9]/.test(fraction)
}

function isValidNonNegativeAmount(value: string): boolean {
  return amountPattern.test(value)
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
    applySettings(await getCheckinSettingsV2())
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
  if (
    !/^([01][0-9]|2[0-3]):[0-5][0-9]$/.test(form.refresh_time) ||
    !Number.isInteger(form.auto_fee_bps) || form.auto_fee_bps < 0 || form.auto_fee_bps > 10000 ||
    !Number.isInteger(form.normal.min_bps) || !Number.isInteger(form.normal.max_bps) ||
    form.normal.min_bps < 1 || form.normal.min_bps > form.normal.max_bps || form.normal.max_bps > 1000000 ||
    !Number.isInteger(form.super.min_bps) || !Number.isInteger(form.super.max_bps) ||
    form.super.min_bps < 1 || form.super.min_bps > form.super.max_bps || form.super.max_bps > 1000000 ||
    !isValidNonNegativeAmount(form.super.cost) || (form.super.enabled && !isValidPositiveAmount(form.super.cost))
  ) {
    appStore.showError(t('checkin.admin.invalidRewardAmount'))
    return
  }
  const rewardTiers = form.reward_tiers.slice(0, form.max_reward_day)
  if (
    rewardTiers.length !== form.max_reward_day ||
    rewardTiers.some(
      (tier, index) =>
        tier.day !== index + 1 ||
        !isValidPositiveAmount(tier.amount) ||
        !isValidNonNegativeAmount(tier.permanent_amount),
    )
  ) {
    appStore.showError(t('checkin.admin.invalidRewardAmount'))
    return
  }

  saving.value = true
  try {
    const saved = await updateCheckinSettingsV2({
      enabled: form.enabled,
      max_reward_day: form.max_reward_day,
      reward_tiers: rewardTiers.map((tier) => ({
        day: tier.day,
        amount: tier.amount,
        permanent_amount: tier.permanent_amount,
      })),
      version: form.version,
      expected_version: form.version,
      refresh_time: form.refresh_time,
      auto_fee_bps: form.auto_fee_bps,
      reviewed: form.reviewed,
      normal: { ...form.normal },
      super: { ...form.super },
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
