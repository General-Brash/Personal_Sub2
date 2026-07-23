<template>
  <div class="space-y-4">
    <section class="card p-4">
      <div class="flex flex-wrap items-end gap-3">
        <div>
          <label for="transaction-user-filter" class="input-label">{{ t('finance.user') }}</label>
          <input
            id="transaction-user-filter"
            v-model.trim="userFilter"
            type="number"
            min="1"
            class="input mt-1 w-32"
            placeholder="ID"
            @keyup.enter="applyFilters"
          />
        </div>
        <div v-if="kind === 'mall'" class="w-44">
          <label class="input-label">{{ t('finance.transactions.productType') }}</label>
          <Select v-model="productType" :options="productTypeOptions" class="mt-1" @change="applyFilters" />
        </div>
        <button type="button" class="btn btn-primary h-10" :disabled="loading" @click="applyFilters">
          <Icon name="search" size="sm" />
          {{ t('common.search') }}
        </button>
        <button type="button" class="btn btn-secondary h-10 w-10 p-0" :title="t('common.refresh')" :disabled="loading" @click="load">
          <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
        </button>
      </div>
    </section>

    <DataTable :columns="columns" :data="items" :loading="loading" :row-key="transactionRowKey">
      <template #cell-user="{ row }">
        <div class="min-w-0 max-w-52">
          <RouterLink
            :to="{ path: '/admin/finance', query: { user_id: row.user_id } }"
            class="truncate text-sm font-medium text-primary-600 hover:text-primary-700 dark:text-primary-400"
          >
            {{ row.username || row.email || `#${row.user_id}` }}
          </RouterLink>
          <p v-if="row.email" class="truncate text-xs text-gray-400">{{ row.email }}</p>
        </div>
      </template>

      <template #cell-product="{ row }">
        <div v-if="kind === 'mall'" class="min-w-0 max-w-56">
          <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ row.product_name }}</p>
          <p class="mt-0.5 text-xs text-gray-400">{{ productTypeLabel(row.product_type) }}</p>
        </div>
        <span v-else data-test="bank-operation" class="text-sm text-gray-700 dark:text-gray-300">{{ bankOperationLabel(row.operation) }}</span>
      </template>

      <template #cell-payment="{ row }">
        <span data-test="transaction-amount" class="font-mono text-sm text-gray-900 dark:text-white">{{ formatTransactionAmount(row) }}</span>
      </template>
      <template #cell-credited="{ row }">
        <div v-if="kind === 'mall'" data-test="credited-change" class="space-y-0.5 font-mono text-xs">
          <p class="text-emerald-600 dark:text-emerald-400">
            P +{{ formatMoneyDisplay(row.permanent_credited_amount) }} / T +{{ formatMoneyDisplay(row.temporary_credited_amount) }}
          </p>
        </div>
      </template>
      <template #cell-balance="{ row }">
        <div class="space-y-0.5 text-right font-mono text-xs text-gray-600 dark:text-gray-300">
          <p data-test="permanent-balance-change">P {{ formatBalanceChange(row.permanent_balance_before, row.permanent_balance_after) }}</p>
          <p data-test="temporary-balance-change">T {{ formatBalanceChange(row.temporary_balance_before, row.temporary_balance_after) }}</p>
          <p v-if="kind === 'bank'" data-test="debt-balance-change">D {{ formatBalanceChange(row.debt_before, row.debt_after) }}</p>
        </div>
      </template>
      <template #cell-status="{ row }">
        <span class="badge badge-success">{{ row.status }}</span>
      </template>
      <template #cell-created_at="{ value }">
        <span class="whitespace-nowrap text-xs text-gray-500 dark:text-gray-400">{{ formatDateTime(value) }}</span>
      </template>
      <template #cell-metadata="{ row }">
        <code v-if="row.metadata" class="block max-w-48 truncate text-xs text-gray-400" :title="JSON.stringify(row.metadata)">{{ JSON.stringify(row.metadata) }}</code>
        <span v-else class="text-xs text-gray-400">-</span>
      </template>
      <template #empty>
        <div class="py-8 text-center text-sm text-gray-500 dark:text-gray-400">{{ t('finance.transactions.empty') }}</div>
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
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '@/api/admin/payment'
import { getBankTransactions } from '@/api/bank'
import { useAppStore } from '@/stores/app'
import type { Column } from '@/components/common/types'
import type { BankTransactionItem, FixedPageResponse, MallTransactionItem } from '@/types/finance'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { bankOperationLabel as translateBankOperation } from './bankOperation'
import { formatFinancialAmount, type FinanceTranslate } from './financialDisplay'
import { formatMoneyDisplay } from '@/utils/format'

type TransactionKind = 'mall' | 'bank'
const props = defineProps<{ kind: TransactionKind }>()
const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(false)
const items = ref<Array<MallTransactionItem | BankTransactionItem>>([])
const page = ref(1)
const total = ref(0)
const userFilter = ref('')
const productType = ref('')

const productTypeOptions = computed(() => [
  { value: '', label: t('finance.transactions.allProducts') },
  { value: 'currency', label: t('finance.transactions.currency') },
  { value: 'subscription', label: t('finance.transactions.subscription') },
])

const columns = computed<Column[]>(() => {
  if (props.kind === 'mall') {
    return [
      { key: 'user', label: t('finance.user') },
      { key: 'product', label: t('finance.transactions.product') },
      { key: 'payment', label: t('finance.transactions.payment') },
      { key: 'credited', label: t('finance.transactions.credited') },
      { key: 'balance', label: t('finance.balance') },
      { key: 'status', label: t('finance.transactions.status') },
      { key: 'created_at', label: t('finance.time') },
    ]
  }
  return [
    { key: 'user', label: t('finance.user') },
    { key: 'product', label: t('finance.source') },
    { key: 'payment', label: t('finance.amount') },
    { key: 'balance', label: t('finance.balance') },
    { key: 'metadata', label: t('finance.transactions.metadata') },
    { key: 'created_at', label: t('finance.time') },
  ]
})

async function load(): Promise<void> {
  loading.value = true
  try {
    const userId = Number(userFilter.value)
    const validUserId = Number.isInteger(userId) && userId > 0 ? userId : undefined
    const result = props.kind === 'mall'
      ? await adminPaymentAPI.getMallTransactions({ page: page.value, user_id: validUserId, product_type: productType.value || undefined })
      : { data: await getBankTransactions({ page: page.value, user_id: validUserId }) }
    const payload = result.data as FixedPageResponse<MallTransactionItem | BankTransactionItem>
    items.value = payload.items ?? []
    total.value = payload.total ?? items.value.length
  } catch (error) {
    console.error('Failed to load transaction log:', error)
    appStore.showError(t('finance.transactions.loadFailed'))
  } finally {
    loading.value = false
  }
}

function applyFilters(): void {
  page.value = 1
  void load()
}

function changePage(nextPage: number): void {
  page.value = nextPage
  void load()
}

function productTypeLabel(value: string): string {
  return value === 'subscription' ? t('finance.transactions.subscription') : t('finance.transactions.currency')
}

function bankOperationLabel(operation: string): string {
  return translateBankOperation(operation, t as FinanceTranslate)
}

function formatTransactionAmount(row: MallTransactionItem | BankTransactionItem): string {
  const amount = props.kind === 'mall'
    ? (row as MallTransactionItem).price
    : (row as BankTransactionItem).transaction_amount
  return formatFinancialAmount(amount, row.currency, row.unit, t as FinanceTranslate)
}

function transactionRowKey(row: MallTransactionItem | BankTransactionItem): string {
  return row.row_id || `${row.source}:${row.id}`
}

function formatSnapshot(value: string | number | null | undefined): string {
  return value === null || value === undefined ? t('finance.historyUnavailable') : formatMoneyDisplay(value)
}

function formatBalanceChange(
  before: string | number | null | undefined,
  after: string | number | null | undefined,
): string {
  return `${formatSnapshot(before)} → ${formatSnapshot(after)}`
}

function formatDateTime(value: string): string {
  return value ? new Date(value).toLocaleString(undefined, { timeZone: 'Asia/Shanghai' }) : '-'
}

onMounted(load)
</script>
