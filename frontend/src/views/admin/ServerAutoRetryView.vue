<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
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
const pageSizePreferenceKey = 'admin.serverAutoRetry.requestsPerPage'
const columnsPreferenceKey = 'admin.serverAutoRetry.visibleColumns'
const requestPageSizes = [10, 20, 40]
const requestPageSizeOptions = requestPageSizes.map(value => ({ value, label: String(value) }))
const informationColumns = [
  { id: 'timestamp', label: 'timestamp', width: 10 },
  { id: 'user', label: 'requestUser', width: 14 },
  { id: 'accountName', label: 'accountName', width: 11 },
  { id: 'accountId', label: 'accountId', width: 6 },
  { id: 'reason', label: 'reason', width: 11 },
  { id: 'httpSemantic', label: 'httpSemantic', width: 9 },
  { id: 'counts', label: 'counts', width: 9 },
  { id: 'wait', label: 'wait', width: 7 },
  { id: 'evidence', label: 'evidence', width: 9 },
  { id: 'usage', label: 'usage', width: 8 },
  { id: 'result', label: 'finalResult', width: 12 }
] as const
type InformationColumn = typeof informationColumns[number]['id']

function readPreference(storageKey: string): unknown {
  try {
    const value = localStorage.getItem(storageKey)
    return value === null ? undefined : JSON.parse(value)
  } catch {
    return undefined
  }
}

function writePreference(storageKey: string, value: unknown) {
  try {
    localStorage.setItem(storageKey, JSON.stringify(value))
  } catch {
    // Storage may be unavailable; the current session remains usable.
  }
}

const savedPageSize = readPreference(pageSizePreferenceKey)
const savedColumns = readPreference(columnsPreferenceKey)
const page = ref(1)
const pageSize = ref(typeof savedPageSize === 'number' && requestPageSizes.includes(savedPageSize) ? savedPageSize : 20)
const visibleColumns = ref<InformationColumn[]>(Array.isArray(savedColumns)
  ? informationColumns.filter(column => savedColumns.includes(column.id)).map(column => column.id)
  : informationColumns.map(column => column.id))
const displayedColumns = computed(() => informationColumns.filter(column => visibleColumns.value.includes(column.id)))
const tableWidth = computed(() => `${17 + displayedColumns.value.reduce((width, column) => width + column.width, 0)}rem`)
const columnsOpen = ref(false)
const columnsButton = ref<HTMLButtonElement | null>(null)
const columnsMenu = ref<HTMLDivElement | null>(null)
const columnsMenuStyle = ref({ left: '0px', top: '0px', width: '288px', maxHeight: '448px' })
const expandedRequests = ref(new Set<string>())
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

interface RequestGroup {
  key: string
  requestId: string
  events: ServerAutoRetryEvent[]
  summary: ServerAutoRetryEvent
  complete: boolean
  accounts: Array<{ id: number | null; name: string | null }>
}

function knownText(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() ? value.trim() : undefined
}

function knownId(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value > 0
}

function isFinalEvent(event: ServerAutoRetryEvent): boolean {
  const eventType = knownText(event.event_type)
  if (eventType) return eventType === 'final' || eventType === 'server_auto_retry.final'
  return ['success', 'succeeded', 'failed', 'failure', 'stopped'].includes(event.outcome ?? '')
}

function cumulativeMaximum(items: ServerAutoRetryEvent[], field: 'dispatch_count' | 'retry_count' | 'switch_count' | 'wait_ms'): number | undefined {
  return items.reduce<number | undefined>((maximum, event) => {
    const value = event[field]
    return typeof value === 'number' && Number.isFinite(value) && value >= 0
      ? Math.max(maximum ?? 0, value) : maximum
  }, undefined)
}

function summarizeRequest(items: ServerAutoRetryEvent[]): ServerAutoRetryEvent {
  const latestFirst = [...items].reverse()
  const latest = latestFirst.find(isFinalEvent) ?? latestFirst[0]!
  const userId = latestFirst.find(event => knownId(event.user_id))?.user_id
  const userEvents = latestFirst.filter(event => !knownId(event.user_id) || event.user_id === userId)
  const accountId = latestFirst.find(event => knownId(event.account_id))?.account_id
  const accountEvents = latestFirst.filter(event => !knownId(event.account_id) || event.account_id === accountId)
  return {
    ...latest,
    user_id: userId,
    user_name: userEvents.map(event => knownText(event.user_name)).find(Boolean),
    user_email: userEvents.map(event => knownText(event.user_email)).find(Boolean),
    account_id: accountId,
    account_name: accountEvents.map(event => knownText(event.account_name)).find(Boolean),
    dispatch_count: cumulativeMaximum(items, 'dispatch_count'),
    retry_count: cumulativeMaximum(items, 'retry_count'),
    switch_count: cumulativeMaximum(items, 'switch_count'),
    wait_ms: cumulativeMaximum(items, 'wait_ms')
  }
}

const requestGroups = computed<RequestGroup[]>(() => {
  const groups = new Map<string, { requestId: string; items: Array<{ event: ServerAutoRetryEvent; index: number }> }>()
  events.value.forEach((event, index) => {
    const requestId = knownText(event.request_id) ?? ''
    const groupKey = requestId ? `request:${requestId}` : `event:${event.id ?? 'missing'}:${index}`
    let group = groups.get(groupKey)
    if (!group) {
      group = { requestId, items: [] }
      groups.set(groupKey, group)
    }
    group.items.push({ event, index })
  })
  return [...groups].map(([groupKey, group]) => {
    const items = group.items.sort((a, b) => {
      const aTime = Date.parse(a.event.timestamp ?? '')
      const bTime = Date.parse(b.event.timestamp ?? '')
      if (Number.isFinite(aTime) && Number.isFinite(bTime) && aTime !== bTime) return aTime - bTime
      if (a.event.id != null && b.event.id != null) {
        const idOrder = String(a.event.id).localeCompare(String(b.event.id), 'en', { numeric: true })
        if (idOrder) return idOrder
      }
      return Number(isFinalEvent(a.event)) - Number(isFinalEvent(b.event)) || a.index - b.index
    }).map(item => item.event)
    const accounts = new Map<string, { id: number | null; name: string | null }>()
    for (const event of items) {
      if (!knownId(event.account_id)) continue
      const id = event.account_id
      const name = knownText(event.account_name) ?? null
      const accountKey = `id:${id}`
      const previous = accounts.get(accountKey)
      accounts.set(accountKey, { id, name: name ?? previous?.name ?? null })
    }
    for (const event of items) {
      const name = knownText(event.account_name)
      if (knownId(event.account_id) || !name || [...accounts.values()].some(account => account.name === name)) continue
      accounts.set(`name:${name}`, { id: null, name })
    }
    return {
      key: groupKey, requestId: group.requestId, events: items,
      summary: summarizeRequest(items), complete: items.some(isFinalEvent), accounts: [...accounts.values()]
    }
  })
})

function toggleRequest(groupKey: string) {
  const next = new Set(expandedRequests.value)
  if (next.has(groupKey)) next.delete(groupKey)
  else next.add(groupKey)
  expandedRequests.value = next
}

function detailsId(groupKey: string): string {
  return `retry-details-${encodeURIComponent(groupKey)}`
}

function eventLabel(event: ServerAutoRetryEvent): string {
  if (isFinalEvent(event)) return t(`${key}.finalEvent`)
  return knownText(event.event_type) ? enumLabel('eventTypes', event.event_type) : t(`${key}.recordedEvent`)
}

function accountListLabel(group: RequestGroup): string {
  return group.accounts.map(account => `${account.name ?? t(`${key}.unknown`)}${account.id !== null ? ` (#${account.id})` : ''}`).join(', ')
}

function toggleColumn(column: InformationColumn) {
  visibleColumns.value = visibleColumns.value.includes(column)
    ? visibleColumns.value.filter(value => value !== column)
    : [...visibleColumns.value, column]
  writePreference(columnsPreferenceKey, visibleColumns.value)
}

function positionColumnsMenu() {
  if (!columnsOpen.value || !columnsButton.value) return
  const rect = columnsButton.value.getBoundingClientRect()
  if (rect.bottom < 0 || rect.top > window.innerHeight) {
    columnsOpen.value = false
    return
  }
  const width = Math.min(288, window.innerWidth - 16)
  const below = Math.max(0, window.innerHeight - rect.bottom - 16)
  const above = Math.max(0, rect.top - 16)
  const openAbove = below < 320 && above > below
  const height = Math.min(448, openAbove ? above : below)
  columnsMenuStyle.value = {
    left: `${Math.max(8, Math.min(rect.left, window.innerWidth - width - 8))}px`,
    top: `${openAbove ? Math.max(8, rect.top - height - 8) : rect.bottom + 8}px`,
    width: `${width}px`, maxHeight: `${height}px`
  }
}

async function toggleColumnsMenu() {
  columnsOpen.value = !columnsOpen.value
  if (!columnsOpen.value) return
  positionColumnsMenu()
  await nextTick()
  columnsMenu.value?.querySelector<HTMLInputElement>('input:not(:disabled)')?.focus()
}

function onColumnsOutside(event: Event) {
  if (!columnsOpen.value || !(event.target instanceof Node)) return
  if (!columnsButton.value?.contains(event.target) && !columnsMenu.value?.contains(event.target)) columnsOpen.value = false
}

function onColumnsKeydown(event: KeyboardEvent) {
  if (event.key !== 'Escape' || !columnsOpen.value) return
  event.preventDefault()
  columnsOpen.value = false
  columnsButton.value?.focus()
}

function onColumnsFocusout(event: FocusEvent) {
  if (!event.relatedTarget) return
  if (event.relatedTarget instanceof Node &&
    (columnsMenu.value?.contains(event.relatedTarget) || columnsButton.value?.contains(event.relatedTarget))) return
  columnsOpen.value = false
}

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
      page: page.value, page_size: pageSize.value, grouped: true,
      ...(appliedRequestId.value ? { request_id: appliedRequestId.value } : {})
    }, controller.signal)
    if (controller.signal.aborted || disposed) return
    if (!result || !Array.isArray(result.items) || !Number.isSafeInteger(result.total) || result.total < 0 ||
      !Number.isSafeInteger(result.page) || result.page < 1 || !requestPageSizes.includes(result.page_size)) {
      throw new Error(t(`${key}.invalidResponse`))
    }
    events.value = result.items.filter((item): item is ServerAutoRetryEvent => !!item && typeof item === 'object')
    const groupKeys = new Set(requestGroups.value.map(group => group.key))
    expandedRequests.value = new Set([...expandedRequests.value].filter(groupKey => groupKeys.has(groupKey)))
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
  expandedRequests.value = new Set()
  page.value = value
  void loadEvents()
}

function changePageSize(value: SelectOption['value']) {
  if (typeof value !== 'number' || !requestPageSizes.includes(value) || value === pageSize.value) return
  pageSize.value = value
  writePreference(pageSizePreferenceKey, value)
  expandedRequests.value = new Set()
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
  expandedRequests.value = new Set()
  requestId.value = typeof value === 'string' ? value : ''
  page.value = 1
  void loadEvents()
}, { immediate: true })

onMounted(() => {
  void loadConfig()
  document.addEventListener('pointerdown', onColumnsOutside)
  document.addEventListener('keydown', onColumnsKeydown)
  window.addEventListener('resize', positionColumnsMenu)
  window.addEventListener('scroll', positionColumnsMenu, true)
})
onBeforeUnmount(() => {
  disposed = true
  configRequest?.abort()
  eventsRequest?.abort()
  accountsRequest?.abort()
  document.removeEventListener('pointerdown', onColumnsOutside)
  document.removeEventListener('keydown', onColumnsKeydown)
  window.removeEventListener('resize', positionColumnsMenu)
  window.removeEventListener('scroll', positionColumnsMenu, true)
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
          <form class="retry-filter" @submit.prevent="filterRequest()">
            <button ref="columnsButton" type="button" class="retry-columns-button btn btn-secondary" :title="t(`${key}.columnSettings`)" :aria-label="t(`${key}.columnSettings`)" :aria-expanded="columnsOpen" aria-controls="retry-columns-menu" aria-haspopup="dialog" @click="toggleColumnsMenu">
              <Icon name="cog" size="sm" class="shrink-0" /><span>{{ t(`${key}.columnSettings`) }}</span>
            </button>
            <input v-model="requestId" class="retry-search input min-w-0" :aria-label="t(`${key}.requestId`)" :placeholder="t(`${key}.requestId`)" />
            <div class="retry-filter-actions flex items-center justify-end gap-2">
              <button type="submit" class="retry-icon btn btn-secondary" :title="t('common.search')" :aria-label="t('common.search')"><Icon name="search" /></button>
              <button v-if="requestId || route.query.request_id" type="button" class="retry-icon btn btn-secondary" :title="t('common.reset')" :aria-label="t('common.reset')" @click="filterRequest('')"><Icon name="x" /></button>
              <button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" :disabled="eventsLoading" @click="loadEvents"><Icon name="refresh" :class="eventsLoading && 'animate-spin'" /></button>
            </div>
          </form>
        </div>
        <Teleport to="body">
          <div v-if="columnsOpen" id="retry-columns-menu" ref="columnsMenu" class="retry-columns-menu border border-gray-200 bg-white p-2 text-sm text-gray-900 shadow-lg dark:border-dark-700 dark:bg-dark-800 dark:text-gray-100" :style="columnsMenuStyle" role="dialog" :aria-label="t(`${key}.columnSettings`)" @focusout="onColumnsFocusout">
            <label v-for="column in informationColumns" :key="column.id" class="flex cursor-pointer items-center gap-3 rounded px-2 py-2 hover:bg-gray-100 dark:hover:bg-dark-700">
              <input type="checkbox" class="h-4 w-4 shrink-0 accent-primary-600" :checked="visibleColumns.includes(column.id)" @change="toggleColumn(column.id)" />
              <span class="min-w-0 break-words">{{ t(`${key}.${column.label}`) }}</span>
            </label>
          </div>
        </Teleport>
        <div v-if="eventsLoading" class="flex items-center justify-center gap-2 py-10 text-sm text-gray-500" role="status"><Icon name="refresh" class="animate-spin" />{{ t('common.loading') }}</div>
        <div v-else-if="eventsError" class="flex flex-wrap items-center gap-3 py-6 text-sm text-red-600 dark:text-red-400" role="alert"><span class="min-w-0 break-words">{{ eventsError }}</span><button type="button" class="retry-icon btn btn-secondary" :title="t('common.refresh')" :aria-label="t('common.refresh')" @click="loadEvents"><Icon name="refresh" /></button></div>
        <p v-else-if="!events.length" class="py-10 text-center text-sm text-gray-500 dark:text-gray-400">{{ t(`${key}.noEvents`) }}</p>
        <div v-else class="min-w-0 overflow-x-auto border-y border-gray-200 dark:border-dark-700">
          <table class="retry-table w-full table-fixed text-left text-xs" :style="{ minWidth: tableWidth }">
            <colgroup><col style="width: 17rem" /><col v-for="column in displayedColumns" :key="column.id" :style="{ width: column.width + 'rem' }" /></colgroup>
            <thead class="bg-gray-100 text-gray-600 dark:bg-dark-800 dark:text-gray-300">
              <tr><th scope="col">{{ t(key + '.requestId') }}</th><th v-for="column in displayedColumns" :key="column.id" scope="col">{{ t(key + '.' + column.label) }}</th></tr>
            </thead>
            <tbody class="divide-y divide-gray-200 dark:divide-dark-700">
              <template v-for="group in requestGroups" :key="group.key">
                <tr class="align-top hover:bg-gray-100/70 dark:hover:bg-dark-800/70">
                  <th scope="row" class="font-normal">
                    <div class="grid grid-cols-[2rem_minmax(0,1fr)] items-start gap-2">
                      <button type="button" class="retry-expand btn btn-secondary" :title="t(key + (expandedRequests.has(group.key) ? '.collapseRequest' : '.expandRequest'), { request: group.requestId || t(key + '.unknown') })" :aria-label="t(key + (expandedRequests.has(group.key) ? '.collapseRequest' : '.expandRequest'), { request: group.requestId || t(key + '.unknown') })" :aria-expanded="expandedRequests.has(group.key)" :aria-controls="detailsId(group.key)" @click="toggleRequest(group.key)">
                        <Icon :name="expandedRequests.has(group.key) ? 'chevronDown' : 'chevronRight'" size="sm" />
                      </button>
                      <div class="min-w-0 space-y-1">
                        <router-link v-if="group.requestId" :to="{ path: '/admin/server-auto-retry', query: { request_id: group.requestId } }" class="break-all font-mono text-primary-600 hover:underline dark:text-primary-400" :title="group.requestId">{{ group.requestId }}</router-link>
                        <span v-else>{{ t(key + '.unknown') }}</span>
                        <div class="font-semibold tabular-nums">{{ t(key + '.cumulativeRetries', { count: numberLabel(group.summary.retry_count) }) }}</div>
                        <div class="text-gray-500 dark:text-gray-400">{{ t(key + '.eventCount', { count: group.events.length }) }} / {{ t(key + (group.complete ? '.finalEvent' : '.partialRequest')) }}</div>
                        <div v-if="group.accounts.length > 1" class="font-medium text-amber-700 dark:text-amber-400" :title="accountListLabel(group)">{{ t(key + '.multipleAccounts', { count: group.accounts.length }) }}</div>
                        <div v-else-if="(group.summary.switch_count ?? 0) > 0" class="font-medium text-amber-700 dark:text-amber-400">{{ t(key + '.accountsSwitched', { count: group.summary.switch_count }) }}</div>
                        <div v-if="group.summary.upstream_request_id" class="break-all text-gray-500 dark:text-gray-400">{{ t(key + '.upstreamRequestId') }}: <span class="font-mono">{{ group.summary.upstream_request_id }}</span></div>
                      </div>
                    </div>
                  </th>
                  <td v-for="column in displayedColumns" :key="column.id">
                    <template v-if="column.id === 'timestamp'">{{ timeLabel(group.summary.timestamp) }}</template>
                    <template v-else-if="column.id === 'user'">
                      <div class="font-medium">{{ knownText(group.summary.user_name) || knownText(group.summary.user_email) || t(key + '.unknown') }}</div>
                      <div v-if="knownText(group.summary.user_name) && knownText(group.summary.user_email)" class="text-gray-500 dark:text-gray-400">{{ group.summary.user_email }}</div>
                      <div class="mt-1 text-gray-500 dark:text-gray-400">{{ t(key + '.userId') }}: {{ knownId(group.summary.user_id) ? numberLabel(group.summary.user_id) : t(key + '.unknown') }}</div>
                    </template>
                    <template v-else-if="column.id === 'accountName'">
                      <div>{{ knownText(group.summary.account_name) || t(key + '.unknown') }}</div>
                      <div v-if="group.accounts.length > 1" class="mt-1 text-gray-500 dark:text-gray-400" :title="accountListLabel(group)">{{ t(key + '.multipleAccounts', { count: group.accounts.length }) }}</div>
                    </template>
                    <template v-else-if="column.id === 'accountId'">{{ knownId(group.summary.account_id) ? numberLabel(group.summary.account_id) : t(key + '.unknown') }}</template>
                    <template v-else-if="column.id === 'reason'">{{ enumLabel('reasons', group.summary.reason) }}</template>
                    <template v-else-if="column.id === 'httpSemantic'"><div>{{ t(key + '.rawHTTP') }}: {{ numberLabel(group.summary.raw_http_status) }}</div><div>{{ t(key + '.semantic') }}: {{ statusLabel(group.summary.semantic_status) }}</div></template>
                    <template v-else-if="column.id === 'counts'"><div>{{ t(key + '.dispatch') }}: {{ numberLabel(group.summary.dispatch_count) }}</div><div class="font-semibold">{{ t(key + '.retry') }}: {{ numberLabel(group.summary.retry_count) }}</div><div>{{ t(key + '.switch') }}: {{ numberLabel(group.summary.switch_count) }}</div></template>
                    <template v-else-if="column.id === 'wait'">{{ typeof group.summary.wait_ms === 'number' ? t(key + '.waitValue', { value: numberLabel(group.summary.wait_ms) }) : t(key + '.unknown') }}</template>
                    <template v-else-if="column.id === 'evidence'"><div>{{ t(key + '.output') }}: {{ booleanLabel(group.summary.has_semantic_output) }}</div><div>{{ t(key + '.committed') }}: {{ booleanLabel(group.summary.response_committed) }}</div></template>
                    <template v-else-if="column.id === 'usage'">{{ enumLabel('usageEvidence', group.summary.usage_evidence) }}</template>
                    <template v-else-if="column.id === 'result'"><div :title="group.summary.stop_reason || undefined">{{ enumLabel('stopReasons', group.summary.stop_reason) }}</div><div class="mt-1 font-medium">{{ enumLabel('outcomes', group.summary.outcome) }}</div></template>
                  </td>
                </tr>
                <tr v-if="expandedRequests.has(group.key)" :id="detailsId(group.key)">
                  <td :colspan="displayedColumns.length + 1" class="bg-gray-50 dark:bg-dark-900">
                    <section :aria-label="t(key + '.requestTimeline', { request: group.requestId || t(key + '.unknown') })" class="retry-timeline">
                      <h4 class="mb-2 text-xs font-semibold">{{ t(key + '.requestTimeline', { request: group.requestId || t(key + '.unknown') }) }}</h4>
                      <ol class="divide-y divide-gray-200 dark:divide-dark-700">
                        <li v-for="(event, index) in group.events" :key="String(event.id ?? 'event') + '-' + index" class="py-3">
                          <div class="mb-3 flex flex-wrap items-center gap-x-4 gap-y-1">
                            <span class="font-semibold">{{ eventLabel(event) }}</span>
                            <span>{{ t(key + '.eventNumber', { count: index + 1 }) }}</span>
                            <span>{{ timeLabel(event.timestamp) }}</span>
                            <span v-if="event.id != null" class="text-gray-500 dark:text-gray-400">{{ t(key + '.eventId') }}: {{ event.id }}</span>
                          </div>
                          <dl class="grid min-w-0 grid-cols-1 gap-x-5 gap-y-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                            <div class="min-w-0"><dt class="retry-label">{{ t(key + '.requestUser') }}</dt><dd class="break-words">{{ knownText(event.user_name) || knownText(event.user_email) || t(key + '.unknown') }}<div v-if="knownText(event.user_name) && knownText(event.user_email)">{{ event.user_email }}</div><div>{{ t(key + '.userId') }}: {{ knownId(event.user_id) ? numberLabel(event.user_id) : t(key + '.unknown') }}</div></dd></div>
                            <div class="min-w-0"><dt class="retry-label">{{ t(key + '.accountName') }}</dt><dd class="break-words">{{ knownText(event.account_name) || t(key + '.unknown') }}<div>{{ t(key + '.accountId') }}: {{ knownId(event.account_id) ? numberLabel(event.account_id) : t(key + '.unknown') }}</div></dd></div>
                            <div><dt class="retry-label">{{ t(key + '.reason') }}</dt><dd>{{ enumLabel('reasons', event.reason) }}</dd></div>
                            <div><dt class="retry-label">{{ t(key + '.httpSemantic') }}</dt><dd>{{ t(key + '.rawHTTP') }}: {{ numberLabel(event.raw_http_status) }}<div>{{ t(key + '.semantic') }}: {{ statusLabel(event.semantic_status) }}</div></dd></div>
                            <div><dt class="retry-label">{{ t(key + '.counts') }}</dt><dd>{{ t(key + '.dispatch') }}: {{ numberLabel(event.dispatch_count) }}<div>{{ t(key + '.retry') }}: {{ numberLabel(event.retry_count) }}</div><div>{{ t(key + '.switch') }}: {{ numberLabel(event.switch_count) }}</div></dd></div>
                            <div><dt class="retry-label">{{ t(key + '.wait') }}</dt><dd>{{ typeof event.wait_ms === 'number' ? t(key + '.waitValue', { value: numberLabel(event.wait_ms) }) : t(key + '.unknown') }}</dd></div>
                            <div><dt class="retry-label">{{ t(key + '.evidence') }}</dt><dd>{{ t(key + '.output') }}: {{ booleanLabel(event.has_semantic_output) }}<div>{{ t(key + '.committed') }}: {{ booleanLabel(event.response_committed) }}</div></dd></div>
                            <div><dt class="retry-label">{{ t(key + '.usage') }}</dt><dd>{{ enumLabel('usageEvidence', event.usage_evidence) }}</dd></div>
                            <div><dt class="retry-label">{{ t(key + '.stopReason') }}</dt><dd>{{ enumLabel('stopReasons', event.stop_reason) }}</dd></div>
                            <div><dt class="retry-label">{{ t(key + '.outcome') }}</dt><dd class="font-medium">{{ enumLabel('outcomes', event.outcome) }}</dd></div>
                            <div class="min-w-0"><dt class="retry-label">{{ t(key + '.upstreamRequestId') }}</dt><dd class="break-all font-mono">{{ knownText(event.upstream_request_id) || t(key + '.unknown') }}</dd></div>
                          </dl>
                        </li>
                      </ol>
                    </section>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <div class="flex flex-wrap items-center justify-between gap-3 text-xs text-gray-600 dark:text-gray-300">
          <span>{{ t(key + '.requestTotal', { count: total }) }}</span>
          <div class="flex items-center gap-2">
            <label for="retry-requests-per-page">{{ t(key + '.requestsPerPage') }}</label>
            <Select id="retry-requests-per-page" class="retry-page-size w-20 shrink-0" :model-value="pageSize" :options="requestPageSizeOptions" :aria-label="t(key + '.requestsPerPage')" @update:model-value="changePageSize" />
          </div>
        </div>
        <Pagination v-if="total > 0 && !eventsError" :total="total" :page="page" :page-size="pageSize" :show-page-size-selector="false" @update:page="changePage" />
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
.retry-page {
  container-type: inline-size;
}
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
.retry-filter {
  display: grid;
  width: 100%;
  grid-template-columns: max-content minmax(0, 1fr);
  align-items: center;
  gap: 0.5rem;
}
.retry-columns-button {
  height: 2.25rem;
  gap: 0.5rem;
  padding: 0 0.625rem;
  white-space: nowrap;
  font-size: 0.875rem;
}
.retry-search {
  width: 100%;
  height: 2.25rem;
}
.retry-filter-actions {
  grid-column: 1 / -1;
}
.retry-columns-menu {
  position: fixed;
  z-index: 100;
  overflow-y: auto;
  overscroll-behavior: contain;
  border-radius: 0.375rem;
}
.retry-expand {
  width: 2rem;
  height: 2rem;
  padding: 0;
}
.retry-timeline {
  width: min(72rem, calc(100vw - 5rem));
  width: min(72rem, calc(100cqw - 1.5rem));
  max-width: 100%;
  min-width: 0;
}
.retry-page-size :deep(.select-trigger) {
  height: 2.25rem;
  border-radius: 0.375rem;
  padding: 0.375rem 0.625rem;
}
@media (min-width: 640px) {
  .retry-filter {
    width: auto;
    max-width: 100%;
    grid-template-columns: max-content minmax(5rem, 18rem) max-content;
  }
  .retry-filter-actions {
    grid-column: auto;
  }
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
