<template>
  <section class="card overflow-hidden" aria-labelledby="oidc-audit-title">
    <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
      <h2 id="oidc-audit-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.audit.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.audit.description') }}</p>
    </div>
    <div v-if="loading" class="flex min-h-32 items-center justify-center p-6"><LoadingSpinner size="md" /></div>
    <div v-else-if="!canRead" class="p-6 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.noPermission') }}</div>
    <div v-else-if="events.length === 0" class="p-6 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.audit.empty') }}</div>
    <div v-else class="overflow-x-auto">
      <table class="min-w-full text-left text-sm">
        <thead class="border-b border-gray-100 text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
          <tr>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.audit.time') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.audit.action') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.audit.result') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.audit.actor') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.audit.client') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.audit.reason') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="event in events" :key="event.id">
            <td class="whitespace-nowrap px-5 py-3 text-xs text-gray-600 dark:text-gray-400">{{ formatDate(event.created_at) }}</td>
            <td class="px-5 py-3 font-mono text-xs text-gray-800 dark:text-gray-200">{{ event.action }}</td>
            <td class="px-5 py-3"><span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="event.result === 'success' ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'">{{ event.result === 'success' ? t('admin.oidcProvider.audit.success') : t('admin.oidcProvider.audit.failure') }}</span></td>
            <td class="px-5 py-3 text-xs text-gray-600 dark:text-gray-400">{{ event.actor_user_id ?? '-' }}</td>
            <td class="max-w-48 break-all px-5 py-3 font-mono text-xs text-gray-600 dark:text-gray-400">{{ event.client_id || '-' }}</td>
            <td class="max-w-64 px-5 py-3 text-xs text-gray-600 dark:text-gray-400">{{ event.reason || t('admin.oidcProvider.audit.redacted') }}</td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { OidcAuditEvent } from '@/api/admin'

defineProps<{
  events: OidcAuditEvent[]
  loading: boolean
  canRead: boolean
}>()

const { t } = useI18n()

function formatDate(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>