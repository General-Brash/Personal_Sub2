<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { groupsAPI, accountsAPI } from '@/api/admin'
import {
  serverAutoRetryAPI,
  recommendedServerAutoRetryConfig,
  type ServerAutoRetryConfig,
  type ServerAutoRetryEvent
} from '@/api/admin/serverAutoRetry'
import { formatDateTime } from '@/utils/format'

const { t, te } = useI18n()
const route = useRoute()
const router = useRouter()
const appStore = useAppStore()
const authStore = useAuthStore()
const key = 'admin.serverAutoRetry'
const maxDurationMS = 9223372036854
const config = ref<ServerAutoRetryConfig | null>(null)
const configLoading = ref(false)
const configError = ref('')
const events = ref<ServerAutoRetryEvent[]>([])
const eventsLoading = ref(false)
const eventsError = ref('')
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const requestId = ref('')
const appliedRequestId = computed(() => typeof route.query.request_id === 'string' ? route.query.request_id.trim() : '')
const settingsOpen = ref(false)
const saving = ref(false)
const saveError = ref('')
const draft = ref(recommendedServerAutoRetryConfig())
const modelsInput = ref('')
const groupIdInput = ref<number | string>('')
const accountIdInput = ref<number | string>('')
const groupOptions = ref<SelectOption[]>([])
const accountOptions = ref<SelectOption[]>([])
const groupLabels = ref<Record<number, string>>({})
const accountLabels = ref<Record<number, string>>({})
const groupsLoading = ref(false)
const accountsLoading = ref(false)
const groupsError = ref('')
const accountsError = ref('')
let configRequest: AbortController | undefined
let eventsRequest: AbortController | undefined
let accountsRequest: AbortController | undefined
let disposed = false

const canReadGroups = computed(() => authStore.canAdmin('groups.read'))
const canReadAccounts = computed(() => authStore.canAdmin('accounts.catalog.read'))
const emptyStrategies = ['same_account_only', 'same_then_switch', 'prefer_other_account'] as const
const rateLimitStrategies = ['same_account_only', 'prefer_other_account'] as const
const emptyStrategyOptions = computed(() => emptyStrategies.map(value => ({ value, label: enumLabel('strategies', value) })))
const rateLimitStrategyOptions = computed(() => rateLimitStrategies.map(value => ({ value, label: enumLabel('strategies', value) })))
const availableGroups = computed(() => groupOptions.value.filter(option => !draft.value.scope.group_ids.includes(Number(option.value))))
const availableAccounts = computed(() => accountOptions.value.filter(option => !draft.value.scope.account_ids.includes(Number(option.value))))
const waitFields = [
  { key: 'empty_backoff_base_ms', label: 'emptyBackoff' },
  { key: 'rate_limit_backoff_base_ms', label: 'rateLimitBackoff' },
  { key: 'backoff_cap_ms', label: 'backoffCap' },
  { key: 'jitter_ms', label: 'jitter' }
] as const
const rules = computed(() => config.value ? [
  { name: 'azure400', description: 'azureRule', ...config.value.azure400 },
  { name: 'emptyResponse', description: 'emptyRule', ...config.value.empty_response }
] : [])

function cloneConfig(value: ServerAutoRetryConfig): ServerAutoRetryConfig {
  return {
    schema_version: value.schema_version,
    enabled: value.enabled,
    scope: {
      platform: value.scope.platform,
      group_ids: [...value.scope.group_ids],
      account_ids: [...value.scope.account_ids],
      models: [...value.scope.models]
    },
    max_additional_attempts: value.max_additional_attempts,
    max_total_retry_wait_ms: value.max_total_retry_wait_ms,
    azure400_strip_input_ids: value.azure400_strip_input_ids,
    azure400: { enabled: value.azure400.enabled, max_retries: value.azure400.max_retries },
    empty_response: { enabled: value.empty_response.enabled, max_retries: value.empty_response.max_retries },
    empty_retry_strategy: value.empty_retry_strategy,
    same_account_retries: value.same_account_retries,
    rate_limit_retry_strategy: value.rate_limit_retry_strategy,
    empty_backoff_base_ms: value.empty_backoff_base_ms,
    rate_limit_backoff_base_ms: value.rate_limit_backoff_base_ms,
    backoff_cap_ms: value.backoff_cap_ms,
    jitter_ms: value.jitter_ms,
    respect_retry_after: value.respect_retry_after
  }
}

function errorMessage(error: unknown, fallback: string, params: Record<string, string> = {}): string {
  const message = (error as { message?: unknown } | null)?.message
  return typeof message === 'string' && message ? message : t(`${key}.${fallback}`, params)
}

function enumLabel(category: string, value: unknown): string {
  if (typeof value !== 'string' || !value.trim()) return t(`${key}.unknown`)
  const path = `${key}.${category}.${value}`
  return te(path) ? t(path) : value
}

function numberLabel(value: unknown): string {
  return typeof value === 'number' && Number.isFinite(value) ? String(value) : t(`${key}.unknown`)
}

function statusLabel(value: unknown): string {
  if (typeof value === 'number') return numberLabel(value)
  if (value === 'unknown') return t(`${key}.unknown`)
  return typeof value === 'string' && value.trim() ? value : t(`${key}.unknown`)
}

function booleanLabel(value: unknown): string {
  return t(`${key}.${value === true ? 'yes' : value === false ? 'no' : 'unknown'}`)
}

function timeLabel(value: string | null | undefined): string {
  return formatDateTime(value) || t(`${key}.unknown`)
}

function scopeLabel(kind: 'groups' | 'accounts' | 'models'): string {
  if (!config.value) return t(`${key}.unknown`)
  const scope = config.value.scope
  if (kind === 'models') return scope.models.join(', ') || t(`${key}.all`)
  const ids = kind === 'groups' ? scope.group_ids : scope.account_ids
  const labels = kind === 'groups' ? groupLabels.value : accountLabels.value
  return ids.map(id => labels[id] || `#${id}`).join(', ') || t(`${key}.all`)
}

function validationError(value: ServerAutoRetryConfig): string {
  if (!value || value.schema_version !== 1 || value.scope?.platform !== 'openai') return t(`${key}.invalidSchema`)
  const ranges: Array<[string, unknown, number, number]> = [
    ['maxAdditionalAttempts', value.max_additional_attempts, 0, 30],
    ['azure400', value.azure400?.max_retries, 0, Math.min(1, value.max_additional_attempts)],
    ['emptyResponse', value.empty_response?.max_retries, 0, Math.min(30, value.max_additional_attempts)],
    ['sameAccountRetries', value.same_account_retries, 0, value.empty_response?.max_retries],
    ['maxTotalWait', value.max_total_retry_wait_ms, 0, maxDurationMS],
    ...waitFields.map(field => [field.label, value[field.key], 0, maxDurationMS] as [string, unknown, number, number])
  ]
  for (const [field, amount, min, max] of ranges) {
    if (typeof amount !== 'number' || !Number.isSafeInteger(amount) || amount < min || amount > max) {
      return t(`${key}.integerRange`, { field: t(`${key}.${field}`), min, max })
    }
  }
  if (
    [value.enabled, value.azure400_strip_input_ids, value.azure400?.enabled, value.empty_response?.enabled, value.respect_retry_after].some(item => typeof item !== 'boolean') ||
    !emptyStrategies.includes(value.empty_retry_strategy) ||
    !rateLimitStrategies.includes(value.rate_limit_retry_strategy) ||
    ![value.scope.group_ids, value.scope.account_ids].every(ids => Array.isArray(ids) && ids.every(id => Number.isSafeInteger(id) && id > 0)) ||
    !Array.isArray(value.scope.models) || value.scope.models.some(model => typeof model !== 'string' || !model.trim())
  ) return t(`${key}.invalidResponse`)
  return ''
}

async function loadConfig() {
  configRequest?.abort()
  const controller = new AbortController()
  configRequest = controller
  configLoading.value = true
  configError.value = ''
  try {
    const snapshot = await serverAutoRetryAPI.getConfig(controller.signal)
    const invalid = validationError(snapshot)
    if (invalid) throw new Error(invalid)
    if (!controller.signal.aborted && !disposed) config.value = cloneConfig(snapshot)
  } catch (error) {
    if (!controller.signal.aborted && !disposed) configError.value = errorMessage(error, 'configLoadError')
  } finally {
    if (configRequest === controller && !disposed) configLoading.value = false
  }
}

async function loadEvents() {
  eventsRequest?.abort()
  const controller = new AbortController()
  eventsRequest = controller
  eventsLoading.value = true
  eventsError.value = ''
  try {
    const result = await serverAutoRetryAPI.listEvents({
      page: page.value, page_size: pageSize.value,
      ...(appliedRequestId.value ? { request_id: appliedRequestId.value } : {})
    }, controller.signal)
    if (controller.signal.aborted || disposed) return
    if (!result || !Array.isArray(result.items) || !Number.isSafeInteger(result.total) || result.total < 0 ||
      !Number.isSafeInteger(result.page) || result.page < 1 || !Number.isSafeInteger(result.page_size) || result.page_size < 1) {
      throw new Error(t(`${key}.invalidResponse`))
    }
    events.value = result.items.filter((item): item is ServerAutoRetryEvent => !!item && typeof item === 'object')
    total.value = result.total
    page.value = result.page
    pageSize.value = result.page_size
    const lastPage = Math.max(1, Math.ceil(result.total / result.page_size))
    if (page.value > lastPage) {
      page.value = lastPage
      void loadEvents()
    }
  } catch (error) {
    if (!controller.signal.aborted && !disposed) eventsError.value = errorMessage(error, 'eventsLoadError')
  } finally {
    if (eventsRequest === controller && !disposed) eventsLoading.value = false
  }
}

async function loadGroups() {
  if (!canReadGroups.value || groupsLoading.value) return
  groupsLoading.value = true
  groupsError.value = ''
  try {
    const groups = await groupsAPI.getAll('openai')
    if (disposed) return
    groupOptions.value = groups.filter(group => group.platform === 'openai').map(group => ({ value: group.id, label: `${group.name} (#${group.id})` }))
    for (const option of groupOptions.value) groupLabels.value[Number(option.value)] = option.label
  } catch (error) {
    if (!disposed) groupsError.value = errorMessage(error, 'scopeLoadError', { name: t(`${key}.groups`) })
  } finally {
    if (!disposed) groupsLoading.value = false
  }
}

async function searchAccounts(search = '') {
  if (!canReadAccounts.value) return
  accountsRequest?.abort()
  const controller = new AbortController()
  accountsRequest = controller
  accountsLoading.value = true
  accountsError.value = ''
  try {
    const result = await accountsAPI.list(1, 50, { platform: 'openai', lite: 'true', search }, { signal: controller.signal })
    if (controller.signal.aborted || disposed) return
    accountOptions.value = result.items.filter(account => account.platform === 'openai').map(account => ({ value: account.id, label: `${account.name} (#${account.id})` }))
    for (const option of accountOptions.value) accountLabels.value[Number(option.value)] = option.label
  } catch (error) {
    if (!controller.signal.aborted && !disposed) accountsError.value = errorMessage(error, 'scopeLoadError', { name: t(`${key}.accounts`) })
  } finally {
    if (accountsRequest === controller && !disposed) accountsLoading.value = false
  }
}

function addScope(kind: 'group_ids' | 'account_ids', value: SelectOption['value']) {
  if (saving.value || typeof value !== 'number' || !Number.isSafeInteger(value) || value < 1) return
  if (!draft.value.scope[kind].includes(value)) draft.value.scope[kind].push(value)
}

function addScopeId(kind: 'group_ids' | 'account_ids') {
  if (saving.value) return
  const input = kind === 'group_ids' ? groupIdInput : accountIdInput
  const id = input.value
  if (typeof id !== 'number' || !Number.isSafeInteger(id) || id <= 0) {
    appStore.showError(t(`${key}.invalidScopeId`, { field: t(`${key}.${kind === 'group_ids' ? 'groupId' : 'accountId'}`) }))
    return
  }
  addScope(kind, id)
  input.value = ''
}

function removeScope(kind: 'group_ids' | 'account_ids', id: number) {
  if (!saving.value) draft.value.scope[kind] = draft.value.scope[kind].filter(value => value !== id)
}

function openSettings() {
  if (!config.value || configLoading.value || !authStore.canAdmin('system.settings.manage')) return
  draft.value = cloneConfig(config.value)
  modelsInput.value = draft.value.scope.models.join(', ')
  groupIdInput.value = ''
  accountIdInput.value = ''
  saveError.value = ''
  settingsOpen.value = true
  void loadGroups()
  void searchAccounts()
}

function closeSettings() {
  if (!saving.value) settingsOpen.value = false
}

function restoreRecommended() {
  if (saving.value) return
  draft.value = recommendedServerAutoRetryConfig()
  modelsInput.value = ''
  groupIdInput.value = ''
  accountIdInput.value = ''
  saveError.value = ''
}

async function saveConfig() {
  if (saving.value || !authStore.canAdmin('system.settings.manage')) return
  const payload = cloneConfig(draft.value)
  payload.scope.models = [...new Set(modelsInput.value.split(/[,\n\uFF0C]/).map(model => model.trim()).filter(Boolean))]
  saveError.value = validationError(payload)
  if (saveError.value) return
  saving.value = true
  try {
    const saved = await serverAutoRetryAPI.updateConfig(payload)
    const invalid = validationError(saved)
    if (invalid) throw new Error(invalid)
    if (disposed) return
    config.value = cloneConfig(saved)
    configError.value = ''
    settingsOpen.value = false
    appStore.showSuccess(t(`${key}.saveSuccess`))
    void loadEvents()
  } catch (error) {
    if (!disposed) saveError.value = errorMessage(error, 'saveError')
  } finally {
    if (!disposed) saving.value = false
  }
}

function refresh() {
  if (!saving.value) void loadConfig()
  void loadEvents()
}

function changePage(value: number) {
  page.value = value
  void loadEvents()
}

function changePageSize(value: number) {
  pageSize.value = value
  page.value = 1
  void loadEvents()
}

async function filterRequest(value = requestId.value) {
  const request = value.trim()
  if (request === route.query.request_id || (!request && !route.query.request_id)) {
    requestId.value = request
    page.value = 1
    void loadEvents()
    return
  }
  await router.replace({ query: { ...route.query, request_id: request || undefined } })
}

watch(() => route.query.request_id, value => {
  requestId.value = typeof value === 'string' ? value : ''
  page.value = 1
  void loadEvents()
}, { immediate: true })

onMounted(() => { void loadConfig() })
onBeforeUnmount(() => {
  disposed = true
  configRequest?.abort()
  eventsRequest?.abort()
  accountsRequest?.abort()
})
</script>

<template>
  <AppLayout>
    <div class="retry-page min-w-0 space-y-5">
      <header class="flex flex-wrap items-center justify-between gap-3">
        <div class="flex min-w-0 flex-wrap items-center gap-3">
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t(`${key}.title`) }}</h2>
          <span v-if="config && !configError" class="text-xs font-medium" :class="config.enabled ? 'text-emerald-700 dark:text-emerald-400' : 'text-gray-500 dark:text-gray-400'">
            {{ t(`${key}.${config.enabled ? 'enabled' : 'disabled'}`) }}
          </span>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" :disabled="configLoading || eventsLoading || saving" @click="refresh">
            <Icon name="refresh" :class="(configLoading || eventsLoading) && 'animate-spin'" />
          </button>
          <button type="button" class="retry-icon btn btn-secondary" :title="t(`${key}.settings`)" :aria-label="t(`${key}.settings`)" :disabled="!config || configLoading || saving" @click="openSettings">
            <Icon name="cog" />
          </button>
        </div>
      </header>

      <div v-if="configLoading" class="flex items-center gap-2 py-5 text-sm text-gray-500" role="status">
        <Icon name="refresh" class="animate-spin" />{{ t('common.loading') }}
      </div>
      <div v-else-if="configError" class="flex flex-wrap items-center gap-3 py-4 text-sm text-red-600 dark:text-red-400" role="alert">
        <span class="min-w-0 break-words">{{ configError }}</span>
        <button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadConfig"><Icon name="refresh" /></button>
      </div>
      <template v-else-if="config">
        <section class="space-y-3 border-y border-gray-200 py-4 text-sm dark:border-dark-700">
          <dl class="grid min-w-0 grid-cols-1 gap-x-6 gap-y-3 sm:grid-cols-2 xl:grid-cols-4">
            <div><dt class="retry-label">{{ t(`${key}.scope`) }}</dt><dd class="break-words">{{ t(`${key}.ordinaryOpenAIResponses`) }}</dd></div>
            <div><dt class="retry-label">{{ t(`${key}.maxAdditionalAttempts`) }}</dt><dd>{{ config.max_additional_attempts }}</dd></div>
            <div><dt class="retry-label">{{ t(`${key}.maxTotalWait`) }}</dt><dd>{{ config.max_total_retry_wait_ms }}</dd></div>
            <div><dt class="retry-label">{{ t(`${key}.schemaVersion`) }}</dt><dd>{{ config.schema_version }}</dd></div>
            <div v-for="kind in (['groups', 'accounts', 'models'] as const)" :key="kind" class="min-w-0"><dt class="retry-label">{{ t(`${key}.${kind}`) }}</dt><dd class="break-words">{{ scopeLabel(kind) }}</dd></div>
            <div><dt class="retry-label">{{ t(`${key}.respectRetryAfter`) }}</dt><dd>{{ booleanLabel(config.respect_retry_after) }}</dd></div>
          </dl>
          <div class="flex flex-wrap gap-x-5 gap-y-1 text-xs text-gray-500 dark:text-gray-400">
            <span>{{ t(`${key}.backoffCap`) }}: {{ config.backoff_cap_ms }}</span>
            <span>{{ t(`${key}.jitter`) }}: {{ config.jitter_ms }}</span>
          </div>
        </section>
        <section class="divide-y divide-gray-200 dark:divide-dark-700">
          <div v-for="rule in rules" :key="rule.name" class="grid min-w-0 gap-2 py-3 sm:grid-cols-[9rem_minmax(0,1fr)_8rem] sm:gap-4">
            <div><h3 class="text-sm font-semibold">{{ t(`${key}.${rule.name}`) }}</h3><span class="text-xs" :class="config.enabled && rule.enabled ? 'text-emerald-700 dark:text-emerald-400' : 'text-gray-500'">{{ t(`${key}.${config.enabled && rule.enabled ? 'enabled' : 'disabled'}`) }}</span></div>
            <div class="min-w-0 space-y-1 text-xs text-gray-500 dark:text-gray-400">
              <p class="break-words">{{ t(`${key}.${rule.description}`) }}</p>
              <template v-if="rule.name === 'emptyResponse'">
                <p>{{ t(`${key}.emptyStrategy`) }}: {{ enumLabel('strategies', config.empty_retry_strategy) }}; {{ t(`${key}.sameAccountRetries`) }}: {{ config.same_account_retries }}</p>
                <p>{{ t(`${key}.rateLimitStrategy`) }}: {{ enumLabel('strategies', config.rate_limit_retry_strategy) }}</p>
                <p>{{ t(`${key}.emptyBackoff`) }}: {{ config.empty_backoff_base_ms }}; {{ t(`${key}.rateLimitBackoff`) }}: {{ config.rate_limit_backoff_base_ms }}</p>
              </template>
            </div>
            <div class="text-sm sm:text-right">{{ t(`${key}.attemptsValue`, { count: rule.max_retries }) }}</div>
          </div>
        </section>
      </template>

      <section class="min-w-0 space-y-3">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h3 class="text-sm font-semibold">{{ t(`${key}.recentEvents`) }}</h3>
          <form class="flex w-full items-center gap-2 sm:w-auto" @submit.prevent="filterRequest()">
            <input v-model="requestId" class="input min-w-0 sm:w-72" :aria-label="t(`${key}.requestId`)" :placeholder="t(`${key}.requestId`)" />
            <button type="submit" class="retry-icon btn btn-secondary" :title="t('common.search')" :aria-label="t('common.search')"><Icon name="search" /></button>
            <button v-if="requestId || route.query.request_id" type="button" class="retry-icon btn btn-secondary" :title="t('common.reset')" :aria-label="t('common.reset')" @click="filterRequest('')"><Icon name="x" /></button>
            <button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" :disabled="eventsLoading" @click="loadEvents"><Icon name="refresh" :class="eventsLoading && 'animate-spin'" /></button>
          </form>
        </div>
        <div v-if="eventsLoading" class="flex items-center justify-center gap-2 py-10 text-sm text-gray-500" role="status"><Icon name="refresh" class="animate-spin" />{{ t('common.loading') }}</div>
        <div v-else-if="eventsError" class="flex flex-wrap items-center gap-3 py-6 text-sm text-red-600 dark:text-red-400" role="alert"><span class="min-w-0 break-words">{{ eventsError }}</span><button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadEvents"><Icon name="refresh" /></button></div>
        <p v-else-if="!events.length" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ t(`${key}.noEvents`) }}</p>
        <div v-else class="min-w-0 overflow-x-auto border-y border-gray-200 dark:border-dark-700">
          <table class="retry-table w-full table-fixed text-left text-xs">
            <thead class="bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-gray-300"><tr>
              <th class="w-40">{{ t(`${key}.timestamp`) }}</th><th class="w-56">{{ t(`${key}.requestId`) }}</th><th class="w-44">{{ t(`${key}.reason`) }}</th>
              <th class="w-36">{{ t(`${key}.httpSemantic`) }}</th><th class="w-20">{{ t(`${key}.account`) }}</th><th class="w-36">{{ t(`${key}.counts`) }}</th>
              <th class="w-28">{{ t(`${key}.wait`) }}</th><th class="w-28">{{ t(`${key}.evidence`) }}</th><th class="w-32">{{ t(`${key}.usage`) }}</th><th class="w-48">{{ t(`${key}.stopReason`) }} / {{ t(`${key}.outcome`) }}</th>
            </tr></thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700"><tr v-for="(event, index) in events" :key="`${event.id ?? 'event'}-${index}`" class="align-top hover:bg-gray-100/70 dark:hover:bg-dark-800/70">
              <td>{{ timeLabel(event.timestamp) }}</td>
              <td><router-link v-if="event.request_id" :to="{ path: '/admin/server-auto-retry', query: { request_id: event.request_id } }" class="break-all font-mono text-primary-600 hover:underline dark:text-primary-400" :title="event.request_id">{{ event.request_id }}</router-link><span v-else>{{ t(`${key}.unknown`) }}</span><div v-if="event.upstream_request_id" class="mt-1 break-all text-gray-500"><span>{{ t(`${key}.upstreamRequestId`) }}: </span><span class="font-mono">{{ event.upstream_request_id }}</span></div></td>
              <td>{{ enumLabel('reasons', event.reason) }}</td>
              <td><div>{{ t(`${key}.rawHTTP`) }}: {{ numberLabel(event.raw_http_status) }}</div><div>{{ t(`${key}.semantic`) }}: {{ statusLabel(event.semantic_status) }}</div></td>
              <td>{{ numberLabel(event.account_id) }}</td>
              <td><div>{{ t(`${key}.dispatch`) }}: {{ numberLabel(event.dispatch_count) }}</div><div>{{ t(`${key}.retry`) }}: {{ numberLabel(event.retry_count) }}</div><div>{{ t(`${key}.switch`) }}: {{ numberLabel(event.switch_count) }}</div></td>
              <td>{{ typeof event.wait_ms === 'number' ? t(`${key}.waitValue`, { value: numberLabel(event.wait_ms) }) : t(`${key}.unknown`) }}</td>
              <td><div :title="t(`${key}.output`)">{{ booleanLabel(event.has_semantic_output) }}</div><div :title="t(`${key}.committed`)">{{ booleanLabel(event.response_committed) }}</div></td>
              <td>{{ enumLabel('usageEvidence', event.usage_evidence) }}</td>
              <td><div class="break-words" :title="event.stop_reason || undefined">{{ enumLabel('stopReasons', event.stop_reason) }}</div><div class="mt-1 font-medium">{{ enumLabel('outcomes', event.outcome) }}</div></td>
            </tr></tbody>
          </table>
        </div>
        <Pagination v-if="total > 0 && !eventsError" :total="total" :page="page" :page-size="pageSize" :show-page-size-selector="false" @update:page="changePage" @update:page-size="changePageSize" />
      </section>
    </div>
    <BaseDialog :show="settingsOpen" :title="t(`${key}.settings`)" width="wide" :close-on-escape="!saving" :show-close-button="!saving" @close="closeSettings">
      <form id="server-auto-retry-settings" class="min-w-0 space-y-5" @submit.prevent="saveConfig">
        <fieldset :disabled="saving" class="min-w-0 space-y-5">
          <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200 pb-3 dark:border-dark-700">
            <label for="retry-enabled" class="text-sm font-medium">{{ t(`${key}.masterSwitch`) }}</label><Toggle id="retry-enabled" v-model="draft.enabled" :aria-label="t(`${key}.masterSwitch`)" />
          </div>
          <div class="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-3">
            <label class="block min-w-0"><span class="input-label">{{ t(`${key}.maxAdditionalAttempts`) }}</span><input v-model.number="draft.max_additional_attempts" class="input" type="number" min="0" max="30" step="1" required /></label>
            <label class="block min-w-0"><span class="input-label">{{ t(`${key}.maxTotalWait`) }}</span><input v-model.number="draft.max_total_retry_wait_ms" class="input" type="number" min="0" :max="maxDurationMS" step="1" required /></label>
            <label class="block min-w-0"><span class="input-label">{{ t(`${key}.schemaVersion`) }}</span><input :value="draft.schema_version" class="input" type="number" readonly /></label>
          </div>
          <section class="min-w-0 space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
            <h4 class="text-sm font-semibold">{{ t(`${key}.scope`) }}</h4>
            <p class="text-xs text-gray-500">{{ t(`${key}.ordinaryOpenAIResponses`) }}. {{ t(`${key}.emptyScope`) }}</p>
            <div class="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2">
              <div class="min-w-0 space-y-2">
                <label for="retry-groups" class="input-label">{{ t(`${key}.groups`) }}</label>
                <Select id="retry-groups" :model-value="null" :options="availableGroups" :placeholder="t(`${key}.addGroup`)" :aria-label="t(`${key}.groups`)" :disabled="saving || !canReadGroups" :loading="groupsLoading" searchable @update:model-value="addScope('group_ids', $event)" />
                <p v-if="!canReadGroups" class="text-xs text-gray-500">{{ t(`${key}.scopePermission`, { name: t(`${key}.groups`) }) }}</p>
                <div v-if="groupsError" class="flex items-center gap-2 text-xs text-red-600" role="alert"><span class="break-words">{{ groupsError }}</span><button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadGroups"><Icon name="refresh" size="sm" /></button></div>
                <div v-if="!canReadGroups || groupsError" class="min-w-0">
                  <label for="retry-group-id" class="input-label">{{ t(`${key}.groupId`) }}</label>
                  <div class="flex min-w-0 items-center gap-2">
                    <input id="retry-group-id" v-model.number="groupIdInput" class="input min-w-0 flex-1" type="number" min="1" :max="Number.MAX_SAFE_INTEGER" step="1" :disabled="saving" @keydown.enter.prevent="addScopeId('group_ids')" />
                    <button type="button" class="retry-icon btn btn-secondary" :title="t(`${key}.addGroupId`)" :aria-label="t(`${key}.addGroupId`)" :disabled="saving" @click="addScopeId('group_ids')"><Icon name="plus" size="sm" /></button>
                  </div>
                </div>
                <div class="flex flex-wrap gap-2"><span v-for="id in draft.scope.group_ids" :key="id" class="retry-chip"><span class="min-w-0 break-all">{{ groupLabels[id] || `#${id}` }}</span><button type="button" :title="t(`${key}.removeScope`, { name: groupLabels[id] || `#${id}` })" :aria-label="t(`${key}.removeScope`, { name: groupLabels[id] || `#${id}` })" @click="removeScope('group_ids', id)"><Icon name="x" size="sm" /></button></span></div>
              </div>
              <div class="min-w-0 space-y-2">
                <label for="retry-accounts" class="input-label">{{ t(`${key}.accounts`) }}</label>
                <Select id="retry-accounts" :model-value="null" :options="availableAccounts" :placeholder="t(`${key}.addAccount`)" :aria-label="t(`${key}.accounts`)" :disabled="saving || !canReadAccounts" :loading="accountsLoading" searchable remote @search="searchAccounts" @update:model-value="addScope('account_ids', $event)" />
                <p v-if="!canReadAccounts" class="text-xs text-gray-500">{{ t(`${key}.scopePermission`, { name: t(`${key}.accounts`) }) }}</p>
                <div v-if="accountsError" class="flex items-center gap-2 text-xs text-red-600" role="alert"><span class="break-words">{{ accountsError }}</span><button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="searchAccounts()"><Icon name="refresh" size="sm" /></button></div>
                <div v-if="!canReadAccounts || accountsError" class="min-w-0">
                  <label for="retry-account-id" class="input-label">{{ t(`${key}.accountId`) }}</label>
                  <div class="flex min-w-0 items-center gap-2">
                    <input id="retry-account-id" v-model.number="accountIdInput" class="input min-w-0 flex-1" type="number" min="1" :max="Number.MAX_SAFE_INTEGER" step="1" :disabled="saving" @keydown.enter.prevent="addScopeId('account_ids')" />
                    <button type="button" class="retry-icon btn btn-secondary" :title="t(`${key}.addAccountId`)" :aria-label="t(`${key}.addAccountId`)" :disabled="saving" @click="addScopeId('account_ids')"><Icon name="plus" size="sm" /></button>
                  </div>
                </div>
                <div class="flex flex-wrap gap-2"><span v-for="id in draft.scope.account_ids" :key="id" class="retry-chip"><span class="min-w-0 break-all">{{ accountLabels[id] || `#${id}` }}</span><button type="button" :title="t(`${key}.removeScope`, { name: accountLabels[id] || `#${id}` })" :aria-label="t(`${key}.removeScope`, { name: accountLabels[id] || `#${id}` })" @click="removeScope('account_ids', id)"><Icon name="x" size="sm" /></button></span></div>
              </div>
            </div>
            <label class="block min-w-0"><span class="input-label">{{ t(`${key}.models`) }}</span><textarea v-model="modelsInput" class="input min-h-16 resize-y" rows="2" :placeholder="t(`${key}.modelsPlaceholder`)" /></label>
          </section>
          <section class="space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
            <div class="flex items-center justify-between gap-3"><label for="retry-azure" class="text-sm font-semibold">{{ t(`${key}.azure400`) }}</label><Toggle id="retry-azure" v-model="draft.azure400.enabled" :aria-label="t(`${key}.azure400`)" /></div>
            <label class="block"><span class="input-label">{{ t(`${key}.retries`) }}</span><input v-model.number="draft.azure400.max_retries" class="input sm:max-w-48" type="number" min="0" :max="Math.min(1, draft.max_additional_attempts)" step="1" required /></label>
            <div class="flex items-start justify-between gap-3">
              <div class="min-w-0 space-y-1">
                <label for="retry-azure-strip-input-ids" class="text-sm font-medium">{{ t(`${key}.azure400StripInputIds`) }}</label>
                <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${key}.azure400StripInputIdsHelp`) }}</p>
              </div>
              <Toggle id="retry-azure-strip-input-ids" v-model="draft.azure400_strip_input_ids" :aria-label="t(`${key}.azure400StripInputIds`)" />
            </div>
          </section>
          <section class="min-w-0 space-y-3 border-t border-gray-200 pt-4 dark:border-dark-700">
            <div class="flex items-center justify-between gap-3"><label for="retry-empty" class="text-sm font-semibold">{{ t(`${key}.emptyResponse`) }}</label><Toggle id="retry-empty" v-model="draft.empty_response.enabled" :aria-label="t(`${key}.emptyResponse`)" /></div>
            <div class="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2">
              <label class="block min-w-0"><span class="input-label">{{ t(`${key}.retries`) }}</span><input v-model.number="draft.empty_response.max_retries" class="input" type="number" min="0" :max="Math.min(30, draft.max_additional_attempts)" step="1" required /></label>
              <label class="block min-w-0"><span class="input-label">{{ t(`${key}.sameAccountRetries`) }}</span><input v-model.number="draft.same_account_retries" class="input" type="number" min="0" :max="draft.empty_response.max_retries" step="1" required /></label>
              <div class="min-w-0"><label for="retry-empty-strategy" class="input-label">{{ t(`${key}.emptyStrategy`) }}</label><Select id="retry-empty-strategy" v-model="draft.empty_retry_strategy" :options="emptyStrategyOptions" :aria-label="t(`${key}.emptyStrategy`)" :disabled="saving" /></div>
              <div class="min-w-0"><label for="retry-rate-strategy" class="input-label">{{ t(`${key}.rateLimitStrategy`) }}</label><Select id="retry-rate-strategy" v-model="draft.rate_limit_retry_strategy" :options="rateLimitStrategyOptions" :aria-label="t(`${key}.rateLimitStrategy`)" :disabled="saving" /></div>
            </div>
          </section>
          <section class="grid min-w-0 grid-cols-1 gap-4 border-t border-gray-200 pt-4 dark:border-dark-700 sm:grid-cols-2">
            <label v-for="field in waitFields" :key="field.key" class="block min-w-0"><span class="input-label">{{ t(`${key}.${field.label}`) }}</span><input v-model.number="draft[field.key]" class="input" type="number" min="0" :max="maxDurationMS" step="1" required /></label>
            <div class="flex items-center justify-between gap-3 sm:col-span-2"><label for="retry-after" class="text-sm">{{ t(`${key}.respectRetryAfter`) }}</label><Toggle id="retry-after" v-model="draft.respect_retry_after" :aria-label="t(`${key}.respectRetryAfter`)" /></div>
          </section>
        </fieldset>
        <p v-if="saveError" class="break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ saveError }}</p>
      </form>
      <template #footer>
        <div class="flex w-full flex-wrap items-center justify-between gap-3">
          <button type="button" class="btn btn-secondary text-sm" :disabled="saving" @click="restoreRecommended"><Icon name="refresh" size="sm" class="mr-2 shrink-0" />{{ t(`${key}.restore`) }}</button>
          <div class="flex items-center gap-2"><button type="button" class="btn btn-secondary" :disabled="saving" @click="closeSettings">{{ t('common.cancel') }}</button><button type="submit" form="server-auto-retry-settings" class="btn btn-primary" :disabled="saving"><Icon :name="saving ? 'refresh' : 'check'" size="sm" class="mr-2" :class="saving && 'animate-spin'" />{{ t(saving ? 'common.saving' : 'common.save') }}</button></div>
        </div>
      </template>
    </BaseDialog>
  </AppLayout>
</template>

<style scoped>
.retry-icon {
  width: 2.25rem;
  height: 2.25rem;
  flex-shrink: 0;
  padding: 0;
}
.retry-label {
  margin-bottom: 0.25rem;
  font-size: 0.75rem;
  color: rgb(107 114 128);
}
.retry-table {
  min-width: 95rem;
}
.retry-table th,
.retry-table td {
  padding: 0.625rem 0.75rem;
  overflow-wrap: anywhere;
}
.retry-chip {
  display: inline-flex;
  max-width: 100%;
  align-items: center;
  gap: 0.375rem;
  padding: 0.25rem 0.375rem;
  border: 1px solid rgb(156 163 175 / 0.4);
  border-radius: 0.25rem;
  font-size: 0.75rem;
}
.retry-chip button {
  flex-shrink: 0;
}
</style>
