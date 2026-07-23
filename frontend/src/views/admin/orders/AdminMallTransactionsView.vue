<template>
  <AppLayout>
    <div class="mx-auto max-w-7xl space-y-5">
      <header>
        <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('finance.transactions.mallTitle') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('finance.transactions.description') }}</p>
      </header>
      <AdminTransactionLogTable kind="mall" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminTransactionLogTable from '@/components/finance/AdminTransactionLogTable.vue'
const { t } = useI18n()
</script>
