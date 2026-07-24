<script setup lang="ts">
import { computed, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { opsAPI, type DataCleanupAudit, type DataCleanupConfig, type DataCleanupExecuteRequest, type DataCleanupFilter, type DataCleanupPreview } from '@/api/admin/ops'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import { extractApiErrorMessage } from '@/utils/apiError'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ close: []; back: []; saved: [] }>()
const { t } = useI18n()
const appStore = useAppStore()

const loading = ref(false)
const saving = ref(false)
const executing = ref(false)
const auditLoading = ref(false)
const config = ref<DataCleanupConfig | null>(null)
const audits = ref<DataCleanupAudit[]>([])
const category = ref('ops_error_logs')
const mode = ref<'range' | 'all'>('range')
const startDate = ref('')
const endDate = ref('')
const preview = ref<DataCleanupPreview | null>(null)
const confirmation = ref('')
const totpCode = ref('')
const previewing = ref(false)
const previewError = ref('')
const configError = ref('')
const auditError = ref('')
const executeError = ref('')
const previewSeq = ref(0)
const previewController = ref<AbortController | null>(null)
const loadController = ref<AbortController | null>(null)
const auditController = ref<AbortController | null>(null)
let auditPollTimer: number | null = null
const previewFilterSnapshot = ref('')

const targetOptions = computed(() => [
  ['ops_error_logs', t('admin.ops.cleanup.targets.opsErrorLogs')],
  ['ops_alert_events', t('admin.ops.cleanup.targets.opsAlertEvents')],
  ['ops_system_logs', t('admin.ops.cleanup.targets.opsSystemLogs')],
  ['ops_system_log_cleanup_audits', t('admin.ops.cleanup.targets.opsCleanupAudits')],
  ['ops_system_metrics', t('admin.ops.cleanup.targets.opsSystemMetrics')],
  ['ops_metrics_hourly', t('admin.ops.cleanup.targets.opsMetricsHourly')],
  ['ops_metrics_daily', t('admin.ops.cleanup.targets.opsMetricsDaily')],
  ['usage_logs', t('admin.ops.cleanup.targets.usageLogs')],
  ['audit_logs', t('admin.ops.cleanup.targets.auditLogs')],
  ['mall_purchases', t('admin.ops.cleanup.targets.mallPurchases')],
  ['payment_orders', t('admin.ops.cleanup.targets.paymentOrders')],
  ['bank_ledger', t('admin.ops.cleanup.targets.bankLedger')]
] as const)

const selectedTargetIsSensitive = computed(() => ['audit_logs', 'mall_purchases', 'payment_orders', 'bank_ledger'].includes(category.value))
const requiresSensitiveGateHint = computed(() => selectedTargetIsSensitive.value || mode.value === 'all')
const canExecute = computed(() => !!preview.value
  && previewFilterSnapshot.value === filterSnapshot(toFilter())
  && confirmation.value === preview.value.confirmation
  && (!preview.value.requires_totp || totpCode.value.length === 6))
const cronValid = computed(() => {
  if (!config.value?.data_retention.cleanup_enabled) return true
  return /^\S+(?:\s+\S+){4}$/.test(config.value.data_retention.cleanup_schedule.trim())
})
const hasPendingAudits = computed(() => audits.value.some(item => item.status === 'pending' || item.status === 'running'))
const auditRetentionEnabled = computed({
  get: () => (config.value?.audit_log_retention_days ?? 0) > 0,
  set: (enabled: boolean) => {
    if (!config.value) return
    config.value.audit_log_retention_days = enabled
      ? (config.value.audit_log_retention_days > 0 ? config.value.audit_log_retention_days : 180)
      : 0
  }
})

async function load() {
  loading.value = true
  configError.value = ''
  loadController.value?.abort()
  const controller = new AbortController()
  loadController.value = controller
  try {
    const loaded = await opsAPI.getDataCleanupConfig({ signal: controller.signal })
    if (controller.signal.aborted) return
    config.value = loaded
  } catch (err: any) {
    if (!controller.signal.aborted) {
      configError.value = showRequestError(err, t('admin.ops.cleanup.loadFailed'))
      appStore.showError(configError.value)
    }
  } finally {
    loading.value = false
  }
  await refreshAudits()
}

watch(() => props.show, (show) => {
  if (show) load()
  else {
    previewController.value?.abort()
    loadController.value?.abort()
    auditController.value?.abort()
    stopAuditPolling()
  }
})
watch([category, mode, startDate, endDate], () => {
  previewController.value?.abort()
  previewSeq.value++
  previewing.value = false
  preview.value = null
  previewFilterSnapshot.value = ''
  confirmation.value = ''
  totpCode.value = ''
  previewError.value = ''
})

function filterSnapshot(filter: DataCleanupFilter): string {
  return JSON.stringify({
    category: filter.category,
    mode: filter.mode,
    start_time: filter.start_time ?? null,
    end_time: filter.end_time ?? null
  })
}

function showRequestError(err: unknown, fallback: string): string {
  if (err && typeof err === 'object') {
    const value = err as {
      message?: string
      reason?: string
      error?: string
      response?: { data?: { message?: string; reason?: string; detail?: string; error?: string | { message?: string; reason?: string; detail?: string } } }
    }
    const responseData = value.response?.data
    const nestedError = responseData?.error
    const serverMessage = responseData?.detail
      || responseData?.message
      || responseData?.reason
      || (typeof nestedError === 'string' ? nestedError : nestedError?.detail || nestedError?.message || nestedError?.reason)
      || value.message
      || value.reason
      || value.error
    if (serverMessage) return serverMessage
  }
  return extractApiErrorMessage(err, fallback)
}

function toFilter(): DataCleanupFilter {
  return {
    category: category.value,
    mode: mode.value,
    ...(mode.value === 'range' ? {
      start_time: `${startDate.value}T00:00:00+08:00`,
      end_time: `${endDate.value}T00:00:00+08:00`
    } : {})
  }
}

async function saveConfig() {
  if (!config.value) return
  if (!cronValid.value) {
    configError.value = t('admin.ops.cleanup.cronInvalid')
    appStore.showError(configError.value)
    return
  }
  saving.value = true
  configError.value = ''
  try {
    config.value = await opsAPI.updateDataCleanupConfig(config.value)
    appStore.showSuccess(t('admin.ops.cleanup.saveSuccess'))
    emit('saved')
  } catch (err: any) {
    configError.value = showRequestError(err, t('admin.ops.cleanup.saveFailed'))
    appStore.showError(configError.value)
  } finally { saving.value = false }
}

async function runPreview() {
  if (mode.value === 'range' && (!startDate.value || !endDate.value)) {
    appStore.showError(t('admin.ops.cleanup.rangeRequired'))
    return
  }
  if (previewing.value) return
  const filter = toFilter()
  const snapshot = filterSnapshot(filter)
  previewController.value?.abort()
  const controller = new AbortController()
  previewController.value = controller
  const seq = ++previewSeq.value
  previewing.value = true
  previewError.value = ''
  try {
    const result = await opsAPI.previewDataCleanup(filter, { signal: controller.signal })
    if (controller.signal.aborted || seq !== previewSeq.value || snapshot !== filterSnapshot(toFilter())) return
    preview.value = result
    previewFilterSnapshot.value = snapshot
    confirmation.value = ''
    totpCode.value = ''
  } catch (err: any) {
    if (!controller.signal.aborted && seq === previewSeq.value) {
      previewError.value = showRequestError(err, t('admin.ops.cleanup.previewFailed'))
      appStore.showError(previewError.value)
    }
  } finally {
    if (seq === previewSeq.value) previewing.value = false
  }
}

async function execute() {
  if (executing.value || !preview.value || !canExecute.value || previewFilterSnapshot.value !== filterSnapshot(toFilter())) return
  executing.value = true
  executeError.value = ''
  try {
    const payload: DataCleanupExecuteRequest = {
      ...toFilter(),
      preview_rows: preview.value.matched_rows,
      confirmation: confirmation.value,
      totp_code: totpCode.value || undefined,
      preview_token: preview.value.preview_token
    }
    const result = await opsAPI.executeDataCleanup(payload)
    appStore.showSuccess(t(result.status === 'pending' ? 'admin.ops.cleanup.executePending' : 'admin.ops.cleanup.executeSuccess'))
    preview.value = null
    confirmation.value = ''
    totpCode.value = ''
    await refreshAudits()
    if (result.status === 'pending') startAuditPolling()
  } catch (err: any) {
    executeError.value = showRequestError(err, t('admin.ops.cleanup.executeFailed'))
    appStore.showError(executeError.value)
  } finally { executing.value = false }
}

async function refreshAudits() {
  auditLoading.value = true
  auditError.value = ''
  auditController.value?.abort()
  const controller = new AbortController()
  auditController.value = controller
  try {
    audits.value = await opsAPI.listDataCleanupAudits({ signal: controller.signal })
    if (hasPendingAudits.value) startAuditPolling()
    else stopAuditPolling()
  } catch (err: any) {
    if (!controller.signal.aborted) auditError.value = showRequestError(err, t('admin.ops.cleanup.auditLoadFailed'))
  } finally {
    if (!controller.signal.aborted) auditLoading.value = false
  }
}

function startAuditPolling() {
  if (auditPollTimer !== null) return
  auditPollTimer = window.setInterval(() => { void refreshAudits() }, 3000)
}

function stopAuditPolling() {
  if (auditPollTimer !== null) window.clearInterval(auditPollTimer)
  auditPollTimer = null
}

function auditOperator(item: DataCleanupAudit): string {
  if (item.operator_email) return item.operator_email
  if (item.operator_id) return `#${item.operator_id}`
  return item.auth_method || t('common.unknown')
}

function auditTaskId(item: DataCleanupAudit): string {
  if (item.task_id != null) return String(item.task_id)
  try {
    const filters = JSON.parse(item.filters || '{}') as { task_id?: string | number }
    return filters.task_id == null ? '-' : String(filters.task_id)
  } catch {
    return '-'
  }
}

onUnmounted(() => {
  previewController.value?.abort()
  loadController.value?.abort()
  auditController.value?.abort()
  stopAuditPolling()
})
</script>

<template>
  <BaseDialog :show="show" :title="t('admin.ops.cleanup.title')" width="extra-wide" @close="emit('close')">
    <div v-if="loading" class="py-10 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
    <div v-else-if="config" class="space-y-6">
      <div v-if="configError" data-testid="cleanup-config-error" class="flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/10 dark:text-red-300">
        <span>{{ configError }}</span>
        <button class="btn btn-secondary btn-sm" type="button" :disabled="saving" @click="saveConfig">{{ t('admin.ops.cleanup.retry') }}</button>
      </div>
      <section class="rounded-2xl border border-amber-200 bg-amber-50/70 p-4 dark:border-amber-900/50 dark:bg-amber-900/10">
        <div class="flex items-start justify-between gap-4">
          <div>
            <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ops.cleanup.automaticTitle') }}</h4>
            <p class="mt-1 text-xs text-gray-600 dark:text-gray-400">{{ t('admin.ops.cleanup.automaticHint') }}</p>
          </div>
          <Toggle v-model="config.data_retention.cleanup_enabled" />
        </div>
        <div class="mt-4 grid grid-cols-1 gap-3 md:grid-cols-2">
          <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.schedule') }}
            <input v-model="config.data_retention.cleanup_schedule" data-testid="cleanup-cron" class="input mt-1" :class="{ 'border-red-400': !cronValid }" placeholder="0 2 * * *" />
            <span class="mt-1 block text-xs" :class="cronValid ? 'text-gray-500' : 'text-red-600 dark:text-red-400'">{{ t(cronValid ? 'admin.ops.cleanup.cronHint' : 'admin.ops.cleanup.cronInvalid') }}</span>
          </label>
          <div class="text-sm text-gray-700 dark:text-gray-300">
            <div class="flex items-center justify-between gap-3"><span>{{ t('admin.ops.cleanup.auditRetention') }}</span><Toggle v-model="auditRetentionEnabled" /></div>
            <input v-model.number="config.audit_log_retention_days" class="input mt-1" type="number" min="0" max="3650" :disabled="!auditRetentionEnabled" />
          </div>
        </div>
        <div class="mt-4 grid grid-cols-1 gap-3 md:grid-cols-2">
          <div v-for="([key, label]) in targetOptions.slice(0, 8)" :key="key" class="flex items-center justify-between rounded-lg bg-white/60 p-3 dark:bg-dark-800/50">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ label }}</span>
            <div class="flex items-center gap-2">
              <input v-model.number="config.data_retention.targets[key].retention_days" class="input w-24" type="number" min="0" max="365" :disabled="!config.data_retention.targets[key].enabled" />
              <Toggle v-model="config.data_retention.targets[key].enabled" />
            </div>
          </div>
        </div>
        <button class="btn btn-primary mt-4" data-testid="cleanup-save" type="button" :disabled="saving || !cronValid" @click="saveConfig">{{ saving ? t('common.saving') : t('common.save') }}</button>
      </section>

      <section class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700">
        <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ops.cleanup.manualTitle') }}</h4>
        <div class="mt-4 grid grid-cols-1 gap-3 md:grid-cols-3">
          <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.category') }}
            <select v-model="category" class="input mt-1">
              <option v-for="([key, label]) in targetOptions" :key="key" :value="key">{{ label }}</option>
            </select>
          </label>
          <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.mode') }}
            <select v-model="mode" class="input mt-1"><option value="range">{{ t('admin.ops.cleanup.rangeMode') }}</option><option value="all">{{ t('admin.ops.cleanup.allMode') }}</option></select>
          </label>
          <div class="flex items-end"><button class="btn btn-secondary w-full" data-testid="cleanup-preview" type="button" :disabled="previewing" @click="runPreview"><Icon name="search" size="sm" class="mr-1" />{{ previewing ? t('common.loading') : t('admin.ops.cleanup.preview') }}</button></div>
        </div>
        <div v-if="mode === 'range'" class="mt-3 grid grid-cols-1 gap-3 md:grid-cols-2">
          <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.startDate') }}<input v-model="startDate" class="input mt-1" type="date" /></label>
          <label class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.endDate') }}<input v-model="endDate" class="input mt-1" type="date" /></label>
        </div>
        <p v-if="requiresSensitiveGateHint" class="mt-3 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.ops.cleanup.sensitiveHint') }}</p>
        <div v-if="previewError" data-testid="cleanup-preview-error" class="mt-3 flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/10 dark:text-red-300">
          <span>{{ previewError }}</span>
          <button class="btn btn-secondary btn-sm" type="button" :disabled="previewing" @click="runPreview">{{ t('admin.ops.cleanup.retry') }}</button>
        </div>
        <div v-if="preview" class="mt-4 rounded-lg bg-gray-50 p-4 dark:bg-dark-800">
          <p class="text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.previewRows', { count: preview.matched_rows }) }}<span v-if="preview.blocked_rows">，{{ t('admin.ops.cleanup.blockedRows', { count: preview.blocked_rows }) }}</span></p>
          <p v-if="preview.deletion_warning" class="mt-1 text-xs text-amber-700">{{ preview.deletion_warning }}</p>
          <label class="mt-3 block text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.confirmation', { value: preview.confirmation }) }}<input v-model="confirmation" data-testid="cleanup-confirmation" class="input mt-1" /></label>
          <label v-if="preview.requires_totp" class="mt-3 block text-sm text-gray-700 dark:text-gray-300">{{ t('admin.ops.cleanup.totp') }}<input v-model="totpCode" data-testid="cleanup-totp" class="input mt-1" type="password" inputmode="numeric" autocomplete="one-time-code" maxlength="6" /></label>
          <button class="btn btn-danger mt-4" data-testid="cleanup-execute" type="button" :disabled="executing || !canExecute" @click="execute">{{ executing ? t('common.loading') : t('admin.ops.cleanup.execute') }}</button>
          <div v-if="executeError" data-testid="cleanup-execute-error" class="mt-3 flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/10 dark:text-red-300">
            <span>{{ executeError }}</span>
            <button class="btn btn-secondary btn-sm" type="button" :disabled="executing || !canExecute" @click="execute">{{ t('admin.ops.cleanup.retry') }}</button>
          </div>
        </div>
      </section>

      <section class="rounded-2xl border border-gray-200 p-4 dark:border-dark-700">
        <div class="flex items-center justify-between gap-3">
          <h4 class="font-semibold text-gray-900 dark:text-white">{{ t('admin.ops.cleanup.auditTitle') }}</h4>
          <button class="btn btn-secondary btn-sm" data-testid="cleanup-audit-refresh" type="button" :disabled="auditLoading" @click="refreshAudits"><Icon name="refresh" size="sm" class="mr-1" />{{ t('common.refresh') }}</button>
        </div>
        <p v-if="hasPendingAudits" data-testid="cleanup-audit-pending" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ t('admin.ops.cleanup.auditPending') }}</p>
        <div v-if="auditError" data-testid="cleanup-audit-error" class="mt-3 flex items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/10 dark:text-red-300">
          <span>{{ auditError }}</span>
          <button class="btn btn-secondary btn-sm" type="button" :disabled="auditLoading" @click="refreshAudits">{{ t('admin.ops.cleanup.retry') }}</button>
        </div>
        <div class="mt-3 overflow-x-auto">
          <table class="min-w-full text-left text-xs">
            <thead><tr><th class="p-2">{{ t('admin.ops.cleanup.auditTime') }}</th><th class="p-2">{{ t('admin.ops.cleanup.operator') }}</th><th class="p-2">{{ t('admin.ops.cleanup.category') }}</th><th class="p-2">{{ t('admin.ops.cleanup.mode') }}</th><th class="p-2">{{ t('admin.ops.cleanup.rowCounts') }}</th><th class="p-2">{{ t('admin.ops.cleanup.status') }}</th><th class="p-2">{{ t('admin.ops.cleanup.taskId') }}</th><th class="p-2">{{ t('admin.ops.cleanup.error') }}</th></tr></thead>
            <tbody>
              <tr v-for="item in audits" :key="item.id" class="border-t border-gray-100 dark:border-dark-700">
                <td class="whitespace-nowrap p-2">{{ new Date(item.created_at).toLocaleString() }}</td>
                <td class="p-2">{{ auditOperator(item) }}</td>
                <td class="p-2">{{ item.category }}</td>
                <td class="p-2">{{ item.mode }}</td>
                <td class="whitespace-nowrap p-2">{{ t('admin.ops.cleanup.rowCountValues', { preview: item.preview_rows, blocked: item.blocked_rows, deleted: item.deleted_rows }) }}</td>
                <td class="p-2">{{ item.status }}</td>
                <td class="p-2">{{ auditTaskId(item) }}</td>
                <td class="max-w-64 break-words p-2 text-red-600 dark:text-red-400">{{ item.error_message || '-' }}</td>
              </tr>
              <tr v-if="!audits.length && !auditLoading"><td class="p-4 text-center text-gray-500" colspan="8">{{ t('common.noData') }}</td></tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>
    <div v-else-if="configError" data-testid="cleanup-load-error" class="space-y-3 rounded-lg border border-red-200 bg-red-50 p-4 text-sm text-red-700 dark:border-red-900/50 dark:bg-red-900/10 dark:text-red-300">
      <p>{{ configError }}</p>
      <button class="btn btn-secondary btn-sm" type="button" :disabled="loading" @click="load">{{ t('admin.ops.cleanup.retry') }}</button>
    </div>
    <template #footer>
      <button class="btn btn-secondary" data-testid="cleanup-back" type="button" @click="emit('back')"><Icon name="arrowLeft" size="sm" class="mr-1" />{{ t('common.back') }}</button>
      <button class="btn btn-secondary" data-testid="cleanup-close" type="button" @click="emit('close')">{{ t('common.close') }}</button>
    </template>
  </BaseDialog>
</template>
