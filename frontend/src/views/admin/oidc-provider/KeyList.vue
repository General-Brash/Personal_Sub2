<template>
  <section class="card overflow-hidden" aria-labelledby="oidc-keys-title">
    <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
      <div>
        <h2 id="oidc-keys-title" class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.oidcProvider.keys.title') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.keys.description') }}</p>
      </div>
      <button type="button" class="btn btn-secondary btn-sm" :disabled="busy || !canRotate" @click="rotate">
        {{ t('admin.oidcProvider.keys.rotate') }}
      </button>
    </div>

    <div v-if="loading" class="flex min-h-32 items-center justify-center p-6">
      <LoadingSpinner size="md" />
    </div>
    <div v-else-if="!canRead" class="p-6 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.noPermission') }}</div>
    <div v-else-if="keys.length === 0" class="p-6 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.oidcProvider.keys.empty') }}</div>
    <div v-else class="overflow-x-auto">
      <table class="min-w-full text-left text-sm">
        <thead class="border-b border-gray-100 text-xs uppercase tracking-wide text-gray-500 dark:border-dark-700 dark:text-gray-400">
          <tr>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.keys.kid') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.keys.alg') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.keys.fingerprint') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.keys.status') }}</th>
            <th class="px-5 py-3">{{ t('admin.oidcProvider.keys.notAfter') }}</th>
            <th class="px-5 py-3"><span class="sr-only">{{ t('admin.oidcProvider.keys.revoke') }}</span></th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
          <tr v-for="key in keys" :key="key.kid">
            <td class="px-5 py-3 font-mono text-xs text-gray-800 dark:text-gray-200">{{ key.kid }}</td>
            <td class="px-5 py-3 font-mono text-xs text-gray-600 dark:text-gray-400">{{ key.alg }}</td>
            <td class="max-w-48 break-all px-5 py-3 font-mono text-xs text-gray-600 dark:text-gray-400">{{ key.fingerprint }}</td>
            <td class="px-5 py-3"><span class="rounded-full px-2 py-0.5 text-xs font-medium" :class="statusClass(key.status)">{{ key.status }}</span></td>
            <td class="px-5 py-3 text-xs text-gray-600 dark:text-gray-400">{{ formatDate(key.not_after) }}</td>
            <td class="px-5 py-3 text-right">
              <div class="flex justify-end gap-3">
                <button v-if="key.status === 'active'" type="button" class="text-xs font-medium text-amber-600 hover:text-amber-700 dark:text-amber-400" :disabled="busy || !canRevoke" @click="retire(key.kid)">
                  {{ t('admin.oidcProvider.keys.retire') }}
                </button>
                <button v-if="key.status !== 'revoked' && key.status !== 'retired'" type="button" class="text-xs font-medium text-red-600 hover:text-red-700 dark:text-red-400" :disabled="busy || !canRevoke" @click="revoke(key.kid)">
                  {{ t('admin.oidcProvider.keys.revoke') }}
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-if="canRead" class="border-t border-gray-100 px-5 py-3 text-xs text-gray-500 dark:border-dark-700 dark:text-gray-400">
      {{ t('admin.oidcProvider.security.privateKeyHidden') }}
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { OidcSigningKeySummary } from '@/api/admin'

defineProps<{
  keys: OidcSigningKeySummary[]
  loading: boolean
  busy: boolean
  canRead: boolean
  canRotate: boolean
  canRevoke: boolean
}>()

const emit = defineEmits<{
  rotate: []
  retire: [kid: string]
  revoke: [kid: string]
}>()

const { t } = useI18n()

function rotate(): void {
  if (window.confirm(t('admin.oidcProvider.keys.confirmRotate'))) emit('rotate')
}

function retire(kid: string): void {
  if (window.confirm(t('admin.oidcProvider.keys.confirmRetire'))) emit('retire', kid)
}

function revoke(kid: string): void {
  if (window.confirm(t('admin.oidcProvider.keys.confirmRevoke'))) emit('revoke', kid)
}

function formatDate(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
}

function statusClass(status: string): string {
  if (status === 'active') return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300'
  if (status === 'revoked' || status === 'retired') return 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'
  return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
}
</script>
