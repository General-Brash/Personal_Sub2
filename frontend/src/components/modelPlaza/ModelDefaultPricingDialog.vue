<template>
  <BaseDialog :show="show" :title="t('modelPlaza.defaultPricing.title')" width="wide" :close-on-escape="!saving" :show-close-button="!saving" @close="close">
    <div class="space-y-4">
      <p class="rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">{{ t('modelPlaza.defaultPricing.scope') }}</p>
      <div class="flex items-end gap-2">
        <label class="min-w-0 flex-1 text-sm font-medium">
          {{ t('modelPlaza.defaultPricing.modelId') }}
          <input v-model="requestedModel" data-testid="default-pricing-model" class="input mt-1 w-full font-mono" maxlength="256" :readonly="!!modelId || !!detail" placeholder="vendor/model-v1" @keydown.enter.prevent="load" />
        </label>
        <button v-if="!detail" type="button" class="btn-secondary shrink-0" data-testid="default-pricing-load" :disabled="loading || !requestedModel.trim()" @click="load">{{ t('modelPlaza.defaultPricing.load') }}</button>
      </div>
      <p v-if="loading" role="status" class="text-sm text-gray-500">{{ t('modelPlaza.defaultPricing.loading') }}</p>
      <p v-if="error" role="alert" class="text-sm text-red-600 dark:text-red-400">{{ error }}</p>
      <template v-if="detail">
        <div class="rounded-lg bg-gray-50 p-3 text-xs text-gray-600 dark:bg-dark-800 dark:text-gray-300">
          <p class="break-all">{{ t('modelPlaza.defaultPricing.pricingKey') }}: <code>{{ detail.pricing_key }}</code> · {{ detail.source }}</p>
          <p class="mt-1 break-all">{{ t('modelPlaza.defaultPricing.baseline') }}: {{ detail.system_baseline.source }} / {{ detail.system_baseline.match_type }} · <code>{{ detail.system_baseline.matched_model_id }}</code></p>
          <p class="mt-1">{{ t('modelPlaza.defaultPricing.revisions', { saved: detail.version, loaded: detail.loaded_version, seconds: detail.refresh_interval_seconds }) }}</p>
          <p v-if="detail.refresh_error || detail.version !== detail.loaded_version" class="mt-1 text-amber-700 dark:text-amber-300">{{ t('modelPlaza.defaultPricing.refreshPending') }}</p>
        </div>
        <p v-if="!detail.has_exact_system_standard && !detail.has_admin_override" data-testid="default-pricing-new-standard" class="text-sm text-primary-700 dark:text-primary-300">{{ t('modelPlaza.defaultPricing.newStandard') }}</p>
        <label class="block text-sm font-medium">
          {{ t('modelPlaza.defaultPricing.mode') }}
          <select v-model="mode" data-testid="default-pricing-mode" class="input mt-1 w-full" :disabled="saving || !canWrite">
            <option v-for="item in modes" :key="item" :value="item" :disabled="!detail.supported_modes.includes(item)">{{ t(`modelPlaza.defaultPricing.modes.${item}`) }}{{ !detail.supported_modes.includes(item) ? ` — ${t('modelPlaza.defaultPricing.unsupported')}` : '' }}</option>
          </select>
        </label>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('modelPlaza.defaultPricing.overrideNotice') }}</p>
        <p v-if="mode === 'token' && detail.cache_fallback_to_input" class="rounded bg-blue-50 p-2 text-xs text-blue-800 dark:bg-blue-900/20 dark:text-blue-200">{{ t('modelPlaza.defaultPricing.newCachePolicy') }}</p>
        <div class="space-y-3">
          <div v-for="field in visibleFields" :key="field" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <div class="flex flex-wrap items-center justify-between gap-2">
              <label :for="`default-price-${field}`" class="text-sm font-medium">{{ t(`modelPlaza.defaultPricing.fields.${field}`) }} <span v-if="requiredFields.includes(field)" class="text-red-500">*</span></label>
              <span class="text-xs text-gray-500">{{ unitLabel }}</span>
            </div>
            <div class="my-2 grid grid-cols-3 gap-2 text-xs text-gray-500 dark:text-gray-400">
              <p>{{ t('modelPlaza.defaultPricing.systemValue') }}<span class="mt-1 block font-mono">{{ formatValue(detail.system_baseline.prices[field]) }}</span></p>
              <p>{{ t('modelPlaza.defaultPricing.adminValue') }}<span class="mt-1 block font-mono">{{ formatValue(detail.admin_override[field]) }}</span></p>
              <p>{{ t('modelPlaza.defaultPricing.effectiveValue') }}<span :data-testid="`default-pricing-effective-${field}`" class="mt-1 block font-mono">{{ formatValue(previewValue(field)) }}</span></p>
            </div>
            <div class="flex items-center gap-2">
              <input :id="`default-price-${field}`" :value="draft[field] ?? ''" :data-testid="`default-pricing-${field}`" type="number" min="0" step="any" class="input min-w-0 flex-1 font-mono" :placeholder="formatValue(detail.effective_pricing[field])" :disabled="saving || !canWrite" @input="editField(field, ($event.target as HTMLInputElement).value)" />
              <button type="button" class="btn-secondary shrink-0 text-xs" :data-testid="`default-pricing-inherit-${field}`" :disabled="saving || !canWrite" @click="inheritField(field)">{{ t('modelPlaza.defaultPricing.inherit') }}</button>
            </div>
            <p v-if="actions[field] === 'inherit'" class="mt-1 text-xs text-primary-600">{{ t('modelPlaza.defaultPricing.willInherit') }}</p>
            <p v-if="previewValue(field) === 0" class="mt-1 text-xs text-amber-700 dark:text-amber-300">{{ t('modelPlaza.defaultPricing.explicitZero') }}</p>
          </div>
        </div>
        <p class="text-xs text-gray-500 dark:text-gray-400">{{ t('modelPlaza.defaultPricing.advancedPreserved') }}</p>
        <p v-if="dirty && validationError" data-testid="default-pricing-validation" class="text-sm text-amber-700 dark:text-amber-300">{{ validationError }}</p>
        <div v-if="conflict" role="alert" data-testid="default-pricing-conflict" class="space-y-2 rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">
          <p>{{ t('modelPlaza.defaultPricing.conflict') }}</p>
          <ul v-if="latest" class="space-y-1 break-all font-mono text-xs">
            <li v-for="field in conflictFields" :key="field">{{ field }}: {{ String(detail.admin_override[field] ?? '—') }} → {{ String(latest.admin_override[field] ?? '—') }}</li>
          </ul>
          <button v-if="latest" type="button" class="btn-secondary" data-testid="default-pricing-accept-latest" @click="acceptLatest">{{ t('modelPlaza.defaultPricing.acceptLatest') }}</button>
          <button v-else type="button" class="btn-secondary" @click="loadLatest">{{ t('modelPlaza.defaultPricing.retryLatest') }}</button>
        </div>
        <p v-if="saved" role="status" data-testid="default-pricing-saved" class="text-sm text-green-700 dark:text-green-300">{{ t('modelPlaza.defaultPricing.saved') }}</p>
        <div v-if="confirmReset" class="rounded-lg border border-amber-300 p-3 text-sm">
          <p>{{ t('modelPlaza.defaultPricing.resetConfirm') }}</p>
          <div class="mt-2 flex gap-2"><button type="button" class="btn-secondary" data-testid="default-pricing-confirm-reset" :disabled="saving" @click="submit(true)">{{ t('modelPlaza.defaultPricing.confirmReset') }}</button><button type="button" class="btn-secondary" :disabled="saving" @click="confirmReset = false">{{ t('modelPlaza.defaultPricing.cancel') }}</button></div>
        </div>
      </template>
    </div>
    <template #footer>
      <div class="flex w-full flex-wrap items-center justify-between gap-2">
        <button v-if="detail?.has_admin_override && canWrite" type="button" class="btn-secondary" data-testid="default-pricing-reset" :disabled="saving || conflict || !systemCanPrice" :title="!systemCanPrice ? t('modelPlaza.defaultPricing.resetBlocked') : ''" @click="confirmReset = true">{{ t('modelPlaza.defaultPricing.reset') }}</button>
        <span v-else class="text-xs text-gray-500">{{ !canWrite ? t('modelPlaza.defaultPricing.readOnly') : '' }}</span>
        <div class="ml-auto flex gap-2">
          <button type="button" class="btn-secondary" :disabled="saving" @click="close">{{ t('modelPlaza.defaultPricing.close') }}</button>
          <button v-if="canWrite" type="button" class="btn-primary" data-testid="default-pricing-save" :disabled="!detail || loading || saving || conflict || !dirty || !!validationError" @click="submit(false)">{{ t(saving ? 'modelPlaza.defaultPricing.saving' : 'modelPlaza.defaultPricing.save') }}</button>
        </div>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { mTokToPerToken, perTokenToMTok } from '@/components/admin/channel/types'
import { getModelDefaultPricing, saveModelDefaultPricing, resetModelDefaultPricing, type DefaultPricingMoneyField, type DefaultPricingMode, type DefaultPricingPatch, type ModelDefaultPricingDetail } from '@/api/admin/modelPlaza'

const props = defineProps<{ show: boolean; modelId?: string }>()
const emit = defineEmits<{ close: []; saved: [detail: ModelDefaultPricingDetail] }>()
const { t } = useI18n()
const auth = useAuthStore()
const canWrite = computed(() => auth.canAdmin('models.pricing.write'))
const modes: DefaultPricingMode[] = ['token', 'per_request', 'image', 'video']
const fieldsByMode: Record<DefaultPricingMode, DefaultPricingMoneyField[]> = {
  token: ['input_price', 'output_price', 'cache_write_price', 'cache_write_1h_price', 'cache_read_price', 'image_input_price', 'image_output_price'],
  per_request: ['per_request_price'], image: ['image_price_1k', 'image_price_2k', 'image_price_4k'], video: ['video_price_480p', 'video_price_720p', 'video_price_1080p'],
}
const requestedModel = ref('')
const detail = ref<ModelDefaultPricingDetail | null>(null)
const latest = ref<ModelDefaultPricingDetail | null>(null)
const mode = ref<DefaultPricingMode>('token')
const initialMode = ref<DefaultPricingMode>('token')
const draft = ref<Partial<Record<DefaultPricingMoneyField, string>>>({})
const actions = ref<Partial<Record<DefaultPricingMoneyField, 'set' | 'inherit'>>>({})
const loading = ref(false), saving = ref(false), saved = ref(false), conflict = ref(false), confirmReset = ref(false)
const error = ref('')
let controller: AbortController | undefined
const visibleFields = computed(() => fieldsByMode[mode.value])
const requiredFields = computed(() => mode.value === 'token' ? fieldsByMode.token.slice(0, 2) : visibleFields.value)
const dirty = computed(() => !!detail.value && (mode.value !== initialMode.value || Object.keys(actions.value).length > 0))
const unitLabel = computed(() => t(`modelPlaza.defaultPricing.units.${mode.value}`))
const systemCanPrice = computed(() => {
  const base = detail.value?.system_baseline.prices
  const baseMode = base?.billing_mode
  if (!base || !baseMode) return false
  if (baseMode === 'token' && detail.value?.system_baseline.match_type === 'family') return false
  return (baseMode === 'token' ? fieldsByMode.token.slice(0, 2) : fieldsByMode[baseMode]).every((field) => base[field] != null)
})
const conflictFields = computed(() => (['billing_mode', ...Object.values(fieldsByMode).flat()] as const).filter((field) => detail.value?.admin_override[field] !== latest.value?.admin_override[field]))

function formatValue(value: number | undefined | null): string {
  if (value == null) return '—'
  return String(mode.value === 'token' ? perTokenToMTok(value) : value)
}
function parsedValue(field: DefaultPricingMoneyField): number | undefined {
  const raw = draft.value[field]?.trim()
  if (!raw) return undefined
  const number = Number(raw)
  if (!Number.isFinite(number) || number < 0) return undefined
  const value = mode.value === 'token' ? mTokToPerToken(number) : number
  return value != null && value <= 1_000_000 ? value : undefined
}
function previewValue(field: DefaultPricingMoneyField): number | undefined {
  if (actions.value[field] === 'set') return parsedValue(field)
  const inherited = actions.value[field] === 'inherit' || detail.value?.admin_override[field] == null
  if (mode.value === 'token' && inherited && detail.value?.system_baseline.prices[field] == null) {
    if (field === 'image_input_price') return previewValue('input_price')
    if (field === 'image_output_price') return previewValue('output_price')
  }
  if (mode.value === 'token' && detail.value?.cache_fallback_to_input && inherited && detail.value.system_baseline.prices[field] == null) {
    if (field === 'cache_read_price' || field === 'cache_write_price') return previewValue('input_price')
    if (field === 'cache_write_1h_price') return previewValue('cache_write_price')
  }
  if (actions.value[field] === 'inherit') return detail.value?.system_baseline.prices[field]
  return detail.value?.effective_pricing[field]
}
const validationError = computed(() => {
  if (!detail.value) return ''
  if (mode.value === 'token' && detail.value.system_baseline.match_type === 'family') {
    const hasExactValue = (field: DefaultPricingMoneyField) => actions.value[field] === 'set'
      ? parsedValue(field) != null
      : actions.value[field] !== 'inherit' && detail.value?.admin_override[field] != null
    if (!hasExactValue('input_price') || !hasExactValue('output_price')) return t('modelPlaza.defaultPricing.exactRequired')
  }
  if (visibleFields.value.some((field) => actions.value[field] === 'set' && parsedValue(field) == null)) return t('modelPlaza.defaultPricing.invalidAmount')
  if (requiredFields.value.some((field) => previewValue(field) == null)) return t('modelPlaza.defaultPricing.required')
  return ''
})
function initialize(data: ModelDefaultPricingDetail) {
  detail.value = data
  mode.value = data.effective_pricing.billing_mode || 'token'
  initialMode.value = mode.value
  draft.value = {}
  for (const field of Object.values(fieldsByMode).flat()) {
    const value = data.admin_override[field]
    if (value != null) draft.value[field] = String(fieldsByMode.token.includes(field) ? perTokenToMTok(value) : value)
  }
  actions.value = {}; latest.value = null; conflict.value = false; confirmReset.value = false
}
async function load() {
  if (!requestedModel.value.trim() || loading.value || saving.value) return
  controller?.abort(); controller = new AbortController()
  const signal = controller.signal
  loading.value = true; error.value = ''; saved.value = false
  try {
    const result = await getModelDefaultPricing(requestedModel.value.trim(), { signal })
    if (!signal.aborted) initialize(result)
  } catch {
    if (!signal.aborted) error.value = t('modelPlaza.defaultPricing.loadFailed')
  } finally { if (!signal.aborted) loading.value = false }
}
function editField(field: DefaultPricingMoneyField, value: string) { draft.value[field] = value; actions.value[field] = 'set'; saved.value = false }
function inheritField(field: DefaultPricingMoneyField) {
  draft.value[field] = ''
  if (detail.value?.admin_override[field] != null) actions.value[field] = 'inherit'
  else delete actions.value[field]
  saved.value = false
}
function makePatch(): DefaultPricingPatch {
  const patch: DefaultPricingPatch = {}
  if (!detail.value?.has_admin_override || mode.value !== initialMode.value) patch.billing_mode = mode.value
  if (mode.value !== initialMode.value) {
    for (const field of Object.values(fieldsByMode).flat()) {
      if (!visibleFields.value.includes(field) && detail.value?.admin_override[field] != null) patch[field] = null
    }
  }
  for (const field of visibleFields.value) {
    if (actions.value[field] === 'inherit') patch[field] = null
    else if (actions.value[field] === 'set') patch[field] = parsedValue(field)!
  }
  return patch
}
async function loadLatest() {
  try { latest.value = await getModelDefaultPricing(requestedModel.value.trim()) }
  catch { error.value = t('modelPlaza.defaultPricing.latestFailed') }
}
function acceptLatest() {
  if (!latest.value) return
  const changedMode = mode.value !== initialMode.value || (Object.keys(actions.value).length > 0 && mode.value !== (latest.value.effective_pricing.billing_mode || 'token'))
  const preservedDraft = { ...draft.value }, preservedActions = { ...actions.value }, selectedMode = mode.value
  initialize(latest.value)
  if (changedMode) mode.value = selectedMode
  actions.value = preservedActions
  for (const field of Object.keys(preservedActions) as DefaultPricingMoneyField[]) draft.value[field] = preservedDraft[field]
  error.value = ''
}
async function submit(reset: boolean) {
  if (!detail.value || saving.value || !canWrite.value || conflict.value || (!reset && validationError.value)) return
  saving.value = true; error.value = ''; saved.value = false
  try {
    const result = reset
      ? await resetModelDefaultPricing(requestedModel.value.trim(), detail.value.version)
      : await saveModelDefaultPricing(requestedModel.value.trim(), detail.value.version, makePatch())
    initialize(result); saved.value = true; emit('saved', result)
  } catch (cause) {
    const failure = cause as { status?: number; code?: string; reason?: string; response?: { status?: number; data?: { reason?: string; code?: string } } }
    const reason = failure.reason || failure.code || failure.response?.data?.reason || failure.response?.data?.code
    const status = failure.status ?? failure.response?.status
    if (status === 409 && reason === 'MODEL_DEFAULT_PRICING_VERSION_CONFLICT') {
      conflict.value = true
      await loadLatest()
    } else {
      error.value = t(reason === 'MODEL_DEFAULT_PRICING_RESET_UNAVAILABLE' ? 'modelPlaza.defaultPricing.resetBlocked' : 'modelPlaza.defaultPricing.saveFailed')
    }
  } finally { saving.value = false }
}
function close() { if (!saving.value) emit('close') }
watch(() => [props.show, props.modelId] as const, ([show, modelId]) => {
  controller?.abort(); loading.value = false
  if (!show) return
  requestedModel.value = modelId || ''; detail.value = null; latest.value = null
  draft.value = {}; actions.value = {}; error.value = ''; saved.value = false; conflict.value = false; confirmReset.value = false
  if (modelId) void load()
}, { immediate: true })
onBeforeUnmount(() => controller?.abort())
</script>