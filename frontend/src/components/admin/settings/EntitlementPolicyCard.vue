<template>
  <section class="card space-y-4 p-5">
    <h3 class="text-lg font-semibold">普通／优质用户等级政策</h3>
    <p class="text-sm text-gray-500">管理角色、消费等级、业务分组互相独立。保存政策、启停政策和对用户赋级是三个独立操作；这里不会自动迁移用户或按组名赋级。</p>
    <p role="status" class="text-sm" :class="canWrite ? 'text-emerald-700' : 'text-amber-700'">{{ capabilityMessage }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
    <p v-if="notice" data-testid="policy-status" role="status" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-200">{{ notice }}</p>
    <div v-if="outcomeUnknown && pending" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">
      <p>{{ pending.input.tier }} 的原请求结果未知；请使用原编号核对／重试，不要另换编号写入。</p>
      <p class="mt-1 break-all text-xs">请求编号：{{ pending.input.request_id }}</p>
      <button data-testid="policy-retry" class="btn btn-secondary mt-2" :disabled="saving" @click="submitPending">使用原请求核对／重试</button>
    </div>
    <button data-testid="policy-load" class="btn btn-secondary" type="button" :disabled="loading || saving" @click="load">{{ loading ? '读取中…' : catalog ? '重新读取等级政策' : '加载等级政策' }}</button>
    <template v-if="catalog">
      <p v-if="!catalog.tiers?.length" class="text-sm text-gray-500">没有可读取的等级政策；不会自动创建政策或重跑 seed。</p>
      <div class="flex gap-2"><button v-for="item in catalog.tiers ?? []" :key="item.tier" type="button" class="btn btn-secondary" :disabled="loading || saving || !!pending" @click="choose(item)">{{ item.display_name }}</button></div>
      <div v-if="form" class="space-y-3">
        <p class="text-sm font-medium">{{ form.tier }}：{{ form.enabled ? '已启用' : '未启用' }} · 版本 {{ form.version }} · 关联组 ID：{{ form.groups?.map(group => group.group_id).join(', ') || '无' }}</p>
        <label class="block"><span class="input-label">等级名称</span><input v-model="form.display_name" :disabled="!canEdit" class="input" /></label>
        <div v-for="(group, index) in form.groups" :key="index" class="grid grid-cols-[1fr_1fr_auto] gap-2">
          <label><span class="input-label">稳定分组 ID</span><input v-model.number="group.group_id" type="number" min="1" :disabled="!canEdit" class="input" /></label>
          <label><span class="input-label">默认倍率（留空继承）</span><input v-model.number="group.rate_multiplier" type="number" min="0" max="1000" step="0.01" :disabled="!canEdit" class="input" /></label>
          <button type="button" class="btn btn-secondary self-end" :disabled="!canEdit" @click="form.groups.splice(index, 1)">移除</button>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="!canEdit" @click="form.groups.push({ group_id: 0, source: 'tier' })">增加等级分组</button>
        <label class="block"><span class="input-label">变更原因（审计必填）</span><input v-model.trim="reason" data-testid="policy-reason" :disabled="!canEdit" class="input" /></label>
        <p class="text-xs text-gray-500">个人手工倍率优先于等级默认；手工分组和有效订阅保留。保存内容不会改变当前启用状态，也不会给用户自动赋级。</p>
        <p v-if="canEdit && !reason" class="text-sm text-amber-700">填写审计原因后方可保存或启停。</p>
        <p v-if="groupError" role="alert" class="text-sm text-red-600">{{ groupError }}</p>
        <div class="flex flex-wrap gap-2">
          <button data-testid="policy-save" type="button" class="btn btn-primary" :disabled="!canSubmit" @click="saveContent">{{ saving ? '保存中…' : '保存政策内容' }}</button>
          <button v-if="form.tier === 'premium'" data-testid="policy-toggle" type="button" class="btn btn-secondary" :disabled="!canSubmit || hasEdits" @click="toggleEnabled">{{ form.enabled ? '单独停用 premium' : '单独启用 premium' }}</button>
        </div>
        <p v-if="hasEdits" class="text-xs text-gray-500">请先保存政策内容，再单独启用或停用。启用后仍需到用户列表预览并应用赋级。</p>
      </div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { getEntitlementCatalog, updateEntitlementPolicy, type EntitlementCatalog, type EntitlementTierPolicy, type EntitlementPolicyUpdate } from '@/api/adminEntitlements'

interface PendingPolicy { input: EntitlementPolicyUpdate; storageKey: string | null }
const authStore = useAuthStore()
const catalog = ref<EntitlementCatalog | null>(null), form = ref<EntitlementTierPolicy | null>(null)
const error = ref(''), notice = ref(''), reason = ref(''), loading = ref(false), saving = ref(false), requiresReload = ref(false)
const pending = ref<PendingPolicy | null>(null), outcomeUnknown = ref(false)
const storageKey = computed(() => authStore.user?.id ? `sub2:entitlement-policy-pending:v1:${authStore.user.id}` : null)
let alive = true, loadSequence = 0
let controller: AbortController | null = null
const canWrite = computed(() => catalog.value?.capabilities?.mode === 'enforce' && catalog.value.capabilities.writes_enabled === true && catalog.value.capabilities.can_write === true)
const canEdit = computed(() => canWrite.value && !loading.value && !saving.value && !pending.value && !requiresReload.value)
const selectedPolicy = computed(() => catalog.value?.tiers?.find(item => item.tier === form.value?.tier))
const hasEdits = computed(() => !!form.value && !!selectedPolicy.value && JSON.stringify(form.value) !== JSON.stringify(selectedPolicy.value))
const capabilityMessage = computed(() => {
  if (loading.value) return '正在读取政策并核验写入能力。'
  const cap = catalog.value?.capabilities
  if (!cap || !['enforce', 'shadow', 'disabled'].includes(cap.mode) || typeof cap.can_write !== 'boolean' || typeof cap.writes_enabled !== 'boolean') return '写入能力未知，请先读取政策；不会把能力缺失当作 enforce 已关闭。'
  if (cap.mode !== 'enforce') return 'enforce 未开启：政策只读，不能保存或启停。'
  if (!canWrite.value) return '当前没有 users.entitlement.manage 写入权限。'
  if (requiresReload.value) return '状态已变化，需重新读取后确认；不会自动覆盖。'
  return '已核验 users.entitlement.manage 写入能力。'
})
const groupError = computed(() => {
  const groups = form.value?.groups ?? []
  if (groups.some(group => !Number.isSafeInteger(group.group_id) || group.group_id <= 0)) return '分组 ID 必须为正整数。'
  if (new Set(groups.map(group => group.group_id)).size !== groups.length) return '同一稳定分组 ID 不能重复。'
  if (groups.some(group => typeof group.rate_multiplier === 'number' && (!Number.isFinite(group.rate_multiplier) || group.rate_multiplier < 0 || group.rate_multiplier > 1000))) return '默认倍率必须在 0 到 1000 之间。'
  return ''
})
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
    if (alive && sequence === loadSequence) error.value = messageOf(value, statusOf(value) === 403 ? '没有 users.entitlement.manage 目录读取权限。' : '读取等级政策失败，请重试读取。')
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
    notice.value = `${result.tier} 政策已保存：${result.enabled ? '已启用' : '未启用'}，版本 ${result.version}，关联组 ID ${(result.groups ?? []).map(group => group.group_id).join(', ') || '无'}。未自动迁移用户；下一步可到用户列表预览并应用赋级。`
    if (!(await load())) notice.value += ' 保存成功，但刷新失败；请重新读取，不要重复保存。'
  } catch (value) {
    if (!alive || storageKey.value !== operation.storageKey || pending.value?.input.request_id !== operation.input.request_id) return
    const status = statusOf(value)
    outcomeUnknown.value = wasUnknown || status == null || status === 0 || status >= 500 || status === 408
    if (outcomeUnknown.value) { store(operation); error.value = '原请求结果未知，请使用同一请求编号核对／重试。' }
    else {
      clearStore(operation); pending.value = null; requiresReload.value = status === 409
      error.value = messageOf(value, status === 409 ? '政策版本或状态已变化，本次未写入；请重新读取后确认，未自动覆盖。' : status === 403 ? '当前没有写入权限，本次未写入。' : '请求被拒绝，请核对政策内容和审计原因。')
    }
  } finally { if (alive) saving.value = false }
}
watch(storageKey, restore, { immediate: true })
onBeforeUnmount(() => { alive = false; loadSequence++; controller?.abort() })
</script>
