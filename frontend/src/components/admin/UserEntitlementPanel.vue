<template>
  <BaseDialog :show="show" title="消费权益" width="wide" @close="close">
    <div class="space-y-4">
      <div class="flex flex-wrap items-center justify-between gap-2 text-sm text-gray-500 dark:text-dark-400">
        <span>目标 {{ targets.length }} 人；下方显示首位用户当前权益。管理角色、消费等级和业务分组互相独立。</span>
        <span :class="canWrite ? 'text-emerald-600' : 'text-amber-600'" class="font-medium">{{ loading ? '核验中' : canWrite ? '可写' : '只读' }}</span>
      </div>
      <p v-if="!canWrite" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">{{ readOnlyReason }}</p>
      <p v-if="loadError" role="alert" class="text-sm text-red-600">{{ loadError }}</p>
      <p v-if="catalogError" role="status" class="text-sm text-amber-700">{{ catalogError }}；已成功读取的基础权益仍可查看。</p>
      <p v-if="writeNotice" data-testid="entitlement-status" role="status" class="rounded-lg bg-emerald-50 p-3 text-sm text-emerald-800 dark:bg-emerald-900/20 dark:text-emerald-200">{{ writeNotice }}</p>
      <div v-if="outcomeUnknown && pendingSubmission" role="alert" class="rounded-lg bg-amber-50 p-3 text-sm text-amber-900 dark:bg-amber-900/20 dark:text-amber-200">
        <p>原请求结果未知，不能另换请求重新赋级。目标：{{ pendingSubmission.userIds.join(', ') }}；等级：{{ pendingSubmission.tier }}。</p>
        <p class="mt-1 break-all text-xs">请求编号：{{ pendingSubmission.requestId }}</p>
        <p v-if="storageUnavailable">无法保存核对信息，请保留本窗口直到核对完成。</p>
        <button data-testid="entitlement-retry" class="btn btn-secondary mt-2" :disabled="applying" @click="submitPending">{{ applying ? '核对中…' : '使用原请求核对／重试' }}</button>
      </div>
      <p v-if="writeError" role="alert" class="text-sm text-red-600">{{ writeError }}</p>
      <button class="btn btn-secondary" :disabled="loading || applying" @click="loadCurrent">重新读取权益与政策</button>
      <div v-if="loading && !entitlement" class="py-8 text-center text-sm text-gray-500">加载基础权益中…</div>
      <template v-if="entitlement">
        <div class="grid gap-3 text-sm sm:grid-cols-3">
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">当前有效消费等级</span><div class="font-semibold">{{ entitlement.tier }}</div></div>
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">权益版本</span><div class="font-semibold">{{ entitlement.version }}</div></div>
          <div class="rounded-lg bg-gray-50 p-3 dark:bg-dark-700"><span class="text-gray-500">Premium 政策</span><div class="font-semibold">{{ premiumPolicyLabel }}</div></div>
        </div>
        <div v-if="catalog" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
          <div class="flex flex-wrap gap-2">
            <button v-for="tier in tiers" :key="tier" :data-testid="`entitlement-preview-${tier}`" :disabled="!canPreview || !!pendingSubmission" class="btn" :class="pendingTier === tier ? 'btn-primary' : 'btn-secondary'" @click="preview(tier)">预览 {{ tier }}</button>
          </div>
          <p class="mt-2 text-xs text-gray-500">保存或启用政策不会自动迁移用户。赋级保留手工倍率、手工分组、独立权益 grant 与有效订阅；独立 grant 可能继续维持 premium。</p>
          <label class="mt-3 block text-sm"><span class="mb-1 block text-gray-600 dark:text-dark-300">原始原因（审计必填）</span><input v-model.trim="reason" :disabled="!!pendingSubmission" data-testid="entitlement-reason" class="input w-full" placeholder="例如：活动授予 / 用户申诉调整" /></label>
          <p v-if="previewLoading" role="status" class="mt-3 text-sm">预览中，暂不可应用…</p>
          <p v-if="previewError" role="alert" class="mt-3 text-sm text-red-600">{{ previewError }}</p>
          <div v-if="previewResult" data-testid="entitlement-preview" class="mt-3 rounded-lg bg-blue-50 p-3 text-sm dark:bg-blue-900/20">
            <div>将更新基础赋级 {{ previewResult.affected_user_ids?.length ?? 0 }} 人，无需更改基础赋级 {{ previewResult.already_at_tier?.length ?? 0 }} 人。</div>
            <div class="mt-1 text-xs">新增可用组 {{ previewResult.granted_group_ids?.join(', ') || '无' }}；撤销可用组 {{ previewResult.revoked_group_ids?.join(', ') || '无' }}。手工分组和有效订阅保留。</div>
            <div class="mt-1 text-xs">政策版本 {{ previewResult.policy_version }}；预览有效至 {{ previewResult.expires_at }}。</div>
            <p v-if="previewResult.policy_enabled !== true" class="mt-2 text-amber-700">该等级政策未启用：本次预览只读，不可应用。</p>
          </div>
          <button data-testid="entitlement-apply" class="btn btn-primary mt-3" :disabled="!canApply" @click="apply">{{ applying ? '应用中…' : '确认应用' }}</button>
        </div>
        <div class="text-sm">
          <div class="font-medium text-gray-900 dark:text-white">来源解释</div>
          <ul class="mt-2 space-y-1 text-gray-600 dark:text-dark-300">
            <li v-for="source in entitlement.sources ?? []" :key="`${source.source}-${source.tier}-${source.group_id ?? 'tier'}`">{{ source.explain }} · {{ source.tier }}<span v-if="source.group_id"> · group {{ source.group_id }}</span><span v-if="source.rate != null"> · {{ source.rate }}x</span></li>
            <li v-if="!entitlement.sources?.length">默认 standard</li>
          </ul>
        </div>
        <div class="grid gap-3 text-sm sm:grid-cols-2">
          <div><span class="text-gray-500">手工分组：</span>{{ entitlement.manual_groups?.join(', ') || '无' }}</div>
          <div><span class="text-gray-500">有效订阅组：</span>{{ entitlement.subscription_groups?.join(', ') || '无' }}</div>
        </div>
      </template>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
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
  if (catalogLoading.value) return '读取中'
  const policy = catalog.value?.tiers?.find(tier => tier.tier === 'premium')
  return policy ? `${policy.enabled ? '已启用' : '未启用'} · v${policy.version}` : '未知（未读取政策）'
})
const readOnlyReason = computed(() => {
  if (loading.value) return '正在核验当前权益与写入能力。'
  const cap = entitlement.value?.capabilities
  if (!cap || typeof cap.can_write !== 'boolean' || typeof cap.writes_enabled !== 'boolean') return '写入能力未知；不会将缺失能力信息视为已获得权限。'
  if (cap.can_write !== true) return '当前管理员没有 users.entitlement.manage 写入能力。'
  if (cap.writes_enabled !== true) return '后端未开启该类写入。'
  return '当前管理员没有 users.entitlement.manage 写入能力。'
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
    if (current()) loadError.value = messageOf(error, statusOf(error) === 403 ? '没有 users.read 权限，无法读取基础权益。' : '基础权益读取失败，请重试读取。')
    return false
  }).finally(() => { if (current()) loading.value = false })
  const policies = getEntitlementCatalog(signal).then(data => { if (current()) catalog.value = data; return true }).catch(error => {
    if (current()) { catalog.value = null; catalogError.value = messageOf(error, statusOf(error) === 403 ? '没有等级政策目录读取权限' : '等级政策目录读取失败') }
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
      previewError.value = '未收到有效的服务端预览凭证，请重新预览；不会绕过版本守卫写入。'
      return
    }
    requestId.value = crypto.randomUUID()
    expiryTimer = setTimeout(() => { invalidatePreview(); previewError.value = '预览已过期，请重新预览。' }, expires - Date.now())
  } catch (error) {
    if (alive && sequence === previewSequence && key === contextKey.value) { previewResult.value = null; previewError.value = messageOf(error, '预览失败，请重新预览。') }
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
    writeNotice.value = `目标 ${operation.userIds.join(', ')} 赋级已保存：变更 ${result.changed} 人，无需更改 ${result.unchanged} 人；版本 ${result.version}${result.idempotent ? '（原请求结果重放）' : ''}。`
    emit('changed')
    if (props.show && !(await loadCurrent())) writeNotice.value += ' 保存成功，但刷新失败；请重新读取，不要重复写入。'
  } catch (error) {
    const status = statusOf(error)
    const unknown = wasUnknown || status == null || status === 0 || status >= 500 || status === 408
    if (!alive || storageKey.value !== operation.storageKey || pendingSubmission.value?.requestId !== operation.requestId) return
    outcomeUnknown.value = unknown
    if (unknown) {
      persistPending(operation)
      writeError.value = '尚未取得原请求的确定结果，请使用同一请求编号核对／重试。'
    } else {
      clearStored(operation); pendingSubmission.value = null
      invalidatePreview()
      writeError.value = messageOf(error, status === 409 ? '预览或状态已变化，本次未写入；请重新读取并预览。' : status === 403 ? '当前无写入权限，本次未写入。' : '请求被拒绝，本次未写入；请核对后重新预览。')
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
