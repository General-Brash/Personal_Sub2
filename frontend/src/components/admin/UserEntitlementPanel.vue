<template>
  <BaseDialog :show="show" :title="t('admin.settings.userEntitlement.title')" width="wide" @close="close">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2 text-sm text-gray-500 dark:text-dark-400">
        <span>{{ t('admin.settings.userEntitlement.targetsSummary', { count: targets.length }) }}</span>
        <span :class="canWrite ? 'text-emerald-600' : 'text-amber-600'" class="font-medium">{{ loading ? t('admin.settings.userEntitlement.verifying') : canWrite ? t('admin.settings.userEntitlement.writable') : t('admin.settings.userEntitlement.readOnly') }}</span>
      </div>
      <p v-if="!canWrite" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ readOnlyReason }}</p>
      <p v-if="loadError" role="alert" class="text-sm text-red-600">{{ loadError }}</p>
      <p v-if="catalogError" role="status" class="text-sm text-amber-700">{{ catalogError }}{{ t('admin.settings.userEntitlement.catalogErrorSuffix') }}</p>
      <p v-if="writeNotice" data-testid="entitlement-status" role="status" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-200">{{ writeNotice }}</p>
      <div v-if="outcomeUnknown && pendingSubmission" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">
        <p>{{ t('admin.settings.userEntitlement.pendingUnknown', { targets: pendingSubmission.userIds.join(', '), tier: pendingSubmission.tier }) }}</p>
        <p class="mt-1 break-all text-xs">{{ t('admin.settings.userEntitlement.requestId', { id: pendingSubmission.requestId }) }}</p>
        <p v-if="storageUnavailable">{{ t('admin.settings.userEntitlement.storageUnavailable') }}</p>
        <button data-testid="entitlement-retry" class="btn btn-secondary mt-2" :disabled="applying" @click="submitPending">{{ applying ? t('admin.settings.userEntitlement.checking') : t('admin.settings.userEntitlement.retryOriginal') }}</button>
      </div>
      <p v-if="writeError" role="alert" class="text-sm text-red-600">{{ writeError }}</p>
      <button class="btn btn-secondary" :disabled="loading || applying" @click="loadCurrent">{{ t('admin.settings.userEntitlement.reload') }}</button>
      <div v-if="loading && !entitlement" class="py-8 text-center text-sm text-gray-500">{{ t('admin.settings.userEntitlement.loadingBasic') }}</div>
      <template v-if="entitlement">
        <div class="grid gap-3 text-sm sm:grid-cols-3">
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">{{ t('admin.settings.userEntitlement.currentTier') }}</span><div class="font-semibold">{{ entitlement.tier }}</div></div>
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">{{ t('admin.settings.userEntitlement.version') }}</span><div class="font-semibold">{{ entitlement.version }}</div></div>
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">{{ t('admin.settings.userEntitlement.premiumPolicy') }}</span><div class="font-semibold">{{ premiumPolicyLabel }}</div></div>
        </div>
        <div v-if="catalog" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex flex-wrap gap-2">
            <button v-for="tier in tiers" :key="tier" :data-testid="`entitlement-preview-${tier}`" :disabled="!canPreview || !!pendingSubmission" class="btn" :class="pendingTier === tier ? 'btn-primary' : 'btn-secondary'" @click="preview(tier)">{{ t('admin.settings.userEntitlement.previewTier', { tier }) }}</button>
          </div>
          <p class="mt-2 text-xs text-gray-500">{{ t('admin.settings.userEntitlement.applyNote') }}</p>
          <label class="mt-3 block text-sm"><span class="mb-1 block text-gray-600 dark:text-dark-300">{{ t('admin.settings.userEntitlement.reasonLabel') }}</span><input v-model.trim="reason" :disabled="!!pendingSubmission" data-testid="entitlement-reason" class="input w-full" :placeholder="t('admin.settings.userEntitlement.reasonPlaceholder')" /></label>
          <p v-if="previewLoading" role="status" class="mt-3 text-sm">{{ t('admin.settings.userEntitlement.previewing') }}</p>
          <p v-if="previewError" role="alert" class="mt-3 text-sm text-red-600">{{ previewError }}</p>
          <div v-if="previewResult" data-testid="entitlement-preview" class="mt-3 rounded-lg bg-blue-50 p-3 text-sm dark:bg-blue-900/20">
            <div>{{ t('admin.settings.userEntitlement.previewSummary', { affected: previewResult.affected_user_ids?.length ?? 0, already: previewResult.already_at_tier?.length ?? 0 }) }}</div>
            <div class="mt-1 text-xs">{{ t('admin.settings.userEntitlement.previewGroups', { granted: previewResult.granted_group_ids?.join(', ') || t('admin.settings.userEntitlement.none'), revoked: previewResult.revoked_group_ids?.join(', ') || t('admin.settings.userEntitlement.none') }) }}</div>
            <div class="mt-1 text-xs">{{ t('admin.settings.userEntitlement.previewMeta', { version: previewResult.policy_version, expires: previewResult.expires_at }) }}</div>
            <p v-if="previewResult.policy_enabled !== true" class="mt-2 text-amber-700">{{ t('admin.settings.userEntitlement.previewDisabled') }}</p>
          </div>
          <button data-testid="entitlement-apply" class="btn btn-primary mt-3" :disabled="!canApply" @click="apply">{{ applying ? t('admin.settings.userEntitlement.applying') : t('admin.settings.userEntitlement.confirmApply') }}</button>
        </div>
        <div class="text-sm">
          <div class="font-medium text-gray-900 dark:text-white">{{ t('admin.settings.userEntitlement.sourcesTitle') }}</div>
          <ul class="mt-2 space-y-1 text-gray-600 dark:text-dark-300">
            <li v-for="source in entitlement.sources ?? []" :key="`${source.source}-${source.tier}-${source.group_id ?? 'tier'}`">{{ source.explain }} · {{ source.tier }}<span v-if="source.group_id"> · group {{ source.group_id }}</span><span v-if="source.rate != null"> · {{ source.rate }}x</span></li>
            <li v-if="!entitlement.sources?.length">{{ t('admin.settings.userEntitlement.defaultStandard') }}</li>
          </ul>
        </div>
        <div class="grid gap-3 text-sm sm:grid-cols-2">
          <div><span class="text-gray-500">{{ t('admin.settings.userEntitlement.manualGroups') }}</span>{{ entitlement.manual_groups?.join(', ') || t('admin.settings.userEntitlement.none') }}</div>
          <div><span class="text-gray-500">{{ t('admin.settings.userEntitlement.subscriptionGroups') }}</span>{{ entitlement.subscription_groups?.join(', ') || t('admin.settings.userEntitlement.none') }}</div>
        </div>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAuthStore } from '@/stores/auth'
import {
  getEntitlementCatalog, getUserEntitlement, previewEntitlementChange,
  applyEntitlementChange, updateUserEntitlement,
  type EntitlementCatalog, type EntitlementChangePreview, type EntitlementTier, type UserEntitlement,
} from '@/api/adminEntitlements'

interface PendingChange {
  userIds: number[]
  tier: EntitlementTier
  reason: string
  requestId: string
  previewToken: string
  storageKey: string | null
}

const props = defineProps<{ show: boolean; userId?: number; userIds?: number[] }>()
const emit = defineEmits<{ close: []; changed: [] }>()
const { t } = useI18n()
const authStore = useAuthStore()
const targets = computed(() => [...new Set(props.userIds?.length ? props.userIds : props.userId ? [props.userId] : [])].sort((a, b) => a - b))
const contextKey = computed(() => `${props.show}:${targets.value.join(',')}`)
const storageKey = computed(() => authStore.user?.id ? `sub2:entitlement-pending:v1:${authStore.user.id}` : null)
const loading = ref(false), catalogLoading = ref(false), previewLoading = ref(false), applying = ref(false)
const entitlement = ref<UserEntitlement | null>(null), catalog = ref<EntitlementCatalog | null>(null)
const pendingTier = ref<EntitlementTier | null>(null), previewResult = ref<EntitlementChangePreview | null>(null)
const reason = ref(''), requestId = ref(''), loadError = ref(''), catalogError = ref(''), previewError = ref(''), writeError = ref(''), writeNotice = ref('')
const pendingSubmission = ref<PendingChange | null>(null), outcomeUnknown = ref(false), storageUnavailable = ref(false)
const tiers: EntitlementTier[] = ['standard', 'premium']
let alive = true, loadSequence = 0, previewSequence = 0
let loadController: AbortController | null = null, previewController: AbortController | null = null
let expiryTimer: ReturnType<typeof setTimeout> | undefined

const canWrite = computed(() => {
  const cap = entitlement.value?.capabilities
  return cap?.writes_enabled === true && cap?.can_write === true
})
const canPreview = computed(() => props.show && !!entitlement.value && !!catalog.value && !loading.value && !catalogLoading.value && !catalogError.value && !applying.value)
const canApply = computed(() => props.show && canWrite.value && !loading.value && !catalogLoading.value && !loadError.value && !catalogError.value && !pendingSubmission.value && !applying.value && !previewLoading.value && !!pendingTier.value && !!reason.value.trim() && !!requestId.value &&
  previewResult.value?.policy_enabled === true && !!previewResult.value.preview_token && Date.parse(previewResult.value.expires_at) > Date.now())
const premiumPolicyLabel = computed(() => {
  if (catalogLoading.value) return t('admin.settings.userEntitlement.reading')
  const policy = catalog.value?.tiers?.find(tier => tier.tier === 'premium')
  return policy ? t('admin.settings.userEntitlement.policyState', { status: policy.enabled ? t('admin.settings.userEntitlement.enabled') : t('admin.settings.userEntitlement.disabled'), version: policy.version }) : t('admin.settings.userEntitlement.unknownPolicy')
})
const readOnlyReason = computed(() => {
  if (loading.value) return t('admin.settings.userEntitlement.ro.verifying')
  const cap = entitlement.value?.capabilities
  if (!cap || typeof cap.can_write !== 'boolean' || typeof cap.writes_enabled !== 'boolean') return t('admin.settings.userEntitlement.ro.unknown')
  if (cap.can_write !== true) return t('admin.settings.userEntitlement.ro.noPermission')
  if (cap.writes_enabled !== true) return t('admin.settings.userEntitlement.ro.writesDisabled')
  return t('admin.settings.userEntitlement.ro.noPermission')
})
function statusOf(error: unknown): number | undefined { const value = error as { status?: number; response?: { status?: number } }; return value?.response?.status ?? value?.status }
function messageOf(error: unknown, fallback: string): string { const value = error as { message?: string; response?: { data?: { message?: string } } }; return value?.response?.data?.message || value?.message || fallback }
function persistPending(operation: PendingChange) {
  storageUnavailable.value = !operation.storageKey
  if (!operation.storageKey) return
  try { sessionStorage.setItem(operation.storageKey, JSON.stringify(operation)) } catch { storageUnavailable.value = true }
}
function clearStored(operation: PendingChange) { if (operation.storageKey) { try { sessionStorage.removeItem(operation.storageKey) } catch { /* Current in-memory result remains authoritative. */ } } }
function restorePending() {
  pendingSubmission.value = null
  outcomeUnknown.value = false
  if (!storageKey.value) return
  try {
    const raw = sessionStorage.getItem(storageKey.value)
    if (!raw) return
    const value = JSON.parse(raw) as Partial<PendingChange>
    if (!Array.isArray(value.userIds) || !value.userIds.length || value.userIds.length > 1000 || value.userIds.some(id => !Number.isSafeInteger(id) || id <= 0) ||
      !tiers.includes(value.tier as EntitlementTier) || typeof value.reason !== 'string' || !value.reason.trim() || typeof value.requestId !== 'string' || !value.requestId || typeof value.previewToken !== 'string' || !value.previewToken) return
    pendingSubmission.value = { userIds: [...value.userIds], tier: value.tier as EntitlementTier, reason: value.reason, requestId: value.requestId, previewToken: value.previewToken, storageKey: storageKey.value }
    outcomeUnknown.value = true
  } catch { /* Corrupt or unavailable session storage must not authorize a write. */ }
}
function invalidatePreview() {
  previewSequence++
  previewController?.abort()
  if (expiryTimer) clearTimeout(expiryTimer)
  previewResult.value = null
  pendingTier.value = null
  previewLoading.value = false
  requestId.value = ''
}
async function loadCurrent(): Promise<boolean> {
  if (!props.show || !targets.value.length) return false
  invalidatePreview()
  const sequence = ++loadSequence, key = contextKey.value, userId = targets.value[0]
  loadController?.abort()
  loadController = new AbortController()
  const signal = loadController.signal
  const current = () => alive && sequence === loadSequence && key === contextKey.value
  loading.value = true; catalogLoading.value = true; loadError.value = ''; catalogError.value = ''
  const basic = getUserEntitlement(userId, signal).then(data => { if (current()) entitlement.value = data; return true }).catch(error => {
    if (current()) loadError.value = messageOf(error, statusOf(error) === 403 ? t('admin.settings.userEntitlement.err.basicForbidden') : t('admin.settings.userEntitlement.err.basicFailed'))
    return false
  }).finally(() => { if (current()) loading.value = false })
  const policies = getEntitlementCatalog(signal).then(data => { if (current()) catalog.value = data; return true }).catch(error => {
    if (current()) { catalog.value = null; catalogError.value = messageOf(error, statusOf(error) === 403 ? t('admin.settings.userEntitlement.err.catalogForbidden') : t('admin.settings.userEntitlement.err.catalogFailed')) }
    return false
  }).finally(() => { if (current()) catalogLoading.value = false })
  const results = await Promise.all([basic, policies])
  return current() && results.every(Boolean)
}
async function preview(tier: EntitlementTier) {
  if (!canPreview.value || pendingSubmission.value) return
  invalidatePreview()
  const sequence = previewSequence, key = contextKey.value, ids = [...targets.value]
  pendingTier.value = tier; previewLoading.value = true; previewError.value = ''; writeError.value = ''; writeNotice.value = ''
  previewController = new AbortController()
  try {
    const result = await previewEntitlementChange(ids, tier, previewController.signal)
    if (!alive || sequence !== previewSequence || key !== contextKey.value) return
    if (result.tier !== tier || [...(result.user_ids ?? [])].sort((a, b) => a - b).join(',') !== ids.join(',')) throw new Error('preview context mismatch')
    previewResult.value = result
    const expires = Date.parse(result.expires_at)
    if (!result.preview_token || !Number.isFinite(expires) || expires <= Date.now()) {
      previewError.value = t('admin.settings.userEntitlement.err.previewToken')
      return
    }
    requestId.value = crypto.randomUUID()
    expiryTimer = setTimeout(() => { invalidatePreview(); previewError.value = t('admin.settings.userEntitlement.err.previewExpired') }, expires - Date.now())
  } catch (error) {
    if (alive && sequence === previewSequence && key === contextKey.value) { previewResult.value = null; previewError.value = messageOf(error, t('admin.settings.userEntitlement.err.previewFailed')) }
  } finally { if (alive && sequence === previewSequence && key === contextKey.value) previewLoading.value = false }
}
async function apply() {
  if (!canApply.value || !pendingTier.value || !previewResult.value) return
  const operation: PendingChange = { userIds: [...targets.value], tier: pendingTier.value, reason: reason.value.trim(), requestId: requestId.value, previewToken: previewResult.value.preview_token, storageKey: storageKey.value }
  pendingSubmission.value = operation
  persistPending(operation)
  await submitPending()
}
async function submitPending() {
  const operation = pendingSubmission.value
  if (!operation || applying.value) return
  const wasUnknown = outcomeUnknown.value
  applying.value = true; writeError.value = ''
  try {
    const result = operation.userIds.length === 1
      ? await updateUserEntitlement(operation.userIds[0], operation.tier, operation.reason, operation.requestId, operation.previewToken)
      : await applyEntitlementChange(operation.userIds, operation.tier, operation.reason, operation.requestId, operation.previewToken)
    if (!result || result.tier !== operation.tier || result.requested !== operation.userIds.length || !Number.isSafeInteger(result.version) || result.version < 1 || typeof result.idempotent !== 'boolean' || !Number.isInteger(result.changed) || !Number.isInteger(result.unchanged) || result.changed < 0 || result.unchanged < 0 || result.changed + result.unchanged !== result.requested) throw new Error('Incomplete write acknowledgement')
    clearStored(operation)
    if (!alive || storageKey.value !== operation.storageKey || pendingSubmission.value?.requestId !== operation.requestId) return
    pendingSubmission.value = null; outcomeUnknown.value = false
    invalidatePreview()
    writeNotice.value = t('admin.settings.userEntitlement.savedNotice', { targets: operation.userIds.join(', '), changed: result.changed, unchanged: result.unchanged, version: result.version, replay: result.idempotent ? t('admin.settings.userEntitlement.replaySuffix') : '' })
    emit('changed')
    if (props.show && !(await loadCurrent())) writeNotice.value += t('admin.settings.userEntitlement.savedRefreshFailed')
  } catch (error) {
    const status = statusOf(error)
    const unknown = wasUnknown || status == null || status === 0 || status >= 500 || status === 408
    if (!alive || storageKey.value !== operation.storageKey || pendingSubmission.value?.requestId !== operation.requestId) return
    outcomeUnknown.value = unknown
    if (unknown) {
      persistPending(operation)
      writeError.value = t('admin.settings.userEntitlement.err.unknownRetry')
    } else {
      clearStored(operation); pendingSubmission.value = null
      invalidatePreview()
      writeError.value = messageOf(error, status === 409 ? t('admin.settings.userEntitlement.err.casConflict') : status === 403 ? t('admin.settings.userEntitlement.err.writeForbidden') : t('admin.settings.userEntitlement.err.writeRejected'))
    }
  } finally { if (alive) applying.value = false }
}
function close() { if (!applying.value && !(pendingSubmission.value && storageUnavailable.value)) emit('close') }
watch(storageKey, restorePending, { immediate: true })
watch(contextKey, () => {
  loadSequence++; loadController?.abort(); invalidatePreview()
  entitlement.value = null; catalog.value = null; loading.value = false; catalogLoading.value = false
  loadError.value = ''; catalogError.value = ''; previewError.value = ''
  if (!pendingSubmission.value) { reason.value = ''; writeError.value = ''; writeNotice.value = '' }
  if (props.show) void loadCurrent()
}, { immediate: true })
onBeforeUnmount(() => { alive = false; loadSequence++; loadController?.abort(); invalidatePreview() })
</script>
