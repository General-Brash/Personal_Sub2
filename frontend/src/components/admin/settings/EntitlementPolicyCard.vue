<template>
  <section class="card space-y-4 p-5">
    <h3 class="text-lg font-semibold">{{ t('admin.settings.entitlementPolicy.title') }}</h3>
    <p class="text-sm text-gray-500">{{ t('admin.settings.entitlementPolicy.description') }}</p>
    <p role="status" class="text-sm" :class="canWrite ? 'text-emerald-700' : 'text-amber-700'">{{ capabilityMessage }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="notice" data-testid="policy-status" role="status" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-200">{{ notice }}</p>
    <div v-if="outcomeUnknown && pending" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">
      <p>{{ t('admin.settings.entitlementPolicy.pendingUnknown', { tier: pending.input.tier }) }}</p>
      <p class="mt-1 break-all text-xs">{{ t('admin.settings.entitlementPolicy.requestId', { id: pending.input.request_id }) }}</p>
      <button data-testid="policy-retry" class="btn btn-secondary mt-2" :disabled="saving" @click="submitPending">{{ t('admin.settings.entitlementPolicy.retryOriginal') }}</button>
    </div>
    <button data-testid="policy-load" class="btn btn-secondary" type="button" :disabled="loading || saving" @click="load">{{ loading ? t('admin.settings.entitlementPolicy.loading') : catalog ? t('admin.settings.entitlementPolicy.reload') : t('admin.settings.entitlementPolicy.load') }}</button>
    <template v-if="catalog">
      <p v-if="!catalog.tiers?.length" class="text-sm text-gray-500">{{ t('admin.settings.entitlementPolicy.emptyTiers') }}</p>
      <div class="flex gap-2"><button v-for="item in catalog.tiers ?? []" :key="item.tier" type="button" class="btn btn-secondary" :disabled="loading || saving || !!pending" @click="choose(item)">{{ item.display_name }}</button></div>
      <div v-if="form" class="space-y-3">
        <p class="text-sm font-medium">{{ t('admin.settings.entitlementPolicy.tierSummary', { tier: form.tier, status: form.enabled ? t('admin.settings.entitlementPolicy.enabled') : t('admin.settings.entitlementPolicy.disabled'), version: form.version, groups: form.groups?.map(group => group.group_id).join(', ') || t('admin.settings.entitlementPolicy.noGroups') }) }}</p>
        <label class="block"><span class="input-label">{{ t('admin.settings.entitlementPolicy.displayName') }}</span><input v-model="form.display_name" :disabled="!canEdit" class="input" /></label>
        <div v-for="(group, index) in form.groups" :key="index" class="grid grid-cols-[1fr_1fr_auto] gap-2">
          <label><span class="input-label">{{ t('admin.settings.entitlementPolicy.groupIdLabel') }}</span><input v-model.number="group.group_id" type="number" min="1" :disabled="!canEdit" class="input" /></label>
          <label><span class="input-label">{{ t('admin.settings.entitlementPolicy.rateLabel') }}</span><input v-model.number="group.rate_multiplier" type="number" min="0" max="1000" step="0.01" :disabled="!canEdit" class="input" /><span class="mt-1 block text-xs text-gray-500">{{ groupSourceHint(group) }}</span></label>
          <button type="button" class="btn btn-secondary self-end" :disabled="!canEdit" @click="form.groups.splice(index, 1)">{{ t('admin.settings.entitlementPolicy.remove') }}</button>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="!canEdit" @click="form.groups.push({ group_id: 0, source: 'tier' })">{{ t('admin.settings.entitlementPolicy.addGroup') }}</button>
        <label class="block"><span class="input-label">{{ t('admin.settings.entitlementPolicy.reasonLabel') }}</span><input v-model.trim="reason" data-testid="policy-reason" :disabled="!canEdit" class="input" /></label>
        <p class="text-xs text-gray-500">{{ t('admin.settings.entitlementPolicy.contentNote') }}</p>
        <p v-if="canEdit && !reason" class="text-sm text-amber-700">{{ t('admin.settings.entitlementPolicy.reasonRequired') }}</p>
        <p v-if="groupError" role="alert" class="text-sm text-red-600">{{ groupError }}</p>
        <div class="flex flex-wrap gap-2">
          <button data-testid="policy-save" type="button" class="btn btn-primary" :disabled="!canSubmit" @click="saveContent">{{ saving ? t('admin.settings.entitlementPolicy.saving') : t('admin.settings.entitlementPolicy.saveContent') }}</button>
          <button v-if="form.tier === 'premium'" data-testid="policy-toggle" type="button" class="btn btn-secondary" :disabled="!canSubmit || hasEdits" @click="toggleEnabled">{{ form.enabled ? t('admin.settings.entitlementPolicy.disablePremium') : t('admin.settings.entitlementPolicy.enablePremium') }}</button>
        </div>
        <p v-if="hasEdits" class="text-xs text-gray-500">{{ t('admin.settings.entitlementPolicy.editsPendingNote') }}</p>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { getEntitlementCatalog, updateEntitlementPolicy, type EntitlementCatalog, type EntitlementTierPolicy, type EntitlementTierGroupPolicy, type EntitlementPolicyUpdate } from '@/api/adminEntitlements'

interface PendingPolicy { input: EntitlementPolicyUpdate; storageKey: string | null }
const { t } = useI18n()
const authStore = useAuthStore()
const catalog = ref<EntitlementCatalog | null>(null), form = ref<EntitlementTierPolicy | null>(null)
const error = ref(''), notice = ref(''), reason = ref(''), loading = ref(false), saving = ref(false), requiresReload = ref(false)
const pending = ref<PendingPolicy | null>(null), outcomeUnknown = ref(false)
const storageKey = computed(() => authStore.user?.id ? `sub2:entitlement-policy-pending:v1:${authStore.user.id}` : null)
let alive = true, loadSequence = 0
let controller: AbortController | null = null
const canWrite = computed(() => catalog.value?.capabilities?.writes_enabled === true && catalog.value?.capabilities?.can_write === true)
const canEdit = computed(() => canWrite.value && !loading.value && !saving.value && !pending.value && !requiresReload.value)
const selectedPolicy = computed(() => catalog.value?.tiers?.find(item => item.tier === form.value?.tier))
const hasEdits = computed(() => !!form.value && !!selectedPolicy.value && JSON.stringify(form.value) !== JSON.stringify(selectedPolicy.value))
const capabilityMessage = computed(() => {
  if (loading.value) return t('admin.settings.entitlementPolicy.cap.loading')
  const cap = catalog.value?.capabilities
  if (!cap || typeof cap.can_write !== 'boolean' || typeof cap.writes_enabled !== 'boolean') return t('admin.settings.entitlementPolicy.cap.unknown')
  if (cap.can_write !== true) return t('admin.settings.entitlementPolicy.cap.noPermission')
  if (cap.writes_enabled !== true) return t('admin.settings.entitlementPolicy.cap.writesDisabled')
  if (requiresReload.value) return t('admin.settings.entitlementPolicy.cap.requiresReload')
  return t('admin.settings.entitlementPolicy.cap.verified')
})
const groupError = computed(() => {
  const groups = form.value?.groups ?? []
  if (groups.some(group => !Number.isSafeInteger(group.group_id) || group.group_id <= 0)) return t('admin.settings.entitlementPolicy.err.groupIdPositive')
  if (new Set(groups.map(group => group.group_id)).size !== groups.length) return t('admin.settings.entitlementPolicy.err.groupIdDuplicate')
  if (groups.some(group => typeof group.rate_multiplier === 'number' && (!Number.isFinite(group.rate_multiplier) || group.rate_multiplier < 0 || group.rate_multiplier > 1000))) return t('admin.settings.entitlementPolicy.err.rateRange')
  return ''
})
function groupSourceHint(group: EntitlementTierGroupPolicy): string {
  return typeof group.rate_multiplier === 'number'
    ? t('admin.settings.entitlementPolicy.sourceTierCustom', { rate: group.rate_multiplier })
    : t('admin.settings.entitlementPolicy.sourceGroupDefault')
}
const canSubmit = computed(() => canEdit.value && !!form.value && !!reason.value.trim() && !groupError.value)
function choose(item: EntitlementTierPolicy) {
  if (saving.value || pending.value) return
  form.value = { ...item, groups: (item.groups ?? []).map(group => ({ ...group })) }
  reason.value = ''; error.value = ''; notice.value = ''
}
function statusOf(value: unknown): number | undefined { const error = value as { status?: number; response?: { status?: number } }; return error?.response?.status ?? error?.status }
function messageOf(value: unknown, fallback: string): string { const error = value as { message?: string; response?: { data?: { message?: string } } }; return error?.response?.data?.message || error?.message || fallback }
function store(operation: PendingPolicy) { if (operation.storageKey) { try { sessionStorage.setItem(operation.storageKey, JSON.stringify(operation.input)) } catch { /* Keep the frozen in-memory request. */ } } }
function clearStore(operation: PendingPolicy) { if (operation.storageKey) { try { sessionStorage.removeItem(operation.storageKey) } catch { /* A confirmed result is not a failed write. */ } } }
function restore() {
  pending.value = null; outcomeUnknown.value = false
  if (!storageKey.value) return
  try {
    const raw = sessionStorage.getItem(storageKey.value)
    if (!raw) return
    const input = JSON.parse(raw) as EntitlementPolicyUpdate
    if (!['standard', 'premium'].includes(input.tier) || typeof input.request_id !== 'string' || !input.request_id || typeof input.reason !== 'string' || !input.reason.trim() || !Number.isSafeInteger(input.expected_version) || input.expected_version < 1 || !Array.isArray(input.groups) || typeof input.enabled !== 'boolean') return
    pending.value = { input, storageKey: storageKey.value }; outcomeUnknown.value = true
  } catch { /* Invalid local data cannot enable new writes. */ }
}
async function load(): Promise<boolean> {
  const sequence = ++loadSequence, tier = form.value?.tier ?? 'premium'
  controller?.abort(); controller = new AbortController()
  loading.value = true; error.value = ''
  try {
    const data = await getEntitlementCatalog(controller.signal)
    if (!alive || sequence !== loadSequence) return false
    catalog.value = data
    const item = data.tiers?.find(policy => policy.tier === tier) ?? data.tiers?.[0]
    if (item && !pending.value) form.value = { ...item, groups: (item.groups ?? []).map(group => ({ ...group })) }
    else if (!item) form.value = null
    requiresReload.value = false
    return true
  } catch (value) {
    if (alive && sequence === loadSequence) error.value = messageOf(value, statusOf(value) === 403 ? t('admin.settings.entitlementPolicy.err.readForbidden') : t('admin.settings.entitlementPolicy.err.readFailed'))
    return false
  } finally { if (alive && sequence === loadSequence) loading.value = false }
}
function startWrite(enabled: boolean, content: EntitlementTierPolicy) {
  const input: EntitlementPolicyUpdate = {
    tier: content.tier, display_name: content.display_name.trim(), enabled, expected_version: content.version,
    groups: content.groups.map(group => ({ ...group, rate_multiplier: typeof group.rate_multiplier === 'number' ? group.rate_multiplier : null })),
    reason: reason.value.trim(), request_id: crypto.randomUUID(),
  }
  pending.value = { input, storageKey: storageKey.value }; store(pending.value)
  void submitPending()
}
function saveContent() { if (canSubmit.value && form.value && selectedPolicy.value) startWrite(selectedPolicy.value.enabled, form.value) }
function toggleEnabled() { if (canSubmit.value && form.value?.tier === 'premium' && selectedPolicy.value && !hasEdits.value) startWrite(!selectedPolicy.value.enabled, selectedPolicy.value) }
async function submitPending() {
  const operation = pending.value
  if (!operation || saving.value) return
  const wasUnknown = outcomeUnknown.value
  saving.value = true; error.value = ''
  try {
    const result = await updateEntitlementPolicy(operation.input)
    if (!result || result.tier !== operation.input.tier || result.enabled !== operation.input.enabled || !Number.isSafeInteger(result.version) || result.version <= operation.input.expected_version) throw new Error('Incomplete policy write acknowledgement')
    clearStore(operation)
    if (!alive || storageKey.value !== operation.storageKey || pending.value?.input.request_id !== operation.input.request_id) return
    pending.value = null; outcomeUnknown.value = false; reason.value = ''
    form.value = { ...result, groups: (result.groups ?? []).map(group => ({ ...group })) }
    if (catalog.value) catalog.value = { ...catalog.value, tiers: catalog.value.tiers.map(item => item.tier === result.tier ? result : item) }
    notice.value = t('admin.settings.entitlementPolicy.savedNotice', { tier: result.tier, status: result.enabled ? t('admin.settings.entitlementPolicy.enabled') : t('admin.settings.entitlementPolicy.disabled'), version: result.version, groups: (result.groups ?? []).map(group => group.group_id).join(', ') || t('admin.settings.entitlementPolicy.noGroups') })
    if (!(await load())) notice.value += t('admin.settings.entitlementPolicy.savedRefreshFailed')
  } catch (value) {
    if (!alive || storageKey.value !== operation.storageKey || pending.value?.input.request_id !== operation.input.request_id) return
    const status = statusOf(value)
    outcomeUnknown.value = wasUnknown || status == null || status === 0 || status >= 500 || status === 408
    if (outcomeUnknown.value) { store(operation); error.value = t('admin.settings.entitlementPolicy.err.unknownRetry') }
    else {
      clearStore(operation); pending.value = null; requiresReload.value = status === 409
      error.value = messageOf(value, status === 409 ? t('admin.settings.entitlementPolicy.err.casConflict') : status === 403 ? t('admin.settings.entitlementPolicy.err.writeForbidden') : t('admin.settings.entitlementPolicy.err.writeRejected'))
    }
  } finally { if (alive) saving.value = false }
}
watch(storageKey, restore, { immediate: true })
onMounted(() => { void load() })
onBeforeUnmount(() => { alive = false; loadSequence++; controller?.abort() })
</script>
