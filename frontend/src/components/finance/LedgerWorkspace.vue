<template>
  <div class="space-y-5">
    <section class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div
        role="group"
        :aria-label="t('finance.summary')"
        class="inline-flex self-start rounded-lg border border-gray-200 bg-gray-100 p-1 dark:border-dark-600 dark:bg-dark-800"
      >
        <button
          v-for="option in windowOptions"
          :key="option.days"
          type="button"
          class="rounded-md px-4 py-2 text-sm font-medium transition-colors"
          :class="selectedDays === option.days
            ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300'
            : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200'"
          @click="selectDays(option.days)"
        >
          {{ option.label }}
        </button>
      </div>

      <div v-if="admin" class="flex flex-wrap items-end gap-2">
        <div>
          <label for="ledger-user-filter" class="input-label">{{ t('finance.user') }}</label>
          <input
            id="ledger-user-filter"
            v-model.trim="userFilter"
            type="number"
            min="1"
            class="input mt-1 w-32"
            placeholder="ID"
            @keyup.enter="applyFilters"
          />
        </div>
        <div class="w-44">
          <label class="input-label">{{ t('finance.category') }}</label>
          <Select v-model="categoryFilter" :options="categoryOptions" class="mt-1" @change="applyFilters" />
        </div>
        <button type="button" class="btn btn-secondary h-10" :disabled="loading" @click="applyFilters">
          <Icon name="search" size="sm" />
          {{ t('common.search') }}
        </button>
      </div>
    </section>

    <section class="grid grid-cols-1 gap-4 sm:grid-cols-3">
      <article class="card p-4 sm:col-span-1">
        <p class="text-xs font-medium text-gray-500 dark:text-gray-400">{{ selectedWindowLabel }}</p>
        <div v-if="selectedWindowTotals.length" class="mt-2 space-y-1">
          <p
            v-for="total in selectedWindowTotals"
            :key="financialAmountKey(total)"
            class="break-all font-mono text-2xl font-semibold text-gray-900 dark:text-white"
          >
            {{ formatFinancialAmount(total.amount, total.currency, total.unit, translate) }}
          </p>
        </div>
        <p v-else class="mt-2 font-mono text-2xl font-semibold text-gray-900 dark:text-white">-</p>
        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
          {{ selectedWindow.count }} {{ t('finance.records') }}
        </p>
      </article>
      <article class="card p-4 sm:col-span-2">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('finance.summary') }}</h2>
        <LedgerPieChart :summary="summary" />
      </article>
    </section>

    <section class="space-y-3">
      <div class="flex items-center justify-between gap-3">
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('finance.details') }}</h2>
        <button
          type="button"
          class="btn btn-secondary h-9 w-9 p-0"
          :title="t('common.refresh')"
          :disabled="loading"
          @click="load"
        >
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>

      <DataTable :columns="columns" :data="items" :loading="loading" :row-key="ledgerRowKey">
        <template v-if="admin" #cell-user="{ row }">
          <div class="min-w-0 max-w-52">
            <RouterLink
              :to="{ path: '/admin/finance', query: { user_id: row.user_id, days: selectedDays } }"
              class="truncate text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400"
              @click="drillIntoUser(row.user_id)"
            >
              {{ row.username || row.email || `#${row.user_id}` }}
            </RouterLink>
            <p v-if="row.email" class="truncate text-xs text-gray-400">{{ row.email }}</p>
          </div>
        </template>
        <template #cell-label="{ row }">
          <div class="min-w-0 max-w-72">
            <p data-test="ledger-label" class="truncate text-sm font-medium text-gray-900 dark:text-white" :title="ledgerItemLabel(row)">
              {{ ledgerItemLabel(row) }}
            </p>
            <p class="mt-0.5 truncate text-xs text-gray-400">
              {{ ledgerItemSecondary(row) }}
            </p>
            <p
              v-if="row.category === 'model' && row.count"
              data-test="model-call-count"
              class="mt-0.5 text-xs text-gray-400"
            >
              {{ t('finance.modelCallCount', { count: row.count }) }}
            </p>
          </div>
        </template>
        <template #cell-amount="{ row }">
          <span data-test="ledger-amount" class="font-mono font-medium text-red-600 dark:text-red-400">
            -{{ formatFinancialAmount(row.cost_amount || row.amount, row.currency, row.unit, translate) }}
          </span>
        </template>
        <template #cell-balance="{ row }">
          <div class="space-y-0.5 text-right font-mono text-xs text-gray-600 dark:text-gray-300">
            <p>P {{ formatSnapshot(row.permanent_balance_after) }}</p>
            <p>T {{ formatSnapshot(row.temporary_balance_after) }}</p>
            <p v-if="row.debt_after !== null && Number(row.debt_after)">D {{ formatSnapshot(row.debt_after) }}</p>
          </div>
        </template>
        <template #cell-created_at="{ value }">
          <span class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">{{ formatDateTime(value) }}</span>
        </template>
        <template #empty>
          <div class="flex flex-col items-center py-6">
            <Icon name="inbox" size="xl" class="mb-3 text-gray-300 dark:text-dark-600" />
            <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('finance.noData') }}</p>
          </div>
        </template>
      </DataTable>

      <Pagination
        v-if="total > 0"
        :total="total"
        :page="page"
        :page-size="20"
        :show-page-size-selector="false"
        @update:page="changePage"
      />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { paymentAPI } from '@/api/payment'
import { adminPaymentAPI } from '@/api/admin/payment'
import { useAppStore } from '@/stores/app'
import type { Column } from '@/components/common/types'
import type { LedgerAmountTotal, LedgerCategorySummary, LedgerItem, LedgerResponse, LedgerWindowSummary } from '@/types/finance'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import LedgerPieChart from './LedgerPieChart.vue'
import { bankLedgerLabel, bankOperationLabel } from './bankOperation'
import { financialAmountKey, formatFinancialAmount, sumFinancialAmounts, type FinanceTranslate } from './financialDisplay'
import { formatMoneyDisplay } from '@/utils/format'

const props = withDefaults(defineProps<{ admin?: boolean; initialUserId?: number; initialDays?: 1 | 7 | 15 }>(), {
  admin: false,
  initialDays: 7,
})

const { t } = useI18n()
const translate = t as unknown as FinanceTranslate
const appStore = useAppStore()
const selectedDays = ref<1 | 7 | 15>(props.initialDays)
const page = ref(1)
const total = ref(0)
const loading = ref(false)
const response = ref<LedgerResponse | null>(null)
const items = ref<LedgerItem[]>([])
const summary = ref<LedgerCategorySummary[]>([])
const userFilter = ref(props.initialUserId ? String(props.initialUserId) : '')
const categoryFilter = ref('')

const windowOptions = computed(() => [
  { days: 1 as const, label: t('finance.windows.today') },
  { days: 7 as const, label: t('finance.windows.sevenDays') },
  { days: 15 as const, label: t('finance.windows.fifteenDays') },
])

const categoryOptions = computed(() => {
  const seen = new Set<string>()
  return [
    { value: '', label: t('finance.total') },
    ...summary.value.flatMap((item) => {
      if (seen.has(item.category)) return []
      seen.add(item.category)
      return [{ value: item.category, label: item.label }]
    }),
  ]
})

const selectedWindowKey = computed(() => selectedDays.value === 1
  ? 'today'
  : selectedDays.value === 7
    ? 'seven_days'
    : 'fifteen_days')

const selectedWindow = computed<LedgerWindowSummary>(() => response.value?.windows?.[selectedWindowKey.value] ?? {
  total_amount: '0',
  count: summary.value.reduce((sum, item) => sum + (Number(item.count) || 0), 0),
  categories: summary.value,
  totals: buildTotals(summary.value),
})

const selectedWindowTotals = computed<LedgerAmountTotal[]>(() => {
  const totals = selectedWindow.value.totals ?? []
  return totals.length ? totals : buildTotals(summary.value)
})

const selectedWindowLabel = computed(() => windowOptions.value.find((option) => option.days === selectedDays.value)?.label ?? '')

const columns = computed<Column[]>(() => {
  const result: Column[] = []
  if (props.admin) result.push({ key: 'user', label: t('finance.user') })
  result.push(
    { key: 'label', label: t('finance.source') },
    { key: 'amount', label: t('finance.amount'), class: 'text-right' },
    { key: 'balance', label: t('finance.balance'), class: 'text-right' },
    { key: 'created_at', label: t('finance.time') },
  )
  return result
})

async function load(): Promise<void> {
  loading.value = true
  try {
    const result = props.admin
      ? await adminPaymentAPI.getLedger({
          page: page.value,
          days: selectedDays.value,
          user_id: validUserId(),
          category: categoryFilter.value || undefined,
        })
      : await paymentAPI.getLedger({ page: page.value, days: selectedDays.value })
    response.value = result.data
    items.value = result.data.items ?? []
    summary.value = result.data.summary ?? []
    total.value = result.data.total ?? items.value.length
  } catch (error) {
    console.error('Failed to load ledger:', error)
    appStore.showError(t('common.error'))
  } finally {
    loading.value = false
  }
}

function validUserId(): number | undefined {
  const value = Number(userFilter.value)
  return Number.isInteger(value) && value > 0 ? value : undefined
}

function selectDays(days: 1 | 7 | 15): void {
  if (selectedDays.value === days) return
  selectedDays.value = days
  page.value = 1
  void load()
}

function applyFilters(): void {
  page.value = 1
  void load()
}

function drillIntoUser(userId: number | undefined): void {
  if (!userId) return
  userFilter.value = String(userId)
  page.value = 1
  void load()
}

function changePage(nextPage: number): void {
  page.value = nextPage
  void load()
}

function formatSnapshot(value: string | number | null | undefined): string {
  return value === null || value === undefined ? t('finance.historyUnavailable') : formatMoneyDisplay(value)
}

function buildTotals(source: Array<LedgerCategorySummary | LedgerAmountTotal>): LedgerAmountTotal[] {
  const grouped = new Map<string, LedgerAmountTotal>()
  for (const item of source) {
    const key = financialAmountKey(item)
    const existing = grouped.get(key)
    if (existing) {
      existing.amount = sumFinancialAmounts([existing.amount, item.amount])
      existing.count += item.count
    } else {
      grouped.set(key, {
        currency: item.currency,
        unit: item.unit,
        amount: item.amount,
        count: item.count,
      })
    }
  }
  return [...grouped.values()]
}

function ledgerRowKey(row: LedgerItem): string {
  return row.row_id || `${row.source}:${row.id}`
}

function ledgerItemLabel(row: LedgerItem): string {
  if (row.source === 'bank') {
    return bankLedgerLabel(row.operation, row.category, row.cost_amount, translate)
  }
  return row.label
}

function ledgerItemSecondary(row: LedgerItem): string {
  if (row.source === 'bank') return bankOperationLabel(row.operation, translate)
  return row.model || row.operation || row.source
}

function formatDateTime(value: string): string {
  return value ? new Date(value).toLocaleString(undefined, { timeZone: 'Asia/Shanghai' }) : '-'
}

watch(() => props.initialUserId, (value) => {
  userFilter.value = value ? String(value) : ''
})

onMounted(load)
</script>
