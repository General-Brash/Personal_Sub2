<template>
  <div class="card overflow-hidden p-0">
    <button
      type="button"
      data-testid="plaza-admin-toggle"
      class="flex w-full items-center justify-between px-5 py-3 text-sm font-semibold text-gray-900 hover:bg-gray-50 dark:text-white dark:hover:bg-dark-800/50"
      @click="toggle"
    >
      <span>{{ locale === 'zh' ? '广场管理（管理员）' : 'Plaza management (admin)' }}</span>
      <span class="text-xs text-gray-400">{{ expanded ? '▲' : '▼' }}</span>
    </button>

    <div v-if="expanded" class="space-y-4 border-t border-gray-100 px-5 py-4 dark:border-dark-700/50">
      <template v-if="canReadDisplay">
      <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200">
        <input v-model="draftHideNoAccount" data-testid="plaza-admin-hide-no-account" :disabled="!canWriteDisplay || saving" type="checkbox" class="mt-0.5" />
        <span>
          {{ locale === 'zh' ? '隐藏“无任何账号支持”的模型（收敛幽灵模型）' : 'Hide models with no supporting account' }}
          <span class="block text-xs text-gray-500 dark:text-gray-400">
            {{ locale === 'zh' ? '限流/过载等临时不可调度仍会展示。' : 'Rate-limited / overloaded models are still shown.' }}
          </span>
        </span>
      </label>

      <div v-if="loading" class="py-4 text-center text-sm text-gray-500">{{ locale === 'zh' ? '加载中…' : 'Loading…' }}</div>
      <div v-else-if="error" class="py-4 text-center text-sm text-red-600">{{ locale === 'zh' ? '加载失败' : 'Failed to load' }}</div>
      <template v-else>
        <div class="grid max-h-96 grid-cols-1 items-start gap-2 overflow-y-auto pr-1 md:grid-cols-2 xl:grid-cols-3" data-testid="plaza-admin-grid">
          <div
            v-for="m in draftModels"
            :key="m.key"
            class="min-w-0 rounded-lg border border-gray-200 p-2.5 text-sm dark:border-dark-700"
          >
            <div class="flex min-w-0 flex-wrap items-start justify-between gap-1">
              <div class="min-w-0">
                <p class="break-words font-medium text-gray-900 dark:text-white" :title="m.display_name || m.model_id">{{ m.display_name || m.model_id }}</p>
                <p class="break-all font-mono text-xs text-gray-500 dark:text-gray-400" :title="m.model_id">{{ m.model_id }} · {{ m.platform }}</p>
              </div>
              <span :class="stateClass(m.availability_state)" class="shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium">{{ stateLabel(m.availability_state) }}</span>
            </div>
            <div class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-2">
            <label class="flex items-center gap-1 text-xs text-gray-600 dark:text-gray-300">
              <input v-model="m.hidden" :data-testid="`plaza-admin-hidden-${m.model_id}`" :disabled="!canWriteDisplay || saving" type="checkbox" /> {{ locale === 'zh' ? '隐藏' : 'Hide' }}
            </label>
            <label class="flex items-center gap-1 text-xs text-gray-600 dark:text-gray-300">
              <input v-model="m.pinned" :disabled="!canWriteDisplay || saving" type="checkbox" /> {{ locale === 'zh' ? '置顶' : 'Pin' }}
            </label>
            <input
              v-model.number="m.sort_order"
              :disabled="!canWriteDisplay || saving"
              type="number"
              class="w-16 rounded border border-gray-300 px-2 py-1 text-xs dark:border-dark-600 dark:bg-dark-800"
              :aria-label="locale === 'zh' ? '排序' : 'Sort order'"
            />
            <button v-if="canReadPricing" type="button" class="text-xs font-medium text-primary-600 hover:underline dark:text-primary-400" :data-testid="`plaza-pricing-edit-${m.model_id}`" @click="openPricing(m.model_id)">{{ t(m.has_exact_pricing_standard ? 'modelPlaza.defaultPricing.edit' : 'modelPlaza.defaultPricing.add') }}</button>
            <router-link to="/admin/groups" class="shrink-0 text-xs font-medium text-primary-600 hover:underline dark:text-primary-400">
              {{ locale === 'zh' ? '配置分组覆盖价' : 'Configure group override' }}
            </router-link>
            </div>
          </div>
        </div>

        <div class="flex flex-wrap items-center gap-3">
          <button
            type="button"
            v-if="canWriteDisplay"
            data-testid="plaza-admin-save"
            class="btn-primary px-4 py-1.5 text-sm"
            :disabled="saving"
            @click="save"
          >
            {{ saving ? (locale === 'zh' ? '保存中…' : 'Saving…') : (locale === 'zh' ? '保存' : 'Save') }}
          </button>
          <span v-if="saveOk" class="text-xs text-green-600 dark:text-green-400">
            {{ locale === 'zh' ? '已保存，前台刷新后生效。' : 'Saved. Refresh the plaza to see changes.' }}
          </span>
          <span v-if="saveError" class="text-xs text-red-600 dark:text-red-400">{{ saveError }}</span>
        </div>
      </template>
      </template>
      <section v-if="canReadPricing" class="space-y-3 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="plaza-pricing-section">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <h3 class="text-sm font-semibold">{{ t('modelPlaza.defaultPricing.listTitle') }}</h3>
          <button v-if="canWritePricing" type="button" class="btn-primary text-sm" data-testid="plaza-pricing-add" @click="openPricing()">{{ t('modelPlaza.defaultPricing.add') }}</button>
        </div>
        <p class="text-xs text-gray-500">{{ t('modelPlaza.defaultPricing.capabilityNotice') }}</p>
        <form class="flex gap-2" @submit.prevent="pricingPage = 1; loadPricingList()">
          <input v-model="pricingSearch" type="search" class="input min-w-0 flex-1" maxlength="256" :placeholder="t('modelPlaza.defaultPricing.search')" />
          <button type="submit" class="btn-secondary" :disabled="pricingLoading">{{ t('modelPlaza.defaultPricing.searchButton') }}</button>
        </form>
        <p v-if="pricingListError" role="alert" class="text-sm text-red-600">{{ pricingListError }}</p>
        <p v-if="pricingLoading" class="text-sm text-gray-500">{{ t('modelPlaza.defaultPricing.loading') }}</p>
        <ul v-else class="space-y-2">
          <li v-for="item in pricingItems" :key="item.model_id" class="flex flex-wrap items-center justify-between gap-2 rounded border border-gray-200 p-2 text-sm dark:border-dark-700">
            <div class="min-w-0"><code class="break-all">{{ item.model_id }}</code><span class="ml-2 text-xs text-gray-500">{{ item.override.billing_mode || t('modelPlaza.defaultPricing.inherit') }}</span>
              <p v-if="canReadDisplay && !loading && !error && !draftModels.some(m => m.model_id.toLowerCase() === item.model_id)" class="text-xs text-amber-700 dark:text-amber-300">{{ t('modelPlaza.defaultPricing.unconnected') }}</p>
            </div>
            <button type="button" class="text-xs text-primary-600 hover:underline" @click="openPricing(item.model_id)">{{ t('modelPlaza.defaultPricing.edit') }}</button>
          </li>
        </ul>
        <p v-if="!pricingLoading && !pricingItems.length && !pricingListError" class="text-sm text-gray-500">{{ t('modelPlaza.defaultPricing.empty') }}</p>
        <div class="flex items-center justify-end gap-2 text-xs">
          <button type="button" class="btn-secondary" :disabled="pricingPage <= 1 || pricingLoading" @click="pricingPage--; loadPricingList()">{{ t('modelPlaza.defaultPricing.previous') }}</button>
          <span>{{ pricingPage }} · {{ pricingTotal }}</span>
          <button type="button" class="btn-secondary" :disabled="pricingPage * 20 >= pricingTotal || pricingLoading" @click="pricingPage++; loadPricingList()">{{ t('modelPlaza.defaultPricing.next') }}</button>
        </div>
      </section>
    </div>
    <ModelDefaultPricingDialog :show="pricingDialogOpen" :model-id="editingModel" @close="pricingDialogOpen = false" @saved="onPricingSaved" />
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useAuthStore } from '@/stores/auth'
import ModelDefaultPricingDialog from './ModelDefaultPricingDialog.vue'
import { useI18n } from 'vue-i18n'
import {
  getModelPlazaAdmin,
  updateModelPlazaAdmin,
  listModelDefaultPricing,
  type DefaultPricingOverrideList,
  type ModelDefaultPricingDetail,
  type ModelPlazaAdminModel,
  type ModelPlazaAdminSettings,
} from '@/api/admin/modelPlaza'
import type { ModelPlazaAvailabilityState } from '@/api/modelPlaza'

const { locale, t } = useI18n()
const emit = defineEmits<{ 'pricing-saved': [] }>()
const auth = useAuthStore()
const canReadDisplay = computed(() => auth.canAdmin('models.catalog.read'))
const canWriteDisplay = computed(() => auth.canAdmin('models.catalog.write'))
const canReadPricing = computed(() => auth.canAdmin('models.pricing.read'))
const canWritePricing = computed(() => auth.canAdmin('models.pricing.write'))
const pricingDialogOpen = ref(false)
const editingModel = ref<string>()
const pricingItems = ref<DefaultPricingOverrideList['items']>([])
const pricingSearch = ref('')
const pricingPage = ref(1)
const pricingTotal = ref(0)
const pricingLoading = ref(false)
const pricingListError = ref('')
const pricingLoaded = ref(false)
function openPricing(model?: string) { editingModel.value = model; pricingDialogOpen.value = true }
async function loadPricingList(afterSave = false) {
  pricingLoading.value = true; pricingListError.value = ''
  try {
    const result = await listModelDefaultPricing(pricingSearch.value, pricingPage.value)
    pricingItems.value = result.items ?? []; pricingTotal.value = result.total; pricingLoaded.value = true
  } catch {
    pricingListError.value = t(afterSave ? 'modelPlaza.defaultPricing.savedRefreshFailed' : 'modelPlaza.defaultPricing.listFailed')
  } finally { pricingLoading.value = false }
}
async function onPricingSaved(detail: ModelDefaultPricingDetail) {
  // Update only pricing metadata; display draft fields and display version remain untouched.
  for (const model of draftModels.value) {
    if (model.model_id.toLowerCase() === detail.requested_model_id.trim().toLowerCase()) model.has_exact_pricing_standard = detail.has_admin_override || detail.has_exact_system_standard
  }
  emit('pricing-saved')
  await loadPricingList(true)
}

const expanded = ref(false)
const loading = ref(false)
const error = ref(false)
const saving = ref(false)
const saveOk = ref(false)
const saveError = ref('')
const version = ref('')
const draftHideNoAccount = ref(true)
const draftModels = ref<ModelPlazaAdminModel[]>([])

function applySettings(s: ModelPlazaAdminSettings) {
  version.value = s.version
  draftHideNoAccount.value = s.hide_no_account
  draftModels.value = (s.models ?? []).map((m) => ({ ...m }))
}

async function load() {
  loading.value = true
  error.value = false
  saveOk.value = false
  saveError.value = ''
  try {
    applySettings(await getModelPlazaAdmin())
  } catch {
    error.value = true
  } finally {
    loading.value = false
  }
}

function toggle() {
  expanded.value = !expanded.value
  if (expanded.value && canReadDisplay.value && !draftModels.value.length && !loading.value) void load()
  if (expanded.value && canReadPricing.value && !pricingLoaded.value && !pricingLoading.value) void loadPricingList()
}

async function save() {
  if (!canWriteDisplay.value) return
  saving.value = true
  saveOk.value = false
  saveError.value = ''
  const overrides: Record<string, { hidden: boolean; pinned: boolean; sort_order: number }> = {}
  for (const m of draftModels.value) {
    // 只回传有实际覆盖的模型，零值项等价“无覆盖”，避免存储膨胀。
    if (m.hidden || m.pinned || m.sort_order !== 0) {
      overrides[m.key] = { hidden: m.hidden, pinned: m.pinned, sort_order: m.sort_order || 0 }
    }
  }
  try {
    applySettings(await updateModelPlazaAdmin({ overrides, hide_no_account: draftHideNoAccount.value, version: version.value }))
    saveOk.value = true
  } catch {
    // 版本乐观锁冲突或其它失败：拉取最新状态，让管理员在最新版本上重试。
    saveError.value = locale.value === 'zh' ? '保存失败，配置可能已更新，已刷新为最新，请重试。' : 'Save failed; reloaded latest settings, please retry.'
    await load()
  } finally {
    saving.value = false
  }
}

function stateLabel(state: ModelPlazaAvailabilityState): string {
  const zh = locale.value === 'zh'
  switch (state) {
    case 'eligible':
      return zh ? '可用' : 'Eligible'
    case 'temporarily_unavailable':
      return zh ? '暂不可调度' : 'Unavailable'
    case 'not_entitled':
      return zh ? '未授权' : 'Not entitled'
    case 'catalog_only':
      return zh ? '仅目录' : 'Catalog only'
    default:
      return zh ? '未知' : 'Unknown'
  }
}

function stateClass(state: ModelPlazaAvailabilityState): string {
  switch (state) {
    case 'eligible':
      return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
    case 'temporarily_unavailable':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
    case 'not_entitled':
      return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
    default:
      return 'bg-gray-100 text-gray-500 dark:bg-dark-700 dark:text-gray-400'
  }
}
</script>
