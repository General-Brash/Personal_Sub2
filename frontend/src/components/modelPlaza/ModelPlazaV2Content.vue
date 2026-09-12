<template>
  <div class="space-y-5">
    <div v-if="loading" class="card p-6 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ locale === 'zh' ? '正在加载模型目录…' : 'Loading model catalog…' }}
    </div>
    <div v-else-if="error" class="card p-6 text-center text-sm text-red-600 dark:text-red-400">
      {{ locale === 'zh' ? '模型目录加载失败。' : 'Failed to load the model catalog.' }}
    </div>
    <div v-else-if="!models.length" class="card p-6 text-center text-sm text-gray-500 dark:text-gray-400">
      {{ locale === 'zh' ? '暂无可用模型。' : 'No models are available.' }}
    </div>
    <div v-else class="grid gap-4 lg:grid-cols-2">
      <article v-for="model in models" :key="`${model.platform}:${model.model_id}`" class="card min-w-0 p-5">
        <div class="flex min-w-0 items-start justify-between gap-3">
          <div class="min-w-0">
            <h2 class="break-words text-base font-semibold text-gray-900 dark:text-white">{{ model.display_name || model.model_id }}</h2>
            <p class="mt-1 break-all font-mono text-xs text-gray-500 dark:text-gray-400">{{ model.model_id }} · {{ model.platform }}</p>
          </div>
          <span :class="stateClass(model.availability_state)" class="shrink-0 rounded-full px-2.5 py-1 text-xs font-medium">
            {{ stateLabel(model.availability_state) }}
          </span>
        </div>

        <div v-if="model.capabilities?.length" class="mt-3 flex flex-wrap gap-1.5">
          <span v-for="capability in model.capabilities" :key="capability" class="rounded bg-gray-100 px-2 py-1 text-[11px] text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ capability }}</span>
        </div>

        <div class="mt-4 space-y-3">
          <div v-for="choice in model.user_group_choices" :key="`${choice.group_id}:${choice.route_kind}`" class="rounded-lg border border-gray-200 p-3 dark:border-dark-700">
            <div class="flex min-w-0 flex-wrap items-center justify-between gap-2">
              <div class="min-w-0">
                <p class="break-words text-sm font-medium text-gray-900 dark:text-white">{{ choice.group_name || `#${choice.group_id}` }}</p>
                <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">{{ choice.platform }} · {{ choice.route_kind }}</p>
              </div>
              <span :class="stateClass(choice.availability_state)" class="rounded-full px-2 py-0.5 text-[11px] font-medium">{{ stateLabel(choice.availability_state) }}</span>
            </div>

            <p v-if="choice.price_quote" class="mt-3 text-xs text-gray-500">{{ locale === 'zh' ? '基准单价，需叠加下方倍率；最终账单以请求快照为准。' : 'Base unit prices; factors below apply. The request snapshot determines billing.' }}</p>
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
            <div v-if="choice.price_quote?.dynamic_factor?.details?.tier_id" class="mt-3 rounded bg-gray-50 p-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">
              当前档 {{ choice.price_quote.dynamic_factor.details.tier_id }} · 累计 {{ choice.price_quote.dynamic_factor.details.current }} {{ choice.price_quote.dynamic_factor.details.metric === 'tokens_m' ? 'tokens' : 'USD' }} · 动态 ×{{ formatMultiplier(choice.price_quote.dynamic_factor.factor) }}
              <span v-if="choice.price_quote.dynamic_factor.details.next_threshold"> · 下一档 {{ choice.price_quote.dynamic_factor.details.next_threshold }}</span>
              <span v-if="choice.price_quote.dynamic_factor.details.reset_at" class="block">重置 {{ formatDate(choice.price_quote.dynamic_factor.details.reset_at) }}；已接纳请求不回算。</span>
            </div>
            <div v-if="choice.price_quote?.effective_rate_multiplier != null || choice.price_quote?.channel_time_multiplier != null || choice.price_quote?.image_rate_independent" class="mt-3 flex flex-wrap gap-2 text-[11px]">
              <span v-if="choice.price_quote?.effective_rate_multiplier != null" class="rounded bg-primary-50 px-2 py-1 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">
                {{ locale === 'zh' ? '生效倍率' : 'Effective rate' }} ×{{ formatMultiplier(choice.price_quote.effective_rate_multiplier) }}
              </span>
              <span v-if="choice.price_quote?.peak_rate_multiplier != null" class="rounded bg-primary-50 px-2 py-1 text-primary-700 dark:bg-primary-900/30 dark:text-primary-300">{{locale==='zh'?'高峰因子':'Peak factor'}} ×{{formatMultiplier(choice.price_quote.peak_rate_multiplier)}}</span>
              <span v-if="choice.price_quote?.channel_time_multiplier != null" class="rounded bg-cyan-50 px-2 py-1 text-cyan-700 dark:bg-cyan-900/30 dark:text-cyan-300">
                {{ locale === 'zh' ? '渠道分时' : 'Channel time' }} ×{{ formatMultiplier(choice.price_quote.channel_time_multiplier) }}
              </span>
              <span v-if="choice.price_quote?.image_rate_independent" class="rounded bg-violet-50 px-2 py-1 text-violet-700 dark:bg-violet-900/30 dark:text-violet-300">
                {{ locale === 'zh' ? '图片独立倍率' : 'Independent image rate' }}
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
          </div>
        </div>
      </article>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ModelPlazaAvailabilityState, ModelPlazaV2PriceCondition, ModelPlazaV2Response } from '@/api/modelPlaza'

const props = defineProps<{
  response: ModelPlazaV2Response | null
  loading: boolean
  error: boolean
}>()

const { locale } = useI18n()
const models = computed(() => props.response?.models ?? [])

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

function conditionSummary(condition: ModelPlazaV2PriceCondition): string {
  const parts: string[] = []
  if (condition.input_per_million != null) parts.push(`in $${formatPrice(condition.input_per_million)}`)
  if (condition.output_per_million != null) parts.push(`out $${formatPrice(condition.output_per_million)}`)
  if (condition.per_request_price != null) parts.push(`req $${formatPrice(condition.per_request_price)}`)
  return parts.join(' · ') || (locale.value === 'zh' ? '条件价' : 'Conditional price')
}
</script>
