<template>
  <div class="flex min-h-56 items-center justify-center">
    <div v-if="!chartGroups.length" class="text-sm text-gray-500 dark:text-gray-400">
      {{ t('finance.noData') }}
    </div>
    <div v-else class="w-full space-y-5">
      <section v-for="group in chartGroups" :key="group.key" class="flex w-full flex-col items-center gap-4 sm:flex-row sm:items-center">
        <div class="h-48 w-48 shrink-0">
          <Doughnut :data="group.chartData" :options="group.chartOptions" />
        </div>
        <div class="min-w-0 flex-1">
          <p class="mb-2 text-xs font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">
            {{ financialUnitLabel(group.currency, group.unit, translate) }}
          </p>
          <ul class="w-full space-y-2 text-sm">
            <li v-for="segment in group.segments" :key="`${segment.category}:${segment.label}`" class="flex items-center justify-between gap-3">
              <span class="flex min-w-0 items-center gap-2 text-gray-700 dark:text-gray-300">
                <span class="h-2.5 w-2.5 shrink-0 rounded-full" :style="{ backgroundColor: segment.color }" />
                <span class="truncate">{{ segment.label }}</span>
              </span>
              <span class="shrink-0 font-mono text-gray-900 dark:text-white">
                {{ formatFinancialAmount(segment.amount, group.currency, group.unit, translate) }}
              </span>
            </li>
          </ul>
        </div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { Chart as ChartJS, ArcElement, Tooltip, Legend } from 'chart.js'
import { Doughnut } from 'vue-chartjs'
import type { LedgerCategorySummary } from '@/types/finance'
import { financialAmountKey, financialUnitLabel, formatFinancialAmount, type FinanceTranslate } from './financialDisplay'

ChartJS.register(ArcElement, Tooltip, Legend)

const { t } = useI18n()
const translate = t as unknown as FinanceTranslate
const props = defineProps<{ summary: LedgerCategorySummary[] }>()

const colors = ['#22c55e', '#06b6d4', '#6366f1', '#f59e0b', '#ef4444', '#ec4899', '#14b8a6']

const chartGroups = computed(() => {
  const grouped = new Map<string, { currency: string; unit: string; segments: Array<LedgerCategorySummary & { amount: number; color: string }> }>()
  for (const item of props.summary ?? []) {
    const amount = Number(item.amount) || 0
    if (amount <= 0) continue
    const key = financialAmountKey(item)
    const group = grouped.get(key) ?? { currency: item.currency, unit: item.unit, segments: [] }
    group.segments.push({ ...item, amount, color: colors[group.segments.length % colors.length] })
    grouped.set(key, group)
  }
  return [...grouped.entries()].map(([key, group]) => {
    const chartData = {
      labels: group.segments.map((item) => item.label),
      datasets: [{
        data: group.segments.map((item) => item.amount),
        backgroundColor: group.segments.map((item) => item.color),
        borderWidth: 0,
      }],
    }
    const chartOptions = {
      responsive: true,
      maintainAspectRatio: false,
      plugins: {
        legend: { display: false },
        tooltip: {
          callbacks: {
            label: (context: { label?: string; raw?: unknown }) => {
              const value = Number(context.raw) || 0
              return `${context.label ?? ''}: ${formatFinancialAmount(value, group.currency, group.unit, translate)}`
            },
          },
        },
      },
    }
    return { key, ...group, chartData, chartOptions }
  })
})
</script>
