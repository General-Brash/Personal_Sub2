<template>
  <p v-if="statistics?.includes_history" class="text-xs text-amber-700 dark:text-amber-400" role="status">
    {{ t('usageStatistics.historyNotice') }}
    <span v-if="statistics.history_timezone">{{ t('usageStatistics.bucketTimezone', { timezone: statistics.history_timezone }) }}</span>
    <span v-if="granularity">{{ t('usageStatistics.actualGranularity', { granularity: granularity === 'hour' ? t('admin.dashboard.hour') : t('admin.dashboard.day') }) }}</span>
    <span v-if="ops">{{ t('usageStatistics.opsNotice') }}</span>
  </p>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import type { UsageHistoryMetadata } from '@/types/usageStatistics'

defineProps<{ statistics?: UsageHistoryMetadata | null; granularity?: string; ops?: boolean }>()
const { t } = useI18n()
</script>
