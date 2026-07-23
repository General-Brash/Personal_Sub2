<template>
  <AppLayout>
    <div class="mx-auto max-w-6xl space-y-6">
      <header class="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900 dark:text-white">{{ t('finance.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('finance.description') }}</p>
        </div>
        <RouterLink
          v-if="authStore.isAdmin"
          to="/admin/finance"
          class="btn btn-secondary inline-flex min-h-10 items-center justify-center gap-2 self-start"
        >
          <Icon name="users" size="sm" />
          {{ t('finance.allSite') }}
        </RouterLink>
      </header>
      <LedgerWorkspace />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import AppLayout from '@/components/layout/AppLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import LedgerWorkspace from '@/components/finance/LedgerWorkspace.vue'

const { t } = useI18n()
const authStore = useAuthStore()
</script>
