<template>
  <section class="card overflow-hidden" aria-labelledby="oidc-consents-title">
    <div class="border-b border-gray-100 px-5 py-4 dark:border-dark-700">
      <h2 id="oidc-consents-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.consents.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.consents.description') }}</p>
    </div>
    <div v-if="loading" class="flex min-h-32 items-center justify-center p-6"><LoadingSpinner size="md" /></div>
    <div v-else-if="!canRead" class="p-6 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.noPermission') }}</div>
    <div v-else-if="consents.length === 0" class="p-6 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.consents.empty') }}</div>
    <div v-else class="overflow-x-auto">
      <table class="min-w-full text-left text-sm">
        <thead class="border-b border-gray-100 text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
          <tr>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.consents.user') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.consents.client') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.consents.scopes') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.consents.source') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.consents.status') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.consents.approvedAt') }}</th>
            <th class="px-5 py-3"><span class="sr-only">{{ t('admin.oidcProvider.consents.revoke') }}</span></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="consent in consents" :key="consent.id">
            <td class="px-5 py-3 text-gray-800 dark:text-gray-200">#{{ consent.user_id }}</td>
            <td class="px-5 py-3"><span class="font-medium text-gray-800 dark:text-gray-200">{{ consent.client_name || consent.client_id }}</span><span v-if="consent.client_name" class="mt-0.5 block font-mono text-xs text-gray-500">{{ consent.client_id }}</span></td>
            <td class="max-w-64 px-5 py-3"><div class="flex flex-wrap gap-1"><span v-for="scope in consent.scopes" :key="scope" class="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[11px] dark:bg-dark-700">{{ scope }}</span></div></td>
            <td class="px-5 py-3 text-xs text-gray-600 dark:text-gray-400">{{ consent.source === 'admin_pre_authorized' ? t('admin.oidcProvider.consents.preAuthorized') : t('admin.oidcProvider.consents.interactive') }}</td>
            <td class="px-5 py-3"><span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="consent.status === 'active' ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'">{{ consent.status }}</span></td>
            <td class="px-5 py-3 text-xs text-gray-600 dark:text-gray-400">{{ formatDate(consent.approved_at) }}</td>
            <td class="px-5 py-3 text-right"><button v-if="consent.status === 'active'" type="button" class="text-xs font-medium text-red-600 hover:text-red-700 dark:text-red-400" :disabled="busyId === consent.id || !canRevoke" @click="revoke(consent.id)">{{ t('admin.oidcProvider.consents.revoke') }}</button></td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { OidcConsentSummary, OidcResourceId } from '@/api/admin'

defineProps<{
  consents: OidcConsentSummary[]
  loading: boolean
  busyId: OidcResourceId | null
  canRead: boolean
  canRevoke: boolean
}>()

const emit = defineEmits<{ revoke: [id: OidcResourceId] }>()
const { t } = useI18n()

function revoke(id: OidcResourceId): void {
  if (window.confirm(t('admin.oidcProvider.consents.confirmRevoke'))) emit('revoke', id)
}

function formatDate(value?: string | null): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}
</script>