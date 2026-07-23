<template>
  <AppLayout>
    <div id="admin-shelf-panel-currency" role="tabpanel" aria-labelledby="admin-shelf-tab-currency" class="space-y-4">
      <header>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('commerce.shelf.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('commerce.shelf.description') }}</p>
      </header>

      <ShelfSectionTabs active-section="currency" />

      <div class="flex flex-wrap items-center justify-end gap-2">
        <RouterLink to="/admin/orders/mall-transactions" class="btn btn-secondary inline-flex items-center gap-2">
          <Icon name="clipboard" size="sm" />
          {{ t('finance.transactions.mallTitle') }}
        </RouterLink>
        <button type="button" class="btn btn-secondary inline-flex items-center gap-2" data-test="toggle-shelf-analytics" @click="toggleAnalytics">
          <Icon name="chart" size="sm" />
          {{ t('finance.analytics.title') }}
        </button>
        <button type="button" class="btn btn-secondary h-10 w-10 p-0" :title="t('common.refresh')" :disabled="loading" @click="loadProducts">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
        <button type="button" class="btn btn-primary inline-flex items-center gap-2" data-test="create-currency-product" @click="openEditor()">
          <Icon name="plus" size="sm" />
          {{ t('commerce.shelf.createCurrencyProduct') }}
        </button>
      </div>

      <section v-if="showAnalytics" data-test="shelf-analytics" class="card space-y-4 p-5">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('finance.analytics.title') }}</h2>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('finance.analytics.description') }}</p>
          </div>
          <div class="inline-flex rounded-lg border border-gray-200 bg-gray-100 p-1 dark:border-dark-600 dark:bg-dark-800">
            <button v-for="days in analyticsDays" :key="days" type="button" class="rounded-md px-3 py-1 text-xs font-medium" :class="analyticsRange === days ? 'bg-white text-primary-700 shadow-sm dark:bg-dark-700 dark:text-primary-300' : 'text-gray-500'" @click="loadAnalytics(days)">
              {{ t('finance.analytics.days', { days }) }}
            </button>
          </div>
        </div>
        <div v-if="analyticsLoading" class="py-10 text-center text-sm text-gray-500">{{ t('finance.loading') }}</div>
        <template v-else-if="analytics">
          <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
            <div class="rounded-lg border border-gray-200 p-3 dark:border-dark-600"><p class="text-xs text-gray-500">{{ t('finance.analytics.totalSales') }}</p><p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ analytics.total_sales }}</p></div>
            <div v-for="total in analyticsRevenueTotals" :key="financialAmountKey(total)" data-test="analytics-revenue-total" class="rounded-lg border border-gray-200 p-3 dark:border-dark-600">
              <p class="text-xs text-gray-500">{{ t('finance.analytics.revenueUnit', { unit: financialUnitLabel(total.currency, total.unit, translate) }) }}</p>
              <p class="mt-1 font-mono text-xl font-semibold text-emerald-600 dark:text-emerald-400">{{ formatFinancialAmount(total.revenue, total.currency, total.unit, translate) }}</p>
            </div>
          </div>
          <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <div>
              <h3 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ t('finance.analytics.categories') }}</h3>
              <div class="space-y-3">
                <div v-for="category in analyticsCategoryTotals" :key="category.product_type" class="border-b border-gray-100 py-2 text-sm dark:border-dark-700">
                  <div class="flex items-center justify-between gap-3">
                    <span>{{ productTypeLabel(category.product_type) }}</span>
                    <span class="font-mono text-gray-600 dark:text-gray-300">{{ category.sales_count }} {{ t('finance.analytics.sales') }}</span>
                  </div>
                  <div v-for="total in category.revenue_totals" :key="financialAmountKey(total)" class="mt-1 text-right font-mono text-xs text-gray-500 dark:text-gray-400">
                    {{ formatFinancialAmount(total.revenue, total.currency, total.unit, translate) }}
                  </div>
                </div>
                <p v-if="!analyticsCategoryTotals.length" class="text-sm text-gray-500">{{ t('finance.analytics.noData') }}</p>
              </div>
            </div>
            <div>
              <h3 class="mb-2 text-sm font-semibold text-gray-900 dark:text-white">{{ t('finance.analytics.products') }}</h3>
              <div v-if="!analytics.products?.length" class="text-sm text-gray-500">{{ t('finance.analytics.noData') }}</div>
              <div v-for="row in analytics.products" :key="analyticsProductKey(row)" class="flex items-center justify-between border-b border-gray-100 py-2 text-sm dark:border-dark-700">
                <span class="min-w-0 truncate">{{ row.product_name }} <span class="text-xs text-gray-400">({{ productTypeLabel(row.product_type) }})</span></span>
                <span class="ml-3 shrink-0 font-mono text-gray-600 dark:text-gray-300">{{ row.sales_count }} / {{ formatFinancialAmount(row.revenue, row.currency, row.unit, translate) }}</span>
              </div>
            </div>
          </div>
          <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
            <DailyRevenueChart
              v-for="group in analyticsDailyGroups"
              :key="group.key"
              :title="t('finance.analytics.dailyUnit', { unit: financialUnitLabel(group.currency, group.unit, translate) })"
              :revenue-label="financialUnitLabel(group.currency, group.unit, translate)"
              :data="group.points"
              :loading="false"
            />
          </div>
        </template>
      </section>

      <DataTable :columns="columns" :data="products" :loading="loading">
        <template #cell-name="{ row }">
          <div class="min-w-0">
            <p class="truncate text-sm font-medium text-gray-900 dark:text-white">{{ row.name }}</p>
            <p v-if="row.description" class="mt-0.5 max-w-md truncate text-xs text-gray-500 dark:text-gray-400">{{ row.description }}</p>
          </div>
        </template>
        <template #cell-payment_price="{ value, row }">
          <span class="font-mono text-sm text-gray-900 dark:text-white">${{ formatMoneyDisplay(value) }}</span>
          <span class="ml-1 text-xs text-gray-500 dark:text-gray-400">{{ creditTypeLabel(row.payment_credit_type) }}</span>
        </template>
        <template #cell-credited_amount="{ value, row }">
          <span class="font-mono text-sm text-emerald-600 dark:text-emerald-400">${{ formatMoneyDisplay(value) }}</span>
          <span class="ml-1 text-xs text-gray-500 dark:text-gray-400">{{ creditTypeLabel(row.credited_type) }}</span>
        </template>
        <template #cell-sales_count="{ row }">
          <span class="font-mono text-sm text-gray-700 dark:text-gray-300">{{ row.sales_count ?? 0 }}</span>
        </template>
        <template #cell-for_sale="{ row }">
          <span
            class="inline-flex rounded px-2 py-0.5 text-xs font-medium ring-1 ring-inset"
            :class="row.is_active && row.for_sale
              ? 'bg-emerald-50 text-emerald-700 ring-emerald-200 dark:bg-emerald-900/20 dark:text-emerald-300 dark:ring-emerald-800'
              : 'bg-gray-100 text-gray-600 ring-gray-200 dark:bg-dark-700 dark:text-gray-300 dark:ring-dark-600'"
          >
            {{ t(row.is_active && row.for_sale ? 'commerce.shelf.published' : 'commerce.shelf.unpublished') }}
          </span>
        </template>
        <template #cell-actions="{ row }">
          <div class="flex items-center gap-1">
            <button type="button" :data-test="`edit-currency-product-${row.id}`" class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 hover:bg-blue-50 hover:text-blue-600 dark:hover:bg-blue-900/20 dark:hover:text-blue-400" :title="t('common.edit')" @click="openEditor(row)">
              <Icon name="edit" size="sm" />
            </button>
            <button type="button" :data-test="`delete-currency-product-${row.id}`" class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-gray-500 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400" :title="t('common.delete')" @click="confirmDelete(row)">
              <Icon name="trash" size="sm" />
            </button>
          </div>
        </template>
        <template #empty>
          <div data-test="currency-products-empty" class="flex flex-col items-center">
            <Icon name="inbox" size="xl" class="mb-4 h-12 w-12 text-gray-400 dark:text-dark-500" />
            <p class="text-lg font-medium text-gray-900 dark:text-gray-100">{{ t('commerce.shelf.empty') }}</p>
          </div>
        </template>
      </DataTable>
    </div>

    <BaseDialog :show="showEditor" :title="editingProduct ? t('commerce.shelf.editCurrencyProduct') : t('commerce.shelf.createCurrencyProduct')" width="wide" @close="closeEditor">
      <fieldset :disabled="saving" class="space-y-4">
        <div>
          <label for="currency-product-name" class="input-label">{{ t('commerce.shelf.name') }}</label>
          <input id="currency-product-name" v-model.trim="form.name" data-test="currency-product-name" type="text" class="input mt-1" maxlength="100" />
        </div>
        <div>
          <label for="currency-product-description" class="input-label">{{ t('commerce.shelf.descriptionLabel') }}</label>
          <textarea id="currency-product-description" v-model.trim="form.description" data-test="currency-product-description" rows="3" class="input mt-1 resize-y" />
        </div>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <div>
              <label class="input-label">{{ t('commerce.shelf.paymentType') }}</label>
              <Select v-model="form.payment_credit_type" :options="creditTypeOptions" data-test="currency-product-payment-type" />
            </div>
            <div>
            <label for="currency-product-price" class="input-label">{{ t('commerce.shelf.paymentPrice') }}</label>
              <div class="relative mt-1">
                <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center font-mono text-gray-500">$</span>
                <input id="currency-product-price" v-model.number="form.payment_price" data-test="currency-product-price" type="number" min="0.00000001" step="0.00000001" class="input pl-7 font-mono" />
              </div>
            </div>
          </div>
          <div class="space-y-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600">
            <div>
              <label class="input-label">{{ t('commerce.shelf.creditedType') }}</label>
              <Select v-model="form.credited_type" :options="creditTypeOptions" data-test="currency-product-credited-type" />
            </div>
            <div>
            <label for="currency-product-credit" class="input-label">{{ t('commerce.shelf.creditedAmount') }}</label>
              <div class="relative mt-1">
                <span class="pointer-events-none absolute inset-y-0 left-3 flex items-center font-mono text-gray-500">$</span>
                <input id="currency-product-credit" v-model.number="form.credited_amount" data-test="currency-product-credit" type="number" min="0.00000001" step="0.00000001" class="input pl-7 font-mono" />
              </div>
            </div>
          </div>
        </div>
        <div>
          <label for="currency-product-sort" class="input-label">{{ t('commerce.shelf.sortOrder') }}</label>
          <input id="currency-product-sort" v-model.number="form.sort_order" data-test="currency-product-sort" type="number" step="1" class="input mt-1" />
        </div>
        <details class="rounded-lg border border-gray-200 px-4 py-3 dark:border-dark-600" data-test="currency-product-advanced-settings">
          <summary class="cursor-pointer text-sm font-medium text-gray-800 dark:text-gray-200">
            {{ t('commerce.shelf.advancedSettings') }}
          </summary>
          <div class="mt-4 grid grid-cols-1 gap-4 sm:grid-cols-2">
            <div>
              <label for="currency-product-daily-limit" class="input-label">{{ t('commerce.shelf.dailyPurchaseLimit') }}</label>
              <input
                id="currency-product-daily-limit"
                v-model.number="form.daily_purchase_limit"
                data-test="currency-product-daily-limit"
                type="number"
                min="0"
                step="1"
                class="input mt-1"
                :placeholder="t('commerce.shelf.purchaseLimitPlaceholder')"
              />
              <p class="input-hint mt-1.5">{{ t('commerce.shelf.dailyPurchaseLimitHint') }}</p>
            </div>
            <div>
              <label for="currency-product-total-limit" class="input-label">{{ t('commerce.shelf.totalPurchaseLimit') }}</label>
              <input
                id="currency-product-total-limit"
                v-model.number="form.total_purchase_limit"
                data-test="currency-product-total-limit"
                type="number"
                min="0"
                step="1"
                class="input mt-1"
                :placeholder="t('commerce.shelf.purchaseLimitPlaceholder')"
              />
              <p class="input-hint mt-1.5">{{ t('commerce.shelf.totalPurchaseLimitHint') }}</p>
            </div>
          </div>
        </details>
        <div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <label class="flex items-center justify-between rounded-lg border border-gray-200 px-4 py-3 text-sm text-gray-700 dark:border-dark-600 dark:text-gray-300">
            {{ t('commerce.shelf.active') }}
            <Toggle v-model="form.is_active" />
          </label>
          <label class="flex items-center justify-between rounded-lg border border-gray-200 px-4 py-3 text-sm text-gray-700 dark:border-dark-600 dark:text-gray-300">
            {{ t('commerce.shelf.forSale') }}
            <Toggle v-model="form.for_sale" />
          </label>
        </div>
      </fieldset>
      <template #footer>
        <div class="flex justify-end gap-3">
          <button type="button" class="btn btn-secondary" :disabled="saving" @click="closeEditor">{{ t('common.cancel') }}</button>
          <button type="button" class="btn btn-primary inline-flex min-w-20 items-center justify-center gap-2" data-test="save-currency-product" :disabled="saving" @click="saveProduct">
            <Icon v-if="saving" name="refresh" size="sm" class="animate-spin" />
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="deletingProduct !== null"
      :title="t('commerce.shelf.deleteTitle')"
      :message="t('commerce.shelf.deleteConfirm')"
      :confirm-text="t('common.delete')"
      danger
      @confirm="deleteProduct"
      @cancel="deletingProduct = null"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminPaymentAPI } from '@/api/admin/payment'
import type { MallAnalyticsProduct, MallAnalyticsResponse, MallAnalyticsRevenueTotal } from '@/types/finance'
import type { CurrencyProduct, CurrencyProductInput } from '@/types/payment'
import type { Column } from '@/components/common/types'
import AppLayout from '@/components/layout/AppLayout.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Icon from '@/components/icons/Icon.vue'
import ShelfSectionTabs from '@/components/admin/payment/ShelfSectionTabs.vue'
import DailyRevenueChart from '@/components/admin/payment/DailyRevenueChart.vue'
import { useAppStore } from '@/stores/app'
import { financialAmountKey, financialUnitLabel, formatFinancialAmount, sumFinancialAmounts, type FinanceTranslate } from '@/components/finance/financialDisplay'
import { formatMoneyDisplay } from '@/utils/format'

const { t } = useI18n()
const translate = t as unknown as FinanceTranslate
const appStore = useAppStore()
const products = ref<CurrencyProduct[]>([])
const loading = ref(false)
const saving = ref(false)
const showEditor = ref(false)
const editingProduct = ref<CurrencyProduct | null>(null)
const deletingProduct = ref<CurrencyProduct | null>(null)
const showAnalytics = ref(false)
const analyticsLoading = ref(false)
const analytics = ref<MallAnalyticsResponse | null>(null)
const analyticsRange = ref(30)
const analyticsDays = [7, 30, 90]

const analyticsRevenueTotals = computed<MallAnalyticsRevenueTotal[]>(() => {
  const explicit = analytics.value?.revenue_totals ?? []
  if (explicit.length) return explicit
  return aggregateRevenueTotals(analytics.value?.products ?? [])
})

const analyticsCategoryTotals = computed(() => {
  const grouped = new Map<string, { product_type: string; sales_count: number; revenue_totals: MallAnalyticsRevenueTotal[] }>()
  for (const product of analytics.value?.products ?? []) {
    const category = grouped.get(product.product_type) ?? { product_type: product.product_type, sales_count: 0, revenue_totals: [] }
    category.sales_count += product.sales_count
    const key = financialAmountKey(product)
    const existing = category.revenue_totals.find((total) => financialAmountKey(total) === key)
    if (existing) existing.revenue = sumFinancialAmounts([existing.revenue, product.revenue])
    else category.revenue_totals.push({ currency: product.currency, unit: product.unit, revenue: product.revenue, sales_count: product.sales_count })
    grouped.set(product.product_type, category)
  }
  return [...grouped.values()]
})

const analyticsDailyGroups = computed(() => {
  const grouped = new Map<string, { key: string; currency: string; unit: string; points: Array<{ date: string; amount: number; count: number }> }>()
  for (const point of analytics.value?.daily ?? []) {
    const key = financialAmountKey(point)
    const group = grouped.get(key) ?? { key, currency: point.currency, unit: point.unit, points: [] }
    group.points.push({ date: point.date, amount: Number(point.revenue) || 0, count: point.sales_count })
    grouped.set(key, group)
  }
  return [...grouped.values()]
})
const creditTypeOptions = computed(() => [
  { value: 'permanent', label: t('commerce.creditType.permanent') },
  { value: 'temporary', label: t('commerce.creditType.temporary') },
])

const form = reactive<CurrencyProductInput>(emptyForm())
const columns = computed<Column[]>(() => [
  { key: 'name', label: t('commerce.shelf.name') },
  { key: 'payment_price', label: t('commerce.shelf.paymentPrice') },
  { key: 'credited_amount', label: t('commerce.shelf.creditedAmount') },
  { key: 'sales_count', label: t('finance.analytics.sales') },
  { key: 'for_sale', label: t('commerce.shelf.forSale') },
  { key: 'sort_order', label: t('commerce.shelf.sortOrder') },
  { key: 'actions', label: t('common.actions') },
])

function emptyForm(): CurrencyProductInput {
  return {
    name: '',
    description: '',
    payment_price: 0,
    payment_credit_type: 'permanent',
    credited_type: 'permanent',
    credited_amount: 0,
    sort_order: 0,
    is_active: true,
    for_sale: true,
    daily_purchase_limit: 0,
    total_purchase_limit: 0,
  }
}

async function loadProducts(): Promise<void> {
  loading.value = true
  try {
    const response = await adminPaymentAPI.getCurrencyProducts()
    products.value = response.data ?? []
  } catch (error) {
    console.error('Failed to load currency products:', error)
    appStore.showError(t('commerce.shelf.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function loadAnalytics(days = analyticsRange.value): Promise<void> {
  analyticsRange.value = days
  analyticsLoading.value = true
  try {
    const response = await adminPaymentAPI.getMallAnalytics(days)
    analytics.value = response.data
  } catch (error) {
    console.error('Failed to load mall analytics:', error)
    appStore.showError(t('finance.transactions.loadFailed'))
  } finally {
    analyticsLoading.value = false
  }
}

function toggleAnalytics(): void {
  showAnalytics.value = !showAnalytics.value
  if (showAnalytics.value && !analytics.value) void loadAnalytics()
}

function aggregateRevenueTotals(products: MallAnalyticsProduct[]): MallAnalyticsRevenueTotal[] {
  const grouped = new Map<string, MallAnalyticsRevenueTotal>()
  for (const product of products) {
    const key = financialAmountKey(product)
    const existing = grouped.get(key)
    if (existing) {
      existing.revenue = sumFinancialAmounts([existing.revenue, product.revenue])
      existing.sales_count += product.sales_count
    } else {
      grouped.set(key, {
        currency: product.currency,
        unit: product.unit,
        revenue: product.revenue,
        sales_count: product.sales_count,
      })
    }
  }
  return [...grouped.values()]
}

function analyticsProductKey(row: MallAnalyticsProduct): string {
  return `${row.product_type}:${row.product_id}:${financialAmountKey(row)}`
}

function productTypeLabel(value: string): string {
  return value === 'subscription' ? t('finance.transactions.subscription') : t('finance.transactions.currency')
}


function openEditor(product?: CurrencyProduct): void {
  editingProduct.value = product ?? null
  Object.assign(form, product ? {
    name: product.name,
    description: product.description,
    payment_price: product.payment_price,
    payment_credit_type: product.payment_credit_type ?? 'permanent',
    credited_type: product.credited_type ?? 'permanent',
    credited_amount: product.credited_amount ?? product.credited_permanent_amount ?? 0,
    sort_order: product.sort_order,
    is_active: product.is_active !== false,
    for_sale: product.for_sale !== false,
    daily_purchase_limit: product.daily_purchase_limit ?? 0,
    total_purchase_limit: product.total_purchase_limit ?? 0,
  } : emptyForm())
  showEditor.value = true
}

function closeEditor(): void {
  if (saving.value) return
  showEditor.value = false
  editingProduct.value = null
}

function validForm(): boolean {
  return Boolean(
    form.name.trim()
    && Number.isFinite(form.payment_price) && form.payment_price > 0
    && Number.isFinite(form.credited_amount) && form.credited_amount > 0
    && Number.isInteger(form.sort_order)
    && validPurchaseLimit(form.daily_purchase_limit)
    && validPurchaseLimit(form.total_purchase_limit),
  )
}

function creditTypeLabel(type: CurrencyProduct['payment_credit_type']): string {
  return t(`commerce.creditType.${type === 'temporary' ? 'temporary' : 'permanent'}`)
}

function validPurchaseLimit(value: unknown): boolean {
  return value === '' || (typeof value === 'number' && Number.isInteger(value) && value >= 0)
}

function normalizedPurchaseLimit(value: unknown): number {
  return typeof value === 'number' && Number.isInteger(value) && value > 0 ? value : 0
}

async function saveProduct(): Promise<void> {
  if (saving.value) return
  if (!validForm()) {
    appStore.showError(t('commerce.shelf.invalid'))
    return
  }
  saving.value = true
  try {
    const payload: CurrencyProductInput = {
      ...form,
      name: form.name.trim(),
      description: form.description.trim(),
      daily_purchase_limit: normalizedPurchaseLimit(form.daily_purchase_limit),
      total_purchase_limit: normalizedPurchaseLimit(form.total_purchase_limit),
    }
    if (editingProduct.value) await adminPaymentAPI.updateCurrencyProduct(editingProduct.value.id, payload)
    else await adminPaymentAPI.createCurrencyProduct(payload)
    appStore.showSuccess(t('commerce.shelf.saved'))
    showEditor.value = false
    editingProduct.value = null
    await loadProducts()
  } catch (error) {
    console.error('Failed to save currency product:', error)
    appStore.showError(t('commerce.shelf.saveFailed'))
  } finally {
    saving.value = false
  }
}

function confirmDelete(product: CurrencyProduct): void {
  deletingProduct.value = product
}

async function deleteProduct(): Promise<void> {
  if (!deletingProduct.value) return
  const id = deletingProduct.value.id
  try {
    await adminPaymentAPI.deleteCurrencyProduct(id)
    appStore.showSuccess(t('commerce.shelf.deleted'))
    deletingProduct.value = null
    await loadProducts()
  } catch (error) {
    console.error('Failed to delete currency product:', error)
    appStore.showError(t('commerce.shelf.deleteFailed'))
  }
}

onMounted(loadProducts)
</script>
