<template>
  <section class="card" data-testid="page-visibility-settings">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
        {{ t('admin.settings.features.pageVisibility.title') }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ t('admin.settings.features.pageVisibility.description') }}
      </p>
    </div>

    <div class="divide-y divide-gray-100 dark:divide-dark-700">
      <div class="p-6" data-test="user-pages-group">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.settings.features.pageVisibility.userPages') }}
        </h3>
        <div class="mt-4 space-y-5">
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.features.pageVisibility.userChannelStatus.label') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.pageVisibility.userChannelStatus.hint') }}
              </p>
            </div>
            <Toggle
              :model-value="userChannelStatusEnabled"
              :aria-label="t('admin.settings.features.pageVisibility.userChannelStatus.label')"
              @update:model-value="emit('update:userChannelStatusEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.features.pageVisibility.mall.label') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.pageVisibility.mall.hint') }}
              </p>
            </div>
            <Toggle
              :model-value="mallEnabled"
              :aria-label="t('admin.settings.features.pageVisibility.mall.label')"
              @update:model-value="emit('update:mallEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.features.pageVisibility.userSubscriptions.label') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.pageVisibility.userSubscriptions.hint') }}
              </p>
            </div>
            <Toggle
              :model-value="userSubscriptionsEnabled"
              :aria-label="t('admin.settings.features.pageVisibility.userSubscriptions.label')"
              @update:model-value="emit('update:userSubscriptionsEnabled', $event)"
            />
          </div>
        </div>
      </div>

      <div class="p-6" data-test="admin-pages-group">
        <h3 class="text-sm font-semibold text-gray-900 dark:text-white">
          {{ t('admin.settings.features.pageVisibility.adminPages') }}
        </h3>
        <div class="mt-4 space-y-5">
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.features.pageVisibility.adminSubscriptions.label') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.pageVisibility.adminSubscriptions.hint') }}
              </p>
            </div>
            <Toggle
              :model-value="adminSubscriptionsEnabled"
              :aria-label="t('admin.settings.features.pageVisibility.adminSubscriptions.label')"
              @update:model-value="emit('update:adminSubscriptionsEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.features.pageVisibility.adminPromoCodes.label') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.pageVisibility.adminPromoCodes.hint') }}
              </p>
            </div>
            <Toggle
              :model-value="adminPromoCodesEnabled"
              :aria-label="t('admin.settings.features.pageVisibility.adminPromoCodes.label')"
              @update:model-value="emit('update:adminPromoCodesEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('admin.settings.features.pageVisibility.adminChannelManagement.label') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.settings.features.pageVisibility.adminChannelManagement.hint') }}
              </p>
            </div>
            <Toggle
              :model-value="adminChannelManagementEnabled"
              :aria-label="t('admin.settings.features.pageVisibility.adminChannelManagement.label')"
              @update:model-value="emit('update:adminChannelManagementEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('finance.allSiteTitle') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('finance.allSiteDescription') }}
              </p>
            </div>
            <Toggle
              :model-value="adminFinanceEnabled"
              :aria-label="t('finance.allSiteTitle')"
              @update:model-value="emit('update:adminFinanceEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('finance.transactions.bankTitle') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('finance.transactions.description') }}
              </p>
            </div>
            <Toggle
              :model-value="adminBankTransactionsEnabled"
              :aria-label="t('finance.transactions.bankTitle')"
              @update:model-value="emit('update:adminBankTransactionsEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('nav.auditLogs') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.audit.description') }}
              </p>
            </div>
            <Toggle
              :model-value="adminAuditLogsEnabled"
              :aria-label="t('nav.auditLogs')"
              @update:model-value="emit('update:adminAuditLogsEnabled', $event)"
            />
          </div>

          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-gray-700 dark:text-gray-300">
                {{ t('nav.ops') }}
              </p>
              <p class="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.ops.description') }}
              </p>
            </div>
            <Toggle
              :model-value="adminOpsEnabled"
              :aria-label="t('nav.ops')"
              @update:model-value="emit('update:adminOpsEnabled', $event)"
            />
          </div>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import Toggle from '@/components/common/Toggle.vue'

defineProps<{
  userChannelStatusEnabled: boolean
  mallEnabled: boolean
  userSubscriptionsEnabled: boolean
  adminSubscriptionsEnabled: boolean
  adminPromoCodesEnabled: boolean
  adminChannelManagementEnabled: boolean
  adminFinanceEnabled: boolean
  adminBankTransactionsEnabled: boolean
  adminAuditLogsEnabled: boolean
  adminOpsEnabled: boolean
}>()

const emit = defineEmits<{
  (event: 'update:userChannelStatusEnabled', value: boolean): void
  (event: 'update:mallEnabled', value: boolean): void
  (event: 'update:userSubscriptionsEnabled', value: boolean): void
  (event: 'update:adminSubscriptionsEnabled', value: boolean): void
  (event: 'update:adminPromoCodesEnabled', value: boolean): void
  (event: 'update:adminChannelManagementEnabled', value: boolean): void
  (event: 'update:adminFinanceEnabled', value: boolean): void
  (event: 'update:adminBankTransactionsEnabled', value: boolean): void
  (event: 'update:adminAuditLogsEnabled', value: boolean): void
  (event: 'update:adminOpsEnabled', value: boolean): void
}>()

const { t } = useI18n()
</script>
