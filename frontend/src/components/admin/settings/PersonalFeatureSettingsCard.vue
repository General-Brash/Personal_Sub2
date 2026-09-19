<template>
  <section class="card space-y-4 p-5" aria-labelledby="personal-features-title">
    <div><h3 id="personal-features-title" class="text-lg font-semibold">新增功能与额度期限</h3><p class="mt-1 text-sm text-gray-500">设置仅影响新请求和新批次，不追改历史额度；不会自动开启其他玩法。</p></div>
    <p role="status" class="text-sm" :class="accessState === 'ready' ? 'text-emerald-700' : 'text-amber-700'">{{ accessMessage }}</p>
    <div v-if="loading" role="status">加载中…</div>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <p v-if="notice" data-testid="personal-settings-status" role="status" class="text-sm text-emerald-700">{{ notice }}</p>
    <button type="button" class="btn btn-secondary" :disabled="loading || saving" @click="load">重新读取设置与权限</button>
    <template v-if="policy">
      <fieldset :disabled="!canEdit" class="grid gap-4 md:grid-cols-2">
        <label class="flex items-center gap-2"><input v-model="policy.model_plaza_v2_enabled" type="checkbox" />独立模型广场 V2</label>
        <label class="flex items-center gap-2"><input v-model="policy.player_invitations_enabled" type="checkbox" />玩家一次性邀请</label>
        <label><span class="input-label">邀请码有效期（秒，启用前必须明确配置）</span><input v-model.number="policy.invitation_ttl_seconds" class="input" type="number" min="0" step="1" /></label>
        <div class="text-sm text-gray-500">初始机会为每用户一次；确认预留、成功注册才消耗，取消或过期释放。不会补发历史返利。</div>
        <label><span class="input-label">签到临时额度截止（北京时间）</span><input v-model="policy.source_expiry.checkin" class="input" type="time" /></label>
        <label><span class="input-label">管理员发放临时额度截止（北京时间）</span><input v-model="policy.source_expiry.admin_grant" class="input" type="time" /></label>
      </fieldset>
      <p class="text-sm text-gray-500">到期时间与签到刷新时间分离。银行兑换期限在银行设置中配置；商城、订阅、预支债务不继承此设置。每笔新额度将固定下一个截止点。</p>
      <div class="flex flex-wrap items-center justify-between gap-3"><span class="break-all text-xs text-gray-500">版本 {{ policy.version }}</span><button data-testid="personal-settings-save" type="button" class="btn btn-primary" :disabled="!canEdit" @click="save">{{ saving ? '保存中…' : '确认对新批次生效' }}</button></div>
    </template>
  </section>
</template>
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { apiClient } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
interface Policy { model_plaza_v2_enabled: boolean; player_invitations_enabled: boolean; invitation_ttl_seconds: number; source_expiry: Record<string, string>; version: string }
const auth = useAuthStore(), app = useAppStore()
const policy = ref<Policy | null>(null), loading = ref(false), saving = ref(false), error = ref(''), notice = ref(''), requiresReload = ref(false)
const permissionState = ref<'loading' | 'ready' | 'unknown'>('loading')
let alive = true, sequence = 0
type AccessState = 'loading' | 'unknown' | 'forbidden' | 'ready'
const accessState = computed<AccessState>(() => {
  if (loading.value || permissionState.value === 'loading') return 'loading'
  if (permissionState.value !== 'ready' || !auth.user) return 'unknown'
  return auth.canAdmin('system.settings.manage') ? 'ready' : 'forbidden'
})
const canEdit = computed(() => accessState.value === 'ready' && !saving.value && !requiresReload.value && !!policy.value)
const accessMessage = computed(() => ({ loading: '正在核验 system.settings.manage 写入能力。', unknown: '写入能力未知，请重新读取。', forbidden: '当前没有 system.settings.manage 权限，不能保存功能设置。', ready: requiresReload.value ? '请先重新读取当前版本，再确认是否需要写入。' : '已核验 system.settings.manage 写入权限。' })[accessState.value])
function statusOf(value: unknown): number | undefined { const error = value as { status?: number; response?: { status?: number } }; return error?.response?.status ?? error?.status }
async function load(): Promise<boolean> {
  const current = ++sequence, wasUncertain = requiresReload.value
  loading.value = true; permissionState.value = 'loading'; error.value = ''
  const settings = apiClient.get<Policy>('/admin/settings/personal-features').then(({ data }) => {
    if (!data?.source_expiry || typeof data.version !== 'string') throw new Error('Incomplete policy')
    if (alive && current === sequence) { policy.value = data; requiresReload.value = false; if (wasUncertain) notice.value = '已读取服务器当前状态，请核对原请求是否生效。' }
    return true
  }).catch(value => { if (alive && current === sequence) { requiresReload.value = true; error.value = statusOf(value) === 403 ? '没有 system.settings.manage 读取权限。' : '读取功能设置失败，请检查权限与服务状态。' } return false })
  const permissions = auth.refreshUser().then(user => { if (alive && current === sequence) permissionState.value = user ? 'ready' : 'unknown'; return !!user }).catch(() => { if (alive && current === sequence) permissionState.value = 'unknown'; return false })
  const result = await Promise.all([settings, permissions])
  if (alive && current === sequence) loading.value = false
  return alive && current === sequence && result.every(Boolean)
}
async function save() {
  if (!policy.value || !canEdit.value) return
  const payload = { ...policy.value, source_expiry: { ...policy.value.source_expiry } }
  saving.value = true; error.value = ''; notice.value = ''
  try {
    const { data } = await apiClient.put<Policy>('/admin/settings/personal-features', payload)
    policy.value = data
  } catch (value) {
    requiresReload.value = true
    error.value = statusOf(value) == null || statusOf(value) === 0 || statusOf(value) === 408 || (statusOf(value) ?? 0) >= 500 ? '保存结果未知；请先重新读取当前版本确认，不会自动重试或覆盖。' : '保存被拒绝或版本已变化；请重新读取并确认，不会自动覆盖。'
    saving.value = false
    return
  }
  notice.value = '设置已保存；存量批次保持原合同。'
  app.showSuccess(notice.value)
  if (!(await load())) notice.value += ' 保存成功，但刷新失败；请重新读取，不要重复保存。'
  saving.value = false
}
onMounted(load)
onBeforeUnmount(() => { alive = false; sequence++ })
</script>
