<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import Toggle from '@/components/common/Toggle.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { useAppStore } from '@/stores/app'
import { dataCleanupAPI, type CleanupOverview } from '@/api/admin/dataCleanup'
import type { DataCleanupAudit, DataCleanupConfig, DataCleanupFilter, DataCleanupPreview } from '@/api/admin/ops'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAuthStore } from '@/stores/auth'
import FilterDeleteDialog from '@/features/prompt-audit/components/FilterDeleteDialog.vue'
import { emptyEventFilters, promptCleanupFilter } from '@/features/prompt-audit/viewModel'
import type { PromptEventFilters } from '@/features/prompt-audit/types'

const { t } = useI18n()
const app = useAppStore()
const auth = useAuthStore()
const tab = ref<'overview' | 'cleanup' | 'policies' | 'tasks'>('overview')
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const overview = ref<CleanupOverview | null>(null)
const config = ref<DataCleanupConfig | null>(null)
const audits = ref<DataCleanupAudit[]>([])
const category = ref('server_auto_retry')
const start = ref('')
const end = ref('')
const month = ref('')
const all = ref(false)
const preview = ref<DataCleanupPreview | null>(null)
const previewSnapshot = ref('')
const confirmation = ref('')
const totp = ref('')
const cancelId = ref<number | null>(null)
const promptDialog = ref(false)
const promptPreview = ref<DataCleanupPreview | null>(null)
const promptPreviewFilters = ref<DataCleanupFilter | null>(null)
const promptFilters = emptyEventFilters()
let poll: ReturnType<typeof setInterval> | undefined
let controller: AbortController | undefined
let previewController: AbortController | undefined
let tasksController: AbortController | undefined
let disposed = false
let promptSequence = 0

const allCategories = [
  'server_auto_retry', 'http_access', 'audit_statistics', 'ops_system_logs', 'ops_error_logs',
  'ops_alert_events', 'ops_system_metrics', 'ops_metrics_hourly', 'ops_metrics_daily',
  'ops_system_log_cleanup_audits', 'audit_logs', 'usage_logs'
]
const labels: Record<string, string> = {
  server_auto_retry: 'serverRetry', http_access: 'httpAccess', audit_statistics: 'auditStatistics',
  ops_system_logs: 'runtime', ops_error_logs: 'errors', ops_alert_events: 'alerts',
  ops_system_metrics: 'minuteMetrics', ops_metrics_hourly: 'hourlyMetrics', ops_metrics_daily: 'dailyMetrics',
  ops_system_log_cleanup_audits: 'cleanupAudit', audit_logs: 'operationAudit', usage_logs: 'usageCompaction', prompt_audit: 'promptAudit'
}
const categories = computed(() => allCategories.filter(value => auth.canAdmin(value.startsWith('audit_') ? 'audit.manage' : 'ops.manage')))
const label = (value: string) => t(`dataCleanup.categories.${labels[value] || value}`)
const isUsage = computed(() => category.value === 'usage_logs')
const bytes = (value: number) => `${(value / 1048576).toFixed(2)} MiB`
const num = (value: number) => new Intl.NumberFormat().format(value)
const lastMonth = computed(() => {
  const now = new Date()
  const parts = new Intl.DateTimeFormat('en', { timeZone: overview.value?.timezone || 'Asia/Shanghai', year: 'numeric', month: 'numeric' }).formatToParts(now)
  const year = Number(parts.find(part => part.type === 'year')?.value)
  const mon = Number(parts.find(part => part.type === 'month')?.value) - 1
  const previous = new Date(Date.UTC(year, mon - 1, 1))
  return `${previous.getUTCFullYear()}-${String(previous.getUTCMonth() + 1).padStart(2, '0')}`
})

function filter(): DataCleanupFilter {
  if (isUsage.value) {
    return { category: category.value, mode: 'range', month: month.value }
  }
  return { category: category.value, mode: all.value ? 'all' : 'range', ...(all.value ? {} : {
    start_date: start.value, end_date: end.value
  }) }
}

const canExecute = computed(() => !!preview.value && preview.value.matched_rows > 0
  && previewSnapshot.value === JSON.stringify(filter()) && confirmation.value === preview.value.confirmation
  && (!preview.value.requires_totp || /^\d{6}$/.test(totp.value)))
const canEditSchedule = computed(() => auth.canAdmin('ops.manage') && auth.canAdmin('audit.manage'))
const policyAllowed = (name: string) => auth.canAdmin(name.startsWith('audit_') ? 'audit.manage' : 'ops.manage')

watch([category, start, end, month, all], () => {
  previewController?.abort()
  preview.value = null
  previewSnapshot.value = ''
  confirmation.value = ''
  totp.value = ''
})

async function load() {
  loading.value = true
  error.value = ''
  controller?.abort()
  controller = new AbortController()
  const signal = controller.signal
  try {
    const result = await Promise.all([dataCleanupAPI.config(signal), dataCleanupAPI.overview(signal)])
    if (signal.aborted) return
    config.value = result[0]
    overview.value = result[1]
    if (!categories.value.includes(category.value) && categories.value.length) category.value = categories.value[0]
    await refreshTasks(true)
  } catch (err) { if (!signal.aborted) error.value = extractApiErrorMessage(err, t('dataCleanup.loadFailed')) }
  finally { if (!signal.aborted) loading.value = false }
}

async function refreshTasks(force = false) {
  if (disposed || (tasksController && !force)) return
  tasksController?.abort()
  const request = new AbortController()
  tasksController = request
  try { const result = await dataCleanupAPI.audits(request.signal); if (!disposed && !request.signal.aborted) { audits.value = result; syncPoll() } }
  catch (err) { if (!disposed && !request.signal.aborted) error.value = extractApiErrorMessage(err, t('dataCleanup.loadFailed')) }
  finally { if (tasksController === request) tasksController = undefined }
}

function syncPoll() {
  if (disposed) return
  const pending = tab.value === 'tasks' || audits.value.some(item => item.status === 'pending' || item.status === 'running')
  if (pending && !poll) poll = setInterval(() => { void refreshTasks() }, 3000)
  if (!pending && poll) { clearInterval(poll); poll = undefined }
}

async function runPreview() {
  if (busy.value) return
  if ((!isUsage.value && !all.value && (!start.value || !end.value)) || (isUsage.value && !month.value)) return
  busy.value = true
  error.value = ''
  preview.value = null
  previewSnapshot.value = ''
  confirmation.value = ''
  totp.value = ''
  const payload = filter()
  const snapshot = JSON.stringify(payload)
  previewController?.abort()
  const request = new AbortController()
  previewController = request
  try {
    const result = await dataCleanupAPI.preview(payload, request.signal)
    if (!request.signal.aborted && snapshot === JSON.stringify(filter())) {
      preview.value = result
      previewSnapshot.value = snapshot
    }
  } catch (err) { if (!request.signal.aborted) error.value = extractApiErrorMessage(err, t('dataCleanup.previewFailed')) }
  finally { busy.value = false }
}

async function execute() {
  if (!preview.value || !canExecute.value || busy.value) return
  busy.value = true
  try {
    await dataCleanupAPI.execute({ ...filter(), preview_rows: preview.value.matched_rows,
      preview_token: preview.value.preview_token, confirmation: confirmation.value, totp_code: totp.value || undefined })
    preview.value = null
    confirmation.value = ''
    totp.value = ''
    tab.value = 'tasks'
    await refreshTasks(true)
    app.showSuccess(t('dataCleanup.submitted'))
  } catch (err) { error.value = extractApiErrorMessage(err, t('dataCleanup.executeFailed')) }
  finally { busy.value = false }
}

async function save() {
  if (!config.value || busy.value) return
  busy.value = true
  try { config.value = await dataCleanupAPI.save(config.value); app.showSuccess(t('common.saved')) }
  catch (err) { error.value = extractApiErrorMessage(err, t('dataCleanup.saveFailed')) }
  finally { busy.value = false }
}

function progress(item: DataCleanupAudit): number {
  try { return JSON.parse(item.filters).processed_rows ?? item.deleted_rows }
  catch { return item.deleted_rows }
}

async function cancel() {
  if (cancelId.value == null || busy.value) return
  busy.value = true
  try { await dataCleanupAPI.cancel(cancelId.value); cancelId.value = null; await refreshTasks(true) }
  catch (err) { error.value = extractApiErrorMessage(err, t('dataCleanup.cancelFailed')) }
  finally { busy.value = false }
}

async function previewPrompt(value: PromptEventFilters) {
  if (busy.value) return
  const sequence = ++promptSequence
  busy.value = true
  promptPreview.value = null
  promptPreviewFilters.value = null
  const payload = promptCleanupFilter(value)
  try { const result = await dataCleanupAPI.preview(payload); if (sequence === promptSequence && !disposed && promptDialog.value) { promptPreview.value = result; promptPreviewFilters.value = payload } }
  catch (err) { error.value = extractApiErrorMessage(err, t('dataCleanup.previewFailed')) }
  finally { busy.value = false }
}

async function deletePrompt(promptConfirmation: string, promptTotp: string) {
  if (!promptPreview.value || !promptPreviewFilters.value || busy.value) return
  if (promptConfirmation !== promptPreview.value.confirmation || !/^\d{6}$/.test(promptTotp)) return
  busy.value = true
  try {
    await dataCleanupAPI.execute({ ...promptPreviewFilters.value, preview_rows: promptPreview.value.matched_rows,
      preview_token: promptPreview.value.preview_token, confirmation: promptConfirmation, totp_code: promptTotp })
    promptDialog.value = false
    clearPromptPreview()
    tab.value = 'tasks'
    await refreshTasks(true)
    app.showSuccess(t('dataCleanup.submitted'))
  }
  catch (err) { error.value = extractApiErrorMessage(err, t('dataCleanup.executeFailed')); promptPreview.value = null }
  finally { busy.value = false }
}

onMounted(() => { month.value = lastMonth.value; void load() })
function clearPromptPreview() { promptSequence++; promptPreview.value = null; promptPreviewFilters.value = null }
watch(promptDialog, () => { clearPromptPreview() })
watch(tab, (value) => { syncPoll(); if (value === 'tasks') void refreshTasks(true) })
onUnmounted(() => { disposed = true; promptSequence++; controller?.abort(); previewController?.abort(); tasksController?.abort(); if (poll) clearInterval(poll) })
</script>

<template>
  <AppLayout>
    <div class="min-w-0 space-y-5">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <h1 class="text-xl font-semibold text-gray-900 dark:text-white">{{ t('dataCleanup.title') }}</h1>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="load"><Icon name="refresh" size="sm" class="mr-2" />{{ t('common.refresh') }}</button>
      </header>
      <nav class="flex flex-wrap gap-5 border-b border-gray-200 dark:border-dark-700" :aria-label="t('dataCleanup.title')">
        <button v-for="view in (['overview', 'cleanup', 'policies', 'tasks'] as const)" :key="view" type="button"
          class="border-b-2 px-1 py-3 text-sm font-medium" :class="tab === view ? 'border-primary-500 text-primary-600' : 'border-transparent text-gray-500'" @click="tab = view">{{ t(`dataCleanup.tabs.${view}`) }}</button>
      </nav>
      <div v-if="error" role="alert" class="rounded border border-red-200 bg-red-50 p-3 text-sm text-red-700 dark:border-red-900 dark:bg-red-950/20">{{ error }}</div>
      <div v-if="loading && !config" class="py-12 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <section v-else-if="tab === 'overview' && overview" class="space-y-5">
        <p v-if="overview.last_cleanup_at" class="text-xs text-gray-500">{{ t('dataCleanup.lastRun') }}: {{ new Date(overview.last_cleanup_at).toLocaleString() }}<span class="ml-3 break-all font-mono">{{ overview.last_cleanup_result }}</span></p>
        <dl class="grid grid-cols-2 gap-4 border-b border-gray-200 pb-5 dark:border-dark-700 lg:grid-cols-4">
          <div><dt class="text-xs text-gray-500">{{ t('dataCleanup.databaseSize') }}</dt><dd class="mt-1 text-lg font-semibold">{{ bytes(overview.database_bytes) }}</dd></div>
          <div><dt class="text-xs text-gray-500">{{ t('dataCleanup.totalRequests') }}</dt><dd class="mt-1 text-lg font-semibold">{{ num(overview.requests) }}</dd></div>
          <div><dt class="text-xs text-gray-500">{{ t('dataCleanup.totalTokens') }}</dt><dd class="mt-1 break-all text-lg font-semibold">{{ num(overview.tokens) }}</dd></div>
          <div><dt class="text-xs text-gray-500">{{ t('dataCleanup.totalCost') }}</dt><dd class="mt-1 text-lg font-semibold">{{ Number(overview.actual_cost).toFixed(6) }}</dd></div>
        </dl>
        <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="py-3">{{ t('dataCleanup.table') }}</th><th>{{ t('dataCleanup.rows') }}</th><th>{{ t('dataCleanup.dataSize') }}</th><th>{{ t('dataCleanup.indexSize') }}</th><th>{{ t('dataCleanup.total') }}</th><th>{{ t('dataCleanup.state') }}</th></tr></thead>
          <tbody><tr v-for="row in overview.tables" :key="row.table" class="border-t border-gray-100 dark:border-dark-700"><td class="py-3 pr-4 font-mono text-xs">{{ row.table }}</td><td class="pr-4 tabular-nums">{{ num(row.estimated_rows) }}</td><td class="whitespace-nowrap pr-4">{{ bytes(row.table_bytes) }}</td><td class="whitespace-nowrap pr-4">{{ bytes(row.index_bytes) }}</td><td class="whitespace-nowrap pr-4">{{ bytes(row.total_bytes) }}</td><td><span :class="row.protected ? 'text-emerald-700 dark:text-emerald-400' : 'text-gray-500'">{{ t(row.protected ? 'dataCleanup.protected' : 'dataCleanup.managed') }}</span></td></tr></tbody>
        </table></div>
        <h2 class="text-sm font-semibold">{{ t('dataCleanup.closedMonths') }}</h2>
        <div v-if="overview.years?.length" class="flex flex-wrap gap-6 border-b border-gray-100 pb-4 dark:border-dark-700"><dl v-for="year in overview.years" :key="year.month"><dt class="text-xs text-gray-500">{{ year.month }}</dt><dd class="mt-1 text-sm">{{ num(year.tokens) }} Token <span class="ml-3 tabular-nums">{{ Number(year.actual_cost).toFixed(6) }}</span></dd></dl></div>
        <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="py-2">{{ t('dataCleanup.month') }}</th><th>{{ t('dataCleanup.users') }}</th><th>{{ t('dataCleanup.totalRequests') }}</th><th>{{ t('dataCleanup.totalTokens') }}</th><th>{{ t('dataCleanup.totalCost') }}</th></tr></thead><tbody><tr v-for="row in overview.months" :key="row.month" class="border-t border-gray-100 dark:border-dark-700"><td class="py-2">{{ row.month.slice(0, 7) }}</td><td>{{ row.users }}</td><td>{{ num(row.requests) }}</td><td>{{ num(row.tokens) }}</td><td>{{ Number(row.actual_cost).toFixed(6) }}</td></tr><tr v-if="!overview.months.length"><td colspan="5" class="py-6 text-center text-gray-500">{{ t('common.noData') }}</td></tr></tbody></table></div>
      </section>
      <section v-else-if="tab === 'cleanup'" class="space-y-5">
        <button v-if="auth.canAdmin('audit.manage')" type="button" class="btn btn-secondary" @click="promptDialog = true"><Icon name="shield" size="sm" class="mr-2" />{{ t('admin.promptAudit.events.deleteByFilter') }}</button>
        <div class="grid items-end gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <label class="text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('dataCleanup.category') }}</span><select v-model="category" class="input"><option v-for="value in categories" :key="value" :value="value">{{ label(value) }}</option></select></label>
          <label v-if="isUsage" class="text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('dataCleanup.month') }}</span><input v-model="month" type="month" class="input" :max="lastMonth" /></label>
          <template v-else>
            <label class="text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('dataCleanup.startDate') }}</span><input v-model="start" type="date" class="input" :disabled="all" /></label>
            <label class="text-sm"><span class="mb-1 block text-gray-600 dark:text-gray-300">{{ t('dataCleanup.endExclusive') }}</span><input v-model="end" type="date" class="input" :disabled="all" /></label>
          </template>
          <button type="button" class="btn btn-primary" :disabled="busy" @click="runPreview"><Icon name="eye" size="sm" class="mr-2" />{{ t('dataCleanup.preview') }}</button>
        </div>
        <label v-if="!isUsage" class="flex items-center gap-2 text-sm"><input v-model="all" type="checkbox" />{{ t('dataCleanup.allRecords') }}</label>
        <p v-if="isUsage" class="text-sm text-emerald-700 dark:text-emerald-400">{{ t('dataCleanup.usageProtected') }}</p>
        <div v-if="preview" class="max-w-3xl rounded border border-gray-200 p-5 dark:border-dark-700">
          <p v-if="preview.estimated_bytes" class="mb-3 text-sm text-gray-500">{{ t('dataCleanup.estimated') }}: {{ bytes(preview.estimated_bytes) }}</p>
          <dl class="grid grid-cols-2 gap-4 text-sm"><div><dt class="text-gray-500">{{ t('dataCleanup.matched') }}</dt><dd class="mt-1 font-semibold">{{ num(preview.matched_rows) }}</dd></div><div><dt class="text-gray-500">{{ t('dataCleanup.retained') }}</dt><dd class="mt-1 font-semibold text-emerald-700">{{ num(preview.blocked_rows) }}</dd></div></dl>
          <p v-if="preview.deletion_warning" class="mt-3 text-sm text-amber-700">{{ preview.deletion_warning }}</p>
          <p class="mt-3 text-xs text-gray-500">{{ t('dataCleanup.diskNotice') }}</p>
          <label class="mt-5 block text-sm">{{ t('dataCleanup.confirmation') }}<code class="ml-2 break-all">{{ preview.confirmation }}</code><input v-model="confirmation" class="input mt-2" autocomplete="off" /></label>
          <label v-if="preview.requires_totp" class="mt-4 block text-sm">{{ t('dataCleanup.totp') }}<input v-model="totp" class="input mt-2 block max-w-52" type="password" inputmode="numeric" maxlength="6" autocomplete="one-time-code" /></label>
          <button type="button" class="btn btn-danger mt-5" :disabled="!canExecute || busy" @click="execute"><Icon :name="isUsage ? 'sync' : 'trash'" size="sm" class="mr-2" />{{ t(isUsage ? 'dataCleanup.compact' : 'dataCleanup.execute') }}</button>
        </div>
      </section>
      <section v-else-if="tab === 'policies' && config" class="space-y-5">
        <div class="flex flex-wrap items-center justify-between gap-4 border-b border-gray-200 pb-4 dark:border-dark-700"><label class="flex items-center gap-3 text-sm"><Toggle v-model="config.data_retention.cleanup_enabled" :disabled="!canEditSchedule" />{{ t('dataCleanup.automatic') }}</label><label class="flex items-center gap-3 text-sm">{{ t('dataCleanup.schedule') }}<input v-model="config.data_retention.cleanup_schedule" :disabled="!canEditSchedule" class="input w-40 font-mono" /></label></div>
        <div class="overflow-x-auto"><table class="w-full text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="py-2">{{ t('dataCleanup.category') }}</th><th>{{ t('dataCleanup.enabled') }}</th><th>{{ t('dataCleanup.retention') }}</th></tr></thead><tbody>
          <tr v-for="(policy, name) in config.data_retention.targets" :key="name" class="border-t border-gray-100 dark:border-dark-700"><td class="py-3 pr-4">{{ labels[name] ? label(name) : name }}</td><td class="pr-5"><Toggle v-model="policy.enabled" :disabled="!policyAllowed(name)" /></td><td><span v-if="name === 'usage_logs'" class="text-xs text-gray-500">{{ t('dataCleanup.closedMonthPolicy') }}</span><input v-else v-model.number="policy.retention_days" :disabled="!policyAllowed(name)" class="input w-28" type="number" min="0" max="365" /></td></tr>
          <tr class="border-t border-gray-100 dark:border-dark-700"><td class="py-3">{{ t('dataCleanup.categories.operationAudit') }}</td><td class="pr-5"><Toggle :model-value="config.audit_log_retention_days > 0" :disabled="!auth.canAdmin('audit.manage')" @update:model-value="config.audit_log_retention_days = $event ? 180 : 0" /></td><td><input v-model.number="config.audit_log_retention_days" :disabled="!auth.canAdmin('audit.manage')" class="input w-28" type="number" min="0" max="3650" /></td></tr>
        </tbody></table></div>
        <button type="button" class="btn btn-primary" :disabled="busy" @click="save"><Icon name="check" size="sm" class="mr-2" />{{ t('common.save') }}</button>
      </section>
      <section v-else-if="tab === 'tasks'" class="space-y-4">
        <div class="overflow-x-auto"><table class="w-full min-w-[740px] text-left text-sm"><thead class="text-xs text-gray-500"><tr><th class="py-3">{{ t('dataCleanup.time') }}</th><th>{{ t('dataCleanup.operator') }}</th><th>{{ t('dataCleanup.category') }}</th><th>{{ t('dataCleanup.progress') }}</th><th>{{ t('dataCleanup.deleted') }}</th><th>{{ t('dataCleanup.state') }}</th><th>{{ t('dataCleanup.error') }}</th><th></th></tr></thead><tbody>
          <tr v-for="item in audits" :key="item.id" class="border-t border-gray-100 align-top dark:border-dark-700"><td class="whitespace-nowrap py-3 pr-4 text-xs">{{ new Date(item.created_at).toLocaleString() }}</td><td class="pr-4 text-xs">{{ item.operator_email || item.operator_id || item.auth_method }}</td><td class="pr-4">{{ labels[item.category] ? label(item.category) : item.category }}</td><td class="min-w-36 pr-5"><span class="text-xs">{{ num(progress(item)) }} / {{ num(item.preview_rows) }}</span><progress class="mt-1 h-1.5 w-full accent-emerald-600" :max="Math.max(item.preview_rows, 1)" :value="progress(item)" /></td><td class="pr-4">{{ num(item.deleted_rows) }}</td><td class="pr-4 text-xs">{{ t(`dataCleanup.status.${item.status}`) }}</td><td class="max-w-64 break-words text-xs text-red-600">{{ item.error_message || '-' }}</td><td><button v-if="item.status === 'pending' || item.status === 'running'" type="button" class="btn btn-secondary btn-sm" :title="t('common.cancel')" @click="cancelId = item.id"><Icon name="x" size="sm" /></button></td></tr>
          <tr v-if="!audits.length"><td colspan="8" class="py-8 text-center text-gray-500">{{ t('common.noData') }}</td></tr>
        </tbody></table></div>
      </section>
    </div>
    <BaseDialog :show="cancelId !== null" :title="t('dataCleanup.cancelTitle')" width="narrow" @close="cancelId = null"><p class="text-sm">{{ t('dataCleanup.cancelNotice') }}</p><template #footer><button class="btn btn-secondary" type="button" @click="cancelId = null">{{ t('common.close') }}</button><button class="btn btn-danger" type="button" :disabled="busy" @click="cancel">{{ t('common.confirm') }}</button></template></BaseDialog>
    <FilterDeleteDialog :show="promptDialog" :initial-filters="promptFilters" :preview="promptPreview" :previewing="busy" :deleting="busy" @close="promptDialog = false" @preview="previewPrompt" @confirm="deletePrompt" @criteria-change="clearPromptPreview" />
  </AppLayout>
</template>
