<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-6">
      <header>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('finance.allSiteTitle') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('finance.allSiteDescription') }}</p>
      </header>
      <LedgerWorkspace :admin="true" :initial-user-id="initialUserId" :initial-days="initialDays" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import LedgerWorkspace from '@/components/finance/LedgerWorkspace.vue'

const route = useRoute()
const { t } = useI18n()
const initialUserId = computed(() => {
  const value = Number(route.query.user_id)
  return Number.isInteger(value) && value > 0 ? value : undefined
})
const initialDays = computed<1 | 7 | 15>(() => {
  const value = Number(route.query.days)
  return value === 1 || value === 15 ? value : 7
})
</script>
