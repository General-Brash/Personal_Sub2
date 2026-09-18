<template>
  <section class="card overflow-hidden" aria-labelledby="oidc-clients-title">
    <div class="flex flex-wrap items-start justify-between gap-3 border-b border-gray-100 px-5 py-4 dark:border-dark-700">
      <div>
        <h2 id="oidc-clients-title" class="text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('admin.oidcProvider.clients.title') }}
        </h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.oidcProvider.clients.description') }}
        </p>
      </div>
      <button
        v-if="canCreate"
        type="button"
        class="btn btn-primary btn-sm"
        @click="$emit('create')"
      >
        {{ t('admin.oidcProvider.clients.create') }}
      </button>
    </div>

    <div v-if="loading" class="flex min-h-40 items-center justify-center p-6">
      <LoadingSpinner size="md" />
    </div>
    <div v-else-if="!canRead" class="p-6 text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.oidcProvider.noPermission') }}
    </div>
    <div v-else-if="clients.length === 0" class="p-6 text-sm text-gray-500 dark:text-gray-400">
      {{ t('admin.oidcProvider.clients.empty') }}
    </div>
    <div v-else class="divide-y divide-gray-100 dark:divide-dark-700">
      <button
        v-for="client in clients"
        :key="client.id"
        type="button"
        class="block w-full px-5 py-4 text-left transition-colors hover:bg-gray-50 dark:hover:bg-dark-800"
        :class="selectedClientId === client.id ? 'bg-primary-50 dark:bg-primary-950/30' : ''"
        @click="$emit('select', client.id)"
      >
        <div class="flex flex-wrap items-start justify-between gap-2">
          <div class="min-w-0">
            <div class="flex flex-wrap items-center gap-2">
              <span class="truncate font-medium text-gray-900 dark:text-white">{{ client.name }}</span>
              <span
                class="rounded-full px-2 py-0.5 text-[11px] font-medium"
                :class="client.enabled ? 'bg-emerald-100 text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300' : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-gray-300'"
              >
                {{ client.enabled ? t('admin.oidcProvider.clients.enabled') : t('admin.oidcProvider.clients.disabled') }}
              </span>
            </div>
            <p class="mt-1 truncate font-mono text-xs text-gray-500 dark:text-gray-400">{{ client.client_id }}</p>
          </div>
          <span class="shrink-0 text-xs text-gray-500 dark:text-gray-400">{{ client.owner }}</span>
        </div>

        <div class="mt-3 grid gap-2 text-xs text-gray-600 dark:text-gray-300">
          <div>
            <span class="font-medium text-gray-700 dark:text-gray-200">{{ t('admin.oidcProvider.clients.redirects') }}:</span>
            <span v-if="client.redirect_uris.length" class="ml-1 break-all">{{ client.redirect_uris.slice(0, 2).join(' · ') }}</span>
            <span v-else class="ml-1 text-gray-400">-</span>
            <span v-if="client.redirect_uris.length > 2" class="ml-1 text-gray-400">+{{ client.redirect_uris.length - 2 }}</span>
          </div>
          <div class="flex flex-wrap items-center gap-1">
            <span class="font-medium text-gray-700 dark:text-gray-200">{{ t('admin.oidcProvider.clients.scopes') }}:</span>
            <span
              v-for="scope in client.allowed_scopes"
              :key="scope"
              class="rounded bg-gray-100 px-1.5 py-0.5 font-mono text-[11px] dark:bg-dark-700"
            >{{ scope }}</span>
          </div>
        </div>
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import type { OidcClientSummary, OidcResourceId } from '@/api/admin'

defineProps<{
  clients: OidcClientSummary[]
  selectedClientId: OidcResourceId | null
  loading: boolean
  canRead: boolean
  canCreate: boolean
}>()

defineEmits<{
  select: [id: OidcResourceId]
  create: []
}>()

const { t } = useI18n()
</script>