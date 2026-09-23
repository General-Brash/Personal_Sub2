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
      <label class="flex items-start gap-2 text-sm text-gray-700 dark:text-gray-200">
        <input v-model="draftHideNoAccount" data-testid="plaza-admin-hide-no-account" type="checkbox" class="mt-0.5" />
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
        <div class="max-h-96 space-y-2 overflow-y-auto pr-1">
          <div
            v-for="m in draftModels"
            :key="m.key"
            class="flex flex-wrap items-center gap-3 rounded-lg border border-gray-200 p-2.5 text-sm dark:border-dark-700"
          >
            <div class="min-w-0 flex-1">
              <p class="truncate font-medium text-gray-900 dark:text-white">{{ m.display_name || m.model_id }}</p>
              <p class="truncate font-mono text-xs text-gray-500 dark:text-gray-400">{{ m.model_id }} · {{ m.platform }}</p>
            </div>
            <span :class="stateClass(m.availability_state)" class="shrink-0 rounded-full px-2 py-0.5 text-[11px] font-medium">
              {{ stateLabel(m.availability_state) }}
            </span>
            <label class="flex items-center gap-1 text-xs text-gray-600 dark:text-gray-300">
              <input v-model="m.hidden" :data-testid="`plaza-admin-hidden-${m.model_id}`" type="checkbox" /> {{ locale === 'zh' ? '隐藏' : 'Hide' }}
            </label>
            <label class="flex items-center gap-1 text-xs text-gray-600 dark:text-gray-300">
              <input v-model="m.pinned" type="checkbox" /> {{ locale === 'zh' ? '置顶' : 'Pin' }}
            </label>
            <input
              v-model.number="m.sort_order"
              type="number"
              class="w-16 rounded border border-gray-300 px-2 py-1 text-xs dark:border-dark-600 dark:bg-dark-800"
              :aria-label="locale === 'zh' ? '排序' : 'Sort order'"
            />
            <router-link to="/admin/groups" class="shrink-0 text-xs font-medium text-primary-600 hover:underline dark:text-primary-400">
              {{ locale === 'zh' ? '去配置定价' : 'Configure pricing' }}
            </router-link>
          </div>
        </div>

        <div class="flex flex-wrap items-center gap-3">
          <button
            type="button"
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
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  getModelPlazaAdmin,
  updateModelPlazaAdmin,
  type ModelPlazaAdminModel,
  type ModelPlazaAdminSettings,
} from '@/api/admin/modelPlaza'
import type { ModelPlazaAvailabilityState } from '@/api/modelPlaza'

const { locale } = useI18n()

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
  if (expanded.value && !draftModels.value.length && !loading.value) void load()
}

async function save() {
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
