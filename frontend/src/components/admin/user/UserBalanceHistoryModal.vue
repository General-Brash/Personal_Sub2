<template>
  <BaseDialog
    :show="show"
    :title="dialogTitle"
    width="wide"
    :close-on-click-outside="true"
    :z-index="40"
    @close="emit('close')"
  >
    <div v-if="user" class="space-y-4">
      <div class="bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex items-center gap-3">
          <div class="flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
            <span class="text-lg font-medium text-primary-700 dark:text-primary-300">
              {{ user.email.charAt(0).toUpperCase() }}
            </span>
          </div>
          <div class="min-w-0 flex-1">
            <div class="flex items-center gap-2">
              <p class="truncate font-medium text-gray-900 dark:text-white">{{ user.email }}</p>
              <span
                v-if="user.deleted_at"
                class="inline-flex flex-shrink-0 items-center rounded px-1 py-px text-[10px] font-medium leading-tight text-rose-600 ring-1 ring-inset ring-rose-200 dark:text-rose-400 dark:ring-rose-500/30"
              >
                {{ t('admin.usage.userDeletedBadge') }}
              </span>
              <span
                v-if="user.username"
                class="flex-shrink-0 rounded bg-primary-50 px-1.5 py-0.5 text-xs text-primary-600 dark:bg-primary-900/20 dark:text-primary-400"
              >
                {{ user.username }}
              </span>
            </div>
            <p class="text-xs text-gray-400 dark:text-dark-500">
              {{ t('admin.users.createdAt') }}: {{ formatDateTime(user.created_at) }}
            </p>
          </div>
          <div class="flex-shrink-0 text-right">
            <p class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.users.currentBalance') }}</p>
            <p class="text-xl font-bold text-gray-900 dark:text-white">
              ${{ formatDecimalAmount(user.balance) }}
            </p>
          </div>
        </div>
        <div class="mt-2.5 flex items-center justify-between border-t border-gray-200/60 pt-2.5 dark:border-dark-600/60">
          <p class="min-w-0 flex-1 truncate text-xs text-gray-500 dark:text-dark-400" :title="user.notes || ''">
            <template v-if="user.notes">{{ t('admin.users.notes') }}: {{ user.notes }}</template>
            <template v-else>&nbsp;</template>
          </p>
          <p
            v-if="viewMode === 'permanent'"
            class="ml-4 flex-shrink-0 text-xs text-gray-500 dark:text-dark-400"
          >
            {{ t('admin.users.totalRecharged') }}:
            <span class="font-semibold text-emerald-600 dark:text-emerald-400">
              ${{ formatDecimalAmount(totalRecharged) }}
            </span>
          </p>
        </div>
      </div>

      <div
        class="grid h-10 grid-cols-2 overflow-hidden rounded-lg border border-gray-200 bg-gray-50 p-1 dark:border-dark-600 dark:bg-dark-700"
        role="group"
        :aria-label="t('checkin.admin.historyView')"
      >
        <button
          type="button"
          data-testid="history-view-permanent"
          :aria-pressed="viewMode === 'permanent'"
          :class="viewButtonClass('permanent')"
          @click="selectView('permanent')"
        >
          {{ t('checkin.admin.permanentHistory') }}
        </button>
        <button
          type="button"
          data-testid="history-view-temporary"
          :aria-pressed="viewMode === 'temporary'"
          :class="viewButtonClass('temporary')"
          @click="selectView('temporary')"
        >
          {{ t('checkin.admin.audit') }}
        </button>
      </div>

      <div class="flex flex-wrap items-center gap-3">
        <Select
          v-if="viewMode === 'permanent'"
          v-model="typeFilter"
          :options="typeOptions"
          class="w-56"
          @change="loadHistory(1)"
        />
        <div class="ml-auto flex items-center gap-3">
          <button
            v-if="!hideActions"
            type="button"
            class="flex items-center gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700"
            @click="emit('deposit')"
          >
            <Icon name="plus" size="sm" class="text-emerald-500" :stroke-width="2" />
            {{ t('admin.users.deposit') }}
          </button>
          <button
            v-if="!hideActions"
            type="button"
            class="flex items-center gap-2 rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-50 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-300 dark:hover:bg-dark-700"
            @click="emit('withdraw')"
          >
            <Icon name="arrowDown" size="sm" class="text-amber-500" :stroke-width="2" />
            {{ t('admin.users.withdraw') }}
          </button>
        </div>
      </div>

      <div
        v-if="loading"
        class="flex min-h-48 items-center justify-center"
        data-testid="history-loading"
      >
        <Icon name="refresh" size="lg" class="animate-spin text-primary-500" />
      </div>

      <div
        v-else-if="loadError"
        class="flex min-h-48 flex-col items-center justify-center gap-3 text-center"
        role="alert"
        data-testid="history-error"
      >
        <p class="text-sm text-red-600 dark:text-red-400">{{ errorMessage }}</p>
        <button type="button" class="btn btn-secondary" @click="loadHistory(currentPage)">
          <Icon name="refresh" size="sm" />
          {{ t('checkin.admin.retry') }}
        </button>
      </div>

      <div v-else-if="activeItems.length === 0" class="min-h-48 py-16 text-center" data-testid="history-empty">
        <p class="text-sm text-gray-500">
          {{ viewMode === 'temporary' ? t('checkin.admin.noTemporaryCredits') : t('admin.users.noBalanceHistory') }}
        </p>
      </div>

      <div v-else-if="viewMode === 'permanent'" class="max-h-[28rem] space-y-3 overflow-y-auto">
        <div
          v-for="item in balanceHistory"
          :key="item.id"
          class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="flex items-start justify-between">
            <div class="flex items-start gap-3">
              <div :class="['flex h-9 w-9 flex-shrink-0 items-center justify-center rounded-lg', getIconBg(item)]">
                <Icon :name="getIconName(item)" size="sm" :class="getIconColor(item)" />
              </div>
              <div>
                <p class="text-sm font-medium text-gray-900 dark:text-white">{{ getItemTitle(item) }}</p>
                <p v-if="item.notes" class="mt-0.5 text-xs text-gray-500 dark:text-dark-400" :title="item.notes">
                  {{ item.notes.length > 60 ? `${item.notes.substring(0, 55)}...` : item.notes }}
                </p>
                <p class="mt-0.5 text-xs text-gray-400 dark:text-dark-500">
                  {{ formatDateTime(item.used_at || item.created_at) }}
                </p>
              </div>
            </div>
            <div class="text-right">
              <p :class="['text-sm font-semibold', getValueColor(item)]">{{ formatValue(item) }}</p>
              <p v-if="isAdminType(item.type)" class="text-xs text-gray-400 dark:text-dark-500">
                {{ t('redeem.adminAdjustment') }}
              </p>
              <p v-else class="font-mono text-xs text-gray-400 dark:text-dark-500">
                {{ item.code.slice(0, 8) }}...
              </p>
            </div>
          </div>
        </div>
      </div>

      <div v-else class="max-h-[32rem] space-y-3 overflow-y-auto" data-testid="temporary-credit-audit-list">
        <article
          v-for="item in temporaryCredits"
          :key="item.id"
          class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800"
        >
          <div class="mb-3 flex items-start justify-between gap-4">
            <div class="flex items-center gap-2">
              <Icon :name="item.source === 'checkin' ? 'calendar' : 'gift'" size="sm" class="text-primary-500" />
              <span class="text-sm font-medium text-gray-900 dark:text-white">
                {{ temporaryCreditSourceLabel(item.source) }}
              </span>
            </div>
            <span class="font-mono text-xs text-gray-400">#{{ item.id }}</span>
          </div>
          <dl class="grid grid-cols-1 gap-x-6 gap-y-3 text-sm sm:grid-cols-2">
            <div>
              <dt class="text-xs text-gray-500">{{ t('checkin.admin.amount') }}</dt>
              <dd class="mt-0.5 font-mono text-gray-900 dark:text-white">{{ formatDecimalAmount(item.amount) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500">{{ t('checkin.admin.remainingAmount') }}</dt>
              <dd class="mt-0.5 font-mono text-gray-900 dark:text-white">{{ formatDecimalAmount(item.remaining_amount) }}</dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500">{{ t('checkin.admin.expiresAtLabel') }}</dt>
              <dd class="mt-0.5 text-gray-900 dark:text-white">
                <time :datetime="item.expires_at">{{ formatDateTime(item.expires_at) }}</time>
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500">{{ t('checkin.admin.createdAt') }}</dt>
              <dd class="mt-0.5 text-gray-900 dark:text-white">
                <time :datetime="item.created_at">{{ formatDateTime(item.created_at) }}</time>
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500">{{ t('checkin.admin.checkinId') }}</dt>
              <dd class="mt-0.5 font-mono text-gray-900 dark:text-white">
                {{ item.checkin_id == null ? '-' : `#${item.checkin_id}` }}
              </dd>
            </div>
            <div>
              <dt class="text-xs text-gray-500">{{ t('checkin.admin.grantedBy') }}</dt>
              <dd class="mt-0.5 font-mono text-gray-900 dark:text-white">
                {{ item.granted_by == null ? '-' : `#${item.granted_by}` }}
              </dd>
            </div>
            <div class="sm:col-span-2">
              <dt class="text-xs text-gray-500">{{ t('admin.users.notes') }}</dt>
              <dd class="mt-0.5 break-words text-gray-900 dark:text-white">{{ item.notes || '-' }}</dd>
            </div>
          </dl>
        </article>
      </div>

      <div v-if="!loading && !loadError && totalPages > 1" class="flex items-center justify-center gap-2 pt-2">
        <button
          type="button"
          :disabled="currentPage <= 1"
          class="btn btn-secondary px-3 py-1 text-sm"
          @click="loadHistory(currentPage - 1)"
        >
          {{ t('pagination.previous') }}
        </button>
        <span class="text-sm text-gray-500 dark:text-dark-400">{{ currentPage }} / {{ totalPages }}</span>
        <button
          type="button"
          :disabled="currentPage >= totalPages"
          class="btn btn-secondary px-3 py-1 text-sm"
          @click="loadHistory(currentPage + 1)"
        >
          {{ t('pagination.next') }}
        </button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  adminAPI,
  type BalanceHistoryItem,
  type TemporaryCreditAuditItem,
} from '@/api/admin'
import { formatDateTime, formatDecimalAmount } from '@/utils/format'
import type { AdminUser } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'

type HistoryView = 'permanent' | 'temporary'

interface HistoryState {
  currentPage: number
  total: number
  loading: boolean
  loadError: boolean
  requestSequence: number
}

const props = defineProps<{
  show: boolean
  user: AdminUser | null
  hideActions?: boolean
}>()
const emit = defineEmits<{
  (event: 'close'): void
  (event: 'deposit'): void
  (event: 'withdraw'): void
}>()
const { t } = useI18n()

const viewMode = ref<HistoryView>('temporary')
const balanceHistory = ref<BalanceHistoryItem[]>([])
const temporaryCredits = ref<TemporaryCreditAuditItem[]>([])
const permanentState = reactive<HistoryState>(createHistoryState())
const temporaryState = reactive<HistoryState>(createHistoryState())
const totalRecharged = ref(0)
const typeFilter = ref('')
const balancePageSize = 15
const temporaryPageSize = 20

const dialogTitle = computed(() =>
  viewMode.value === 'temporary'
    ? t('checkin.admin.audit')
    : t('admin.users.balanceHistoryTitle'),
)
const activePageSize = computed(() =>
  viewMode.value === 'temporary' ? temporaryPageSize : balancePageSize,
)
const activeState = computed(() => stateFor(viewMode.value))
const loading = computed(() => activeState.value.loading)
const loadError = computed(() => activeState.value.loadError)
const currentPage = computed(() => activeState.value.currentPage)
const totalPages = computed(() =>
  Math.ceil(activeState.value.total / activePageSize.value) || 1,
)
const activeItems = computed(() =>
  viewMode.value === 'temporary' ? temporaryCredits.value : balanceHistory.value,
)
const errorMessage = computed(() =>
  viewMode.value === 'temporary'
    ? t('checkin.admin.failedToLoadAudit')
    : t('admin.users.failedToLoadBalanceHistory'),
)
const typeOptions = computed(() => [
  { value: '', label: t('admin.users.allTypes') },
  { value: 'balance', label: t('admin.users.typeBalance') },
  { value: 'affiliate_balance', label: t('admin.users.typeAffiliateBalance') },
  { value: 'admin_balance', label: t('admin.users.typeAdminBalance') },
  { value: 'concurrency', label: t('admin.users.typeConcurrency') },
  { value: 'admin_concurrency', label: t('admin.users.typeAdminConcurrency') },
  { value: 'subscription', label: t('admin.users.typeSubscription') },
])

watch(
  [() => props.show, () => props.user?.id],
  ([visible, userID]) => {
    if (!visible || !userID) {
      invalidateHistoryRequests()
      return
    }
    resetHistory()
    viewMode.value = 'temporary'
    typeFilter.value = ''
    void loadHistory(1)
  },
  { immediate: true },
)

function viewButtonClass(view: HistoryView): string[] {
  return [
    'text-sm font-medium transition-colors',
    viewMode.value === view
      ? 'bg-white text-gray-900 shadow-sm dark:bg-dark-800 dark:text-white'
      : 'text-gray-500 dark:text-gray-400',
  ]
}

async function selectView(view: HistoryView) {
  if (viewMode.value === view) return
  viewMode.value = view
  await loadHistory(stateFor(view).currentPage)
}

async function loadHistory(page: number) {
  if (!props.user) return
  const requestedView = viewMode.value
  const state = stateFor(requestedView)
  const sequence = ++state.requestSequence
  state.loading = true
  state.loadError = false
  state.currentPage = page
  try {
    if (requestedView === 'temporary') {
      const response = await adminAPI.users.getTemporaryCredits(
        props.user.id,
        page,
        temporaryPageSize,
      )
      if (sequence !== state.requestSequence) return
      temporaryCredits.value = response.items ?? []
      state.total = response.total ?? 0
    } else {
      const response = await adminAPI.users.getUserBalanceHistory(
        props.user.id,
        page,
        balancePageSize,
        typeFilter.value || undefined,
      )
      if (sequence !== state.requestSequence) return
      balanceHistory.value = response.items ?? []
      state.total = response.total ?? 0
      totalRecharged.value = response.total_recharged ?? 0
    }
  } catch (error) {
    if (sequence !== state.requestSequence) return
    console.error('Failed to load user history:', error)
    state.loadError = true
    state.total = 0
    if (requestedView === 'temporary') temporaryCredits.value = []
    else {
      balanceHistory.value = []
      totalRecharged.value = 0
    }
  } finally {
    if (sequence === state.requestSequence) state.loading = false
  }
}

function createHistoryState(): HistoryState {
  return {
    currentPage: 1,
    total: 0,
    loading: false,
    loadError: false,
    requestSequence: 0,
  }
}

function stateFor(view: HistoryView): HistoryState {
  return view === 'temporary' ? temporaryState : permanentState
}

function resetHistory() {
  for (const state of [permanentState, temporaryState]) {
    const requestSequence = state.requestSequence + 1
    Object.assign(state, createHistoryState(), { requestSequence })
  }
  balanceHistory.value = []
  temporaryCredits.value = []
  totalRecharged.value = 0
}

function invalidateHistoryRequests() {
  for (const state of [permanentState, temporaryState]) {
    state.requestSequence += 1
    state.loading = false
  }
}

function temporaryCreditSourceLabel(source: string): string {
  if (source === 'checkin') return t('checkin.admin.sourceCheckin')
  if (source === 'admin_grant') return t('checkin.admin.sourceAdminGrant')
  return source
}

const isAdminType = (type: string) => type === 'admin_balance' || type === 'admin_concurrency'
const isBalanceType = (type: string) =>
  type === 'balance' || type === 'admin_balance' || type === 'affiliate_balance'
const isSubscriptionType = (type: string) => type === 'subscription'

function getIconName(item: BalanceHistoryItem): 'dollar' | 'badge' | 'bolt' {
  if (isBalanceType(item.type)) return 'dollar'
  if (isSubscriptionType(item.type)) return 'badge'
  return 'bolt'
}

function getIconBg(item: BalanceHistoryItem): string {
  if (isBalanceType(item.type)) {
    return item.value >= 0
      ? 'bg-emerald-100 dark:bg-emerald-900/30'
      : 'bg-red-100 dark:bg-red-900/30'
  }
  if (isSubscriptionType(item.type)) return 'bg-purple-100 dark:bg-purple-900/30'
  return item.value >= 0
    ? 'bg-blue-100 dark:bg-blue-900/30'
    : 'bg-orange-100 dark:bg-orange-900/30'
}

function getIconColor(item: BalanceHistoryItem): string {
  if (isBalanceType(item.type)) {
    return item.value >= 0
      ? 'text-emerald-600 dark:text-emerald-400'
      : 'text-red-600 dark:text-red-400'
  }
  if (isSubscriptionType(item.type)) return 'text-purple-600 dark:text-purple-400'
  return item.value >= 0
    ? 'text-blue-600 dark:text-blue-400'
    : 'text-orange-600 dark:text-orange-400'
}

const getValueColor = getIconColor

function getItemTitle(item: BalanceHistoryItem): string {
  switch (item.type) {
    case 'balance': return t('redeem.balanceAddedRedeem')
    case 'affiliate_balance': return t('redeem.balanceAddedAffiliate')
    case 'admin_balance':
      return item.value >= 0 ? t('redeem.balanceAddedAdmin') : t('redeem.balanceDeductedAdmin')
    case 'concurrency': return t('redeem.concurrencyAddedRedeem')
    case 'admin_concurrency':
      return item.value >= 0 ? t('redeem.concurrencyAddedAdmin') : t('redeem.concurrencyReducedAdmin')
    case 'subscription': return t('redeem.subscriptionAssigned')
    default: return t('common.unknown')
  }
}

function formatValue(item: BalanceHistoryItem): string {
  if (isBalanceType(item.type)) {
    const sign = item.value >= 0 ? '+' : ''
    return `${sign}$${formatDecimalAmount(item.value)}`
  }
  if (isSubscriptionType(item.type)) {
    const days = item.validity_days || Math.round(item.value)
    const groupName = item.group?.name || ''
    return groupName ? `${days}d - ${groupName}` : `${days}d`
  }
  const sign = item.value >= 0 ? '+' : ''
  return `${sign}${item.value}`
}
</script>
