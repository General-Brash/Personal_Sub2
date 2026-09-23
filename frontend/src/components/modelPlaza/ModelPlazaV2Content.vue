<template>
  <div class="space-y-5">
    <!-- 全局价格说明(管理员配置,Markdown;与 legacy 广场同源) -->
    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-2xl border border-gray-100 bg-white px-5 py-4 text-sm shadow-card dark:border-dark-700/50 dark:bg-dark-800/50"
      v-html="descriptionHtml"
    ></div>
    <ModelPlazaAdminPanel v-if="authStore.isAdmin" />
    <div v-if="loading" class="card p-6 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ locale === 'zh' ? '正在加载模型目录…' : 'Loading model catalog…' }}
    </div>
    <div v-else-if="error" class="card p-6 text-center text-sm text-red-600 dark:text-red-400">
      {{ locale === 'zh' ? '模型目录加载失败。' : 'Failed to load the model catalog.' }}
    </div>
    <div v-else-if="!models.length" class="card p-6 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ locale === 'zh' ? '暂无可用模型。' : 'No models are available.' }}
    </div>
    <template v-else>
      <!-- V2 的倍率属于 choice，而非 group；各维度按实际 choice 联动。 -->
      <div class="space-y-2 text-sm" data-testid="plaza-v2-filters">
        <div class="flex flex-wrap items-center gap-2">
          <span class="w-10 shrink-0 text-xs text-gray-500">{{ locale === 'zh' ? '平台' : 'Platform' }}</span>
          <button v-for="platform in ['all', ...platforms]" :key="platform" type="button" class="rounded-lg px-2 py-1 ring-1 ring-gray-200 disabled:opacity-40 dark:ring-dark-600" :class="selectedPlatform === platform ? 'bg-primary-600 text-white' : 'dark:text-gray-200'" :disabled="platform !== 'all' && !hasChoice(platform, selectedGroupId, selectedRate)" @click="selectedPlatform = platform">{{ platform === 'all' ? (locale === 'zh' ? '全部' : 'All') : platform }}</button>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="w-10 shrink-0 text-xs text-gray-500">{{ locale === 'zh' ? '分组' : 'Group' }}</span>
          <button type="button" class="rounded-lg px-2 py-1 ring-1 ring-gray-200 dark:ring-dark-600" :class="selectedGroupId === 'all' ? 'bg-primary-600 text-white' : 'dark:text-gray-200'" @click="selectedGroupId = 'all'">{{ locale === 'zh' ? '全部' : 'All' }}</button>
          <button v-for="group in groupOptions" :key="group.id" type="button" class="rounded-lg px-2 py-1 ring-1 ring-gray-200 disabled:opacity-40 dark:ring-dark-600" :class="selectedGroupId === group.id ? 'bg-primary-600 text-white' : 'dark:text-gray-200'" :disabled="!hasChoice(selectedPlatform, group.id, selectedRate)" @click="selectedGroupId = group.id">{{ group.name }} · {{ group.platform }}</button>
        </div>
        <div class="flex flex-wrap items-center gap-2">
          <span class="w-10 shrink-0 text-xs text-gray-500">{{ locale === 'zh' ? '倍率' : 'Rate' }}</span>
          <button type="button" class="rounded-lg px-2 py-1 ring-1 ring-gray-200 dark:ring-dark-600" :class="selectedRate === 'all' ? 'bg-primary-600 text-white' : 'dark:text-gray-200'" @click="selectedRate = 'all'">{{ locale === 'zh' ? '全部' : 'All' }}</button>
          <button v-for="rate in rates" :key="rate" type="button" class="rounded-lg px-2 py-1 ring-1 ring-gray-200 disabled:opacity-40 dark:ring-dark-600" :class="selectedRate === rate ? 'bg-primary-600 text-white' : 'dark:text-gray-200'" :disabled="!hasChoice(selectedPlatform, selectedGroupId, rate)" @click="selectedRate = rate">{{ rate }}x</button>
        </div>
        <label class="flex items-center gap-2"><span class="w-10 shrink-0 text-xs text-gray-500">{{ locale === 'zh' ? '模型' : 'Model' }}</span><input v-model="searchQuery" type="search" class="input min-w-0 max-w-72 rounded-lg py-1.5" :placeholder="locale === 'zh' ? '搜索模型' : 'Search models'" /></label>
      </div>

      <div v-if="filteredModels.length" class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3" data-testid="plaza-v2-grid">
      <article v-for="model in filteredModels" :key="modelKey(model)" class="card min-w-0 p-3.5">
        <div class="flex min-w-0 flex-wrap items-start gap-2" data-testid="plaza-v2-card-header">
          <div class="flex min-w-0 flex-1 flex-wrap items-start gap-2">
            <div class="min-w-0 flex-[1_1_9rem]">
              <h2 class="break-words text-base font-semibold text-gray-900 dark:text-white">{{ model.display_name || model.model_id }}</h2>
              <p class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-gray-400">{{ model.model_id }} · {{ model.platform }}</p>
            </div>
            <div class="flex min-w-0 max-w-full flex-[1_1_9rem] flex-wrap gap-1" :aria-label="locale === 'zh' ? '可选分组' : 'Available groups'">
              <button v-for="item in matchingChoices(model)" :key="item.key" type="button" class="min-w-0 max-w-full break-words rounded-md border px-2 py-1 text-left text-[11px]" :class="selectedKey(model) === item.key ? 'border-primary-500 bg-primary-50 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300' : 'border-gray-200 text-gray-600 dark:border-dark-600 dark:text-gray-300'" :aria-pressed="selectedKey(model) === item.key" :aria-label="`${item.choice.group_name || `#${item.choice.group_id}`} #${item.choice.group_id} · ${item.choice.platform} · ${item.choice.route_kind} · ${choiceStatus(item.choice)}${item.total > 1 ? (locale === 'zh' ? ` · 重复路由第 ${item.ordinal + 1} 项，共 ${item.total} 项` : ` · duplicate route ${item.ordinal + 1} of ${item.total}`) : ''}`" :title="`${item.choice.group_name || `#${item.choice.group_id}`} · ${item.choice.platform} · ${item.choice.route_kind}`" @click="selectChoice(model, item.key)">{{ item.choice.group_name || `#${item.choice.group_id}` }} <span v-if="item.choice.group_name">#{{ item.choice.group_id }}</span> · {{ item.choice.route_kind }}<span v-if="item.total > 1"> · {{ item.ordinal + 1 }}/{{ item.total }}</span><span v-if="item.choice.platform !== model.platform"> · {{ item.choice.platform }}</span><span v-if="item.choice.availability_state !== 'eligible' || !item.choice.schedulable" class="font-medium text-amber-700 dark:text-amber-300"> · {{ choiceStatus(item.choice) }}</span></button>
              <span v-if="!matchingChoices(model).length" class="text-xs text-amber-700">{{ locale === 'zh' ? '无可选分组' : 'No available group' }}</span>
            </div>
          </div>
          <span :class="stateClass(model.availability_state)" class="shrink-0 rounded-full px-2.5 py-1 text-xs font-medium">
            {{ stateLabel(model.availability_state) }}
          </span>
        </div>

        <div v-if="model.capabilities?.length" class="mt-2 flex flex-wrap gap-1">
          <span v-for="capability in model.capabilities" :key="capability" class="rounded bg-gray-100 px-2 py-1 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ capability }}</span>
        </div>

        <p v-if="resetNotices[modelKey(model)]" role="status" aria-live="polite" class="mt-2 text-xs text-amber-700 dark:text-amber-300">{{ locale === 'zh' ? '路由列表已更新；重复项无法稳定区分，已恢复默认，请重新选择。' : 'Routes changed; duplicate choices cannot be tracked reliably. Default restored; please choose again.' }}</p>
        <p v-if="isSampleLowest(model)" class="mt-2 text-[11px] text-emerald-700 dark:text-emerald-300">{{ locale === 'zh' ? '可调度组样例估算最低：1M 输入 + 1M 输出（无缓存）或按次 1 次；实际按请求结算。' : 'Lowest schedulable sample estimate: 1M input + 1M output (no cache), or one request. Actual billing varies.' }}</p>
        <p v-else-if="isComparable(model)" class="mt-2 text-[11px] text-gray-500 dark:text-gray-400">{{ locale === 'zh' ? '手选分组；当前不是可调度组的样例估算最低，实际按请求结算。' : 'Manually selected; not the lowest schedulable sample estimate. Actual billing varies.' }}</p>
        <p v-else class="mt-2 text-[11px] text-amber-700 dark:text-amber-300">{{ locale === 'zh' ? '谨慎展示：报价不可完整比较，非最低价保证。' : 'Caution: quotes are not fully comparable; not a lowest-price guarantee.' }}</p>
        <div class="mt-2">
          <div v-if="selectedChoice(model)" class="min-w-0 rounded-lg border border-gray-200 p-2.5 dark:border-dark-700" data-testid="plaza-v2-quote">
            <template v-for="choice in [selectedChoice(model)!]" :key="`${selectedKey(model)}:${choice.group_id}:${choice.route_kind}`">
            <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
              <div class="min-w-0">
                <p class="break-words text-sm font-medium text-gray-900 dark:text-white">{{ choice.group_name || `#${choice.group_id}` }}</p>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ choice.platform }} · {{ choice.route_kind }}</p>
              </div>
              <span :class="stateClass(choice.availability_state)" class="rounded-full px-2 py-0.5 text-[11px] font-medium">{{ stateLabel(choice.availability_state) }}</span>
            </div>

            <p v-if="choice.price_quote" class="mt-2 text-[11px] text-gray-500 dark:text-gray-400">{{ choice.price_quote.currency }} · {{ choice.price_quote.pricing_unit }}</p>
            <p v-if="choice.price_quote" class="mt-2 text-xs text-gray-500">{{ locale === 'zh' ? '基准单价，需叠加下方倍率；最终账单以请求快照为准。' : 'Base unit prices; factors below apply. The request snapshot determines billing.' }}</p>
            <dl v-if="choice.price_quote" class="mt-3 grid grid-cols-2 gap-x-4 gap-y-2 text-xs">
              <div v-if="choice.price_quote.input_per_million != null">
                <dt class="text-gray-500 dark:text-gray-400">{{ locale === 'zh' ? '输入 / 1M' : 'Input / 1M' }}</dt>
                <dd class="font-mono font-medium text-gray-900 dark:text-white">${{ formatPrice(choice.price_quote.input_per_million) }}</dd>
              </div>
              <div v-if="choice.price_quote.output_per_million != null">
                <dt class="text-gray-500 dark:text-gray-400">{{ locale === 'zh' ? '输出 / 1M' : 'Output / 1M' }}</dt>
                <dd class="font-mono font-medium text-gray-900 dark:text-white">${{ formatPrice(choice.price_quote.output_per_million) }}</dd>
              </div>
              <div v-if="choice.price_quote.cache_read_per_million != null">
                <dt class="text-gray-500 dark:text-gray-400">{{ locale === 'zh' ? '缓存读取 / 1M' : 'Cache read / 1M' }}</dt>
                <dd class="font-mono font-medium text-gray-900 dark:text-white">${{ formatPrice(choice.price_quote.cache_read_per_million) }}</dd>
              </div>
              <div v-if="choice.price_quote.cache_write_per_million != null"><dt class="text-gray-500">{{locale === 'zh' ? '缓存写入 / 1M' : 'Cache write / 1M'}}</dt><dd class="font-mono">${{formatPrice(choice.price_quote.cache_write_per_million)}}</dd></div>
              <div v-if="choice.price_quote.cache_write_1h_per_million != null"><dt class="text-gray-500">{{locale === 'zh' ? '缓存写入 1h / 1M' : '1h cache write / 1M'}}</dt><dd class="font-mono">${{formatPrice(choice.price_quote.cache_write_1h_per_million)}}</dd></div>
              <div v-if="choice.price_quote.per_request_price != null">
                <dt class="text-gray-500 dark:text-gray-400">{{ locale === 'zh' ? '按次价格' : 'Per request' }}</dt>
                <dd class="font-mono font-medium text-gray-900 dark:text-white">${{ formatPrice(choice.price_quote.per_request_price) }}</dd>
              </div>
            </dl>
            <p v-else class="mt-3 text-xs font-medium text-amber-700 dark:text-amber-300">{{ locale === 'zh' ? '报价未知，不显示 0 元。' : 'Quote unknown; zero is not shown.' }}</p>
            <div v-if="choice.price_quote?.intervals?.length" class="mt-3 space-y-1 rounded bg-gray-50 p-2 text-xs dark:bg-dark-700">
              <div v-for="(interval,index) in choice.price_quote.intervals" :key="index" class="flex flex-wrap justify-between gap-2">
                <span>{{interval.tier_label || `${interval.min_tokens ?? 0}–${interval.max_tokens ?? '∞'} tokens`}}</span>
                <span v-if="interval.input_per_million != null">{{locale==='zh'?'输入':'Input'}} ${{formatPrice(interval.input_per_million)}} / 1M</span>
                <span v-if="interval.output_per_million != null">{{locale==='zh'?'输出':'Output'}} ${{formatPrice(interval.output_per_million)}} / 1M</span>
                <span v-if="interval.per_request_price != null">${{formatPrice(interval.per_request_price)}} / request</span>
              </div>
            </div>
            <p v-if="choice.price_quote?.unknown_fields?.length" class="mt-2 text-xs text-amber-700">{{locale==='zh'?'未确定字段：':'Unknown fields: '}}{{choice.price_quote.unknown_fields.join(', ')}}</p>
            <p v-if="choice.price_quote && !['not_configured', 'available'].includes(choice.price_quote.dynamic_factor_status)" class="mt-2 text-xs text-amber-700">{{ locale === 'zh' ? '动态因子状态' : 'Dynamic factor status' }}: {{ choice.price_quote.dynamic_factor_status }}</p>
            <div v-if="choice.price_quote?.dynamic_factor && !choice.price_quote.dynamic_factor.details?.tier_id" class="mt-2 text-xs text-gray-600 dark:text-gray-300">{{ locale === 'zh' ? '动态因子' : 'Dynamic factor' }} ×{{ formatMultiplier(choice.price_quote.dynamic_factor.factor) }}</div>
            <div v-if="choice.price_quote?.dynamic_factor?.details?.tier_id" class="mt-3 rounded bg-gray-50 p-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
              当前档 {{ choice.price_quote.dynamic_factor.details.tier_id }} · 累计 {{ choice.price_quote.dynamic_factor.details.current }} {{ choice.price_quote.dynamic_factor.details.metric === 'tokens_m' ? 'tokens' : 'USD' }} · 动态 ×{{ formatMultiplier(choice.price_quote.dynamic_factor.factor) }}
              <span v-if="choice.price_quote.dynamic_factor.details.next_threshold"> · 下一档 {{ choice.price_quote.dynamic_factor.details.next_threshold }}</span>
              <span v-if="choice.price_quote.dynamic_factor.details.reset_at" class="block">重置 {{ formatDate(choice.price_quote.dynamic_factor.details.reset_at) }}；已接纳请求不回算。</span>
            </div>
            <div v-if="choice.price_quote?.effective_rate_multiplier != null || choice.price_quote?.channel_time_multiplier != null || choice.price_quote?.image_rate_independent || choice.price_quote?.rate_source" class="mt-3 flex flex-wrap gap-2 text-[11px]">
              <span v-if="choice.price_quote?.effective_rate_multiplier != null" class="rounded bg-primary-50 px-2 py-1 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                {{ locale === 'zh' ? '生效倍率' : 'Effective rate' }} ×{{ formatMultiplier(choice.price_quote.effective_rate_multiplier) }}
              </span>
              <span v-if="rateSourceLabel(choice.price_quote?.rate_source)" class="rounded bg-gray-100 px-2 py-1 text-gray-600 dark:bg-dark-700 dark:text-gray-300">
                {{ locale === 'zh' ? '来源' : 'Source' }}: {{ rateSourceLabel(choice.price_quote?.rate_source) }}
              </span>
              <span v-if="choice.price_quote?.peak_rate_multiplier != null" class="rounded bg-primary-50 px-2 py-1 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{locale==='zh'?'高峰因子':'Peak factor'}} ×{{formatMultiplier(choice.price_quote.peak_rate_multiplier)}}</span>
              <span v-if="choice.price_quote?.channel_time_multiplier != null" class="rounded bg-cyan-50 px-2 py-1 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300">
                {{ locale === 'zh' ? '渠道分时' : 'Channel time' }} ×{{ formatMultiplier(choice.price_quote.channel_time_multiplier) }}
              </span>
              <span v-if="choice.price_quote?.image_rate_independent" class="rounded bg-violet-50 px-2 py-1 text-violet-700 dark:bg-violet-900/30 dark:text-violet-300">
                {{ locale === 'zh' ? '图片独立倍率' : 'Independent image rate' }} ×{{ formatMultiplier(choice.price_quote.image_rate_multiplier ?? choice.price_quote.effective_rate_multiplier ?? NaN) }}
              </span>
            </div>
            <div v-if="choice.price_quote?.price_conditions?.length" class="mt-3 rounded-md border border-amber-200 bg-amber-50 p-2.5 dark:border-amber-500/30 dark:bg-amber-500/10">
              <p class="text-[11px] font-medium text-amber-800 dark:text-amber-200">{{ locale === 'zh' ? '同组存在多个匹配价，实际按条件生效：' : 'Multiple matching prices; the applied condition depends on the route:' }}</p>
              <ul class="mt-1.5 space-y-1">
                <li v-for="condition in choice.price_quote.price_conditions" :key="condition.pattern" class="flex min-w-0 flex-wrap items-center justify-between gap-2 text-[11px] text-amber-900 dark:text-amber-100">
                  <code class="break-all font-mono">{{ condition.pattern }}</code>
                  <span class="font-mono">{{ conditionSummary(condition) }}</span>
                </li>
              </ul>
            </div>
            <p v-if="choice.price_quote?.priced_at" class="mt-2 break-all text-[11px] text-gray-400 dark:text-gray-500">
              {{ locale === 'zh' ? '报价时间' : 'Priced at' }}: {{ formatDate(choice.price_quote.priced_at) }}
            </p>
            </template>
          </div>
          <p v-else class="text-xs text-amber-700">{{ locale === 'zh' ? '报价未知。' : 'Quote unknown.' }}</p>
        </div>
      </article>
      </div>
      <div v-else class="card p-6 text-center text-sm text-gray-500 dark:text-gray-400">
        {{ locale === 'zh' ? '筛选后无匹配模型。' : 'No models match the current filters.' }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import ModelPlazaAdminPanel from './ModelPlazaAdminPanel.vue'
import { useAuthStore } from '@/stores/auth'
import type { ModelPlazaAvailabilityState, ModelPlazaV2GroupChoice, ModelPlazaV2Model, ModelPlazaV2PriceCondition, ModelPlazaV2Response } from '@/api/modelPlaza'

const authStore = useAuthStore()

const props = defineProps<{
  response: ModelPlazaV2Response | null
  loading: boolean
  error: boolean
}>()

const { locale } = useI18n()
const models = computed(() => props.response?.models ?? [])

const selectedPlatform = ref<string>('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')

// 全局价格说明:与 legacy 广场同源(marked + DOMPurify)。
const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

type ChoiceItem = { key: string; choice: ModelPlazaV2GroupChoice; ordinal: number; total: number }
function choiceIdentity(choice: ModelPlazaV2GroupChoice): string { return JSON.stringify([choice.platform, choice.group_id, choice.route_kind]) }
function modelKey(model: ModelPlazaV2Model): string { return JSON.stringify([model.platform, model.model_id]) }
function choiceItems(model: ModelPlazaV2Model): ChoiceItem[] {
  const occurrences = new Map<string, number>()
  const choices = model.user_group_choices ?? []
  const totals = new Map<string, number>()
  for (const choice of choices) {
    const identity = choiceIdentity(choice)
    totals.set(identity, (totals.get(identity) ?? 0) + 1)
  }
  return choices.map((choice) => {
    const identity = choiceIdentity(choice)
    const ordinal = occurrences.get(identity) ?? 0
    occurrences.set(identity, ordinal + 1)
    return { key: JSON.stringify([identity, ordinal]), choice, ordinal, total: totals.get(identity)! }
  })
}
function choiceRate(choice: ModelPlazaV2GroupChoice): number | null {
  const quote = choice.price_quote
  const rate = quote?.image_rate_independent ? quote.image_rate_multiplier : quote?.effective_rate_multiplier
  return rate != null && Number.isFinite(rate) ? rate : null
}
const allChoices = computed(() => models.value.flatMap((model) => model.user_group_choices ?? []))
const groupOptions = computed(() => {
  const map = new Map<number, { id: number; name: string; platform: string }>()
  for (const choice of allChoices.value) if (!map.has(choice.group_id)) {
    map.set(choice.group_id, { id: choice.group_id, name: choice.group_name || `#${choice.group_id}`, platform: choice.platform })
  }
  return [...map.values()]
})
const platforms = computed(() => [...new Set(allChoices.value.map((choice) => choice.platform).filter(Boolean))].sort())
const rates = computed(() => [...new Set(allChoices.value.map(choiceRate).filter((rate): rate is number => rate !== null))].sort((a, b) => a - b))
function hasChoice(platform: string, group: number | 'all', rate: number | 'all'): boolean {
  return allChoices.value.some((choice) =>
    (platform === 'all' || choice.platform === platform) &&
    (group === 'all' || choice.group_id === group) &&
    (rate === 'all' || choiceRate(choice) === rate))
}
watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) selectedRate.value = 'all'
})
watch(groupOptions, (list) => {
  if (selectedGroupId.value !== 'all' && !list.some((g) => g.id === selectedGroupId.value)) selectedGroupId.value = 'all'
})
watch(platforms, (list) => {
  if (selectedPlatform.value !== 'all' && !list.includes(selectedPlatform.value)) selectedPlatform.value = 'all'
})
function choiceMatches(choice: ModelPlazaV2GroupChoice): boolean {
  return (selectedPlatform.value === 'all' || choice.platform === selectedPlatform.value) &&
    (selectedGroupId.value === 'all' || choice.group_id === selectedGroupId.value) &&
    (selectedRate.value === 'all' || choiceRate(choice) === selectedRate.value)
}
function matchingChoices(model: ModelPlazaV2Model): ChoiceItem[] { return choiceItems(model).filter((item) => choiceMatches(item.choice)) }
const filteredModels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  return models.value.filter((model) =>
    (matchingChoices(model).length > 0 || (!(model.user_group_choices ?? []).length && selectedPlatform.value === 'all' && selectedGroupId.value === 'all' && selectedRate.value === 'all')) &&
    (!q || (model.display_name || '').toLowerCase().includes(q) || model.model_id.toLowerCase().includes(q)))
})
function validAmount(value: number | null | undefined): value is number {
  return value != null && Number.isFinite(value) && value >= 0
}
function knownQuote(choice: ModelPlazaV2GroupChoice): boolean {
  const q = choice.price_quote
  return !!q && (validAmount(q.per_request_price) || validAmount(q.input_per_million) || validAmount(q.output_per_million))
}
function samplePrice(choice: ModelPlazaV2GroupChoice): { currency: string; unit: string; amount: number } | null {
  const q = choice.price_quote
  if (!q || !q.currency || q.intervals?.length || q.price_conditions?.length) return null
  // The backend lists unknown cache prices even for a no-cache sample. Reject every
  // other unknown field unless it belongs only to the other billing unit.
  const irrelevantFields = q.pricing_unit === 'per_request'
    ? ['input_per_million', 'output_per_million', 'cache_read_per_million', 'cache_write_per_million', 'cache_write_1h_per_million']
    : q.pricing_unit === 'per_1m_tokens'
      ? ['per_request_price', 'cache_read_per_million', 'cache_write_per_million', 'cache_write_1h_per_million']
      : []
  if (q.unknown_fields?.some((field) => !irrelevantFields.includes(field))) return null
  const rate = q.effective_rate_multiplier
  if (!validAmount(rate)) return null
  if (q.dynamic_factor_status !== 'not_configured' && q.dynamic_factor_status !== 'available') return null
  const dynamic = q.dynamic_factor_status === 'available' ? q.dynamic_factor?.factor : 1
  if (!validAmount(dynamic) || (q.peak_rate_multiplier != null && !validAmount(q.peak_rate_multiplier)) ||
    (q.channel_time_multiplier != null && !validAmount(q.channel_time_multiplier))) return null
  const base = q.pricing_unit === 'per_1m_tokens' && validAmount(q.input_per_million) && validAmount(q.output_per_million)
    ? q.input_per_million + q.output_per_million
    : q.pricing_unit === 'per_request' && validAmount(q.per_request_price) ? q.per_request_price : null
  if (base === null) return null
  const amount = base * rate * (q.peak_rate_multiplier ?? 1) * (q.channel_time_multiplier ?? 1) * dynamic
  return Number.isFinite(amount) ? { currency: q.currency, unit: q.pricing_unit, amount } : null
}
function defaultChoice(items: ChoiceItem[]): { key: string; comparable: boolean } {
  const eligible = items.filter(({ choice }) => choice.availability_state === 'eligible' && choice.schedulable)
  const candidates = eligible.length ? eligible : items
  const prices = candidates.map(({ choice }) => samplePrice(choice))
  if (prices.every((price) => price !== null) && prices.every((price) => price!.currency === prices[0]!.currency && price!.unit === prices[0]!.unit)) {
    const ranked = candidates.map((item, index) => ({ item, amount: prices[index]!.amount }))
      .sort((a, b) => a.amount - b.amount || a.item.choice.group_id - b.item.choice.group_id ||
        a.item.choice.route_kind.localeCompare(b.item.choice.route_kind) || a.item.key.localeCompare(b.item.key))
    return { key: ranked[0].item.key, comparable: eligible.length > 0 }
  }
  const fallback = items.find(({ choice }) => choice.schedulable && knownQuote(choice)) ?? items[0]
  return { key: fallback.key, comparable: false }
}
const selectedKeys = ref<Record<string, string>>({})
const resetNotices = ref<Record<string, boolean>>({})
watch([models, selectedPlatform, selectedGroupId, selectedRate], ([currentModels], [previousModels]) => {
  const refreshed = Array.isArray(previousModels) && currentModels !== previousModels
  const previousByKey = new Map((previousModels ?? []).map((model) => [modelKey(model), model]))
  const notices: Record<string, boolean> = {}
  const next: Record<string, string> = {}
  for (const model of models.value) {
    const items = matchingChoices(model)
    if (!items.length) continue
    const key = modelKey(model)
    const previous = previousByKey.get(key)
    const oldItem = previous && choiceItems(previous).find((item) => item.key === selectedKeys.value[key])
    const duplicateOnRefresh = refreshed && oldItem && (
      oldItem.total > 1 || items.some((item) => choiceIdentity(item.choice) === choiceIdentity(oldItem.choice) && item.total > 1)
    )
    next[key] = !duplicateOnRefresh && items.some((item) => item.key === selectedKeys.value[key])
      ? selectedKeys.value[key] : defaultChoice(items).key
    if (duplicateOnRefresh) notices[key] = true
    else if (!refreshed && resetNotices.value[key]) notices[key] = true
  }
  resetNotices.value = notices
  selectedKeys.value = next
}, { immediate: true, flush: 'sync' })
function selectedKey(model: ModelPlazaV2Model): string { return selectedKeys.value[modelKey(model)] ?? '' }
function selectedChoice(model: ModelPlazaV2Model): ModelPlazaV2GroupChoice | undefined {
  return matchingChoices(model).find((item) => item.key === selectedKey(model))?.choice
}
function selectChoice(model: ModelPlazaV2Model, key: string) {
  const id = modelKey(model)
  selectedKeys.value = { ...selectedKeys.value, [id]: key }
  if (resetNotices.value[id]) resetNotices.value = { ...resetNotices.value, [id]: false }
}
function isComparable(model: ModelPlazaV2Model): boolean {
  const items = matchingChoices(model)
  return items.length > 0 && defaultChoice(items).comparable
}
function isSampleLowest(model: ModelPlazaV2Model): boolean {
  const items = matchingChoices(model)
  return items.length > 0 && defaultChoice(items).comparable && selectedKey(model) === defaultChoice(items).key
}

function choiceStatus(choice: ModelPlazaV2GroupChoice): string {
  return choice.availability_state === 'eligible' && !choice.schedulable
    ? (locale.value === 'zh' ? '暂不可调度' : 'Unavailable')
    : stateLabel(choice.availability_state)
}

function stateLabel(state: ModelPlazaAvailabilityState): string {
  const zh: Record<ModelPlazaAvailabilityState, string> = {
    catalog_only: '仅目录展示',
    eligible: '可调用',
    temporarily_unavailable: '暂不可调度',
    not_entitled: '无调用资格',
    unknown: '状态未知'
  }
  const en: Record<ModelPlazaAvailabilityState, string> = {
    catalog_only: 'Catalog only',
    eligible: 'Eligible',
    temporarily_unavailable: 'Temporarily unavailable',
    not_entitled: 'Not entitled',
    unknown: 'Unknown'
  }
  return (locale.value === 'zh' ? zh : en)[state] ?? state
}

function stateClass(state: ModelPlazaAvailabilityState): string {
  if (state === 'eligible') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (state === 'temporarily_unavailable') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  if (state === 'not_entitled') return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  if (state === 'catalog_only') return 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
  return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
}

function formatPrice(value: number): string {
  return Number.isFinite(value) ? value.toLocaleString(undefined, { maximumFractionDigits: 8 }) : '—'
}

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function formatMultiplier(value: number): string {
  return Number.isFinite(value) ? value.toLocaleString(undefined, { maximumFractionDigits: 4 }) : '—'
}

// 生效倍率的来源层:用户覆盖 / 消费权益(tier) / 分组默认。留空或未知不展示徽标。
function rateSourceLabel(source: string | undefined): string {
  if (!source) return ''
  const zh: Record<string, string> = {
    user_override: '用户专属',
    entitlement_tier: '权益等级',
    group_default: '分组默认'
  }
  const en: Record<string, string> = {
    user_override: 'User override',
    entitlement_tier: 'Entitlement tier',
    group_default: 'Group default'
  }
  return (locale.value === 'zh' ? zh : en)[source] ?? source
}

function conditionSummary(condition: ModelPlazaV2PriceCondition): string {
  const parts: string[] = []
  if (condition.input_per_million != null) parts.push(`in $${formatPrice(condition.input_per_million)}`)
  if (condition.output_per_million != null) parts.push(`out $${formatPrice(condition.output_per_million)}`)
  if (condition.per_request_price != null) parts.push(`req $${formatPrice(condition.per_request_price)}`)
  return parts.join(' · ') || (locale.value === 'zh' ? '条件价' : 'Conditional price')
}
</script>

<style scoped>
.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}
</style>
