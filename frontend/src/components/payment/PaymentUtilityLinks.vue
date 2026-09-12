<template>
  <section class="card p-4 sm:p-5" data-test="payment-utility-links">
    <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('commerce.sections.store') }}</h2>
    <div class="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-4">
      <RouterLink to="/purchase" class="btn btn-secondary min-h-10 justify-center">{{ t('purchase.title') }}</RouterLink>
      <RouterLink to="/redeem" class="btn btn-secondary min-h-10 justify-center">{{ t('common.nav.redeem') }}</RouterLink>
      <RouterLink to="/subscriptions" class="btn btn-secondary min-h-10 justify-center">{{ t('common.nav.subscriptions') }}</RouterLink>
      <RouterLink to="/orders" class="btn btn-secondary min-h-10 justify-center">{{ t('payment.orders.title') }}</RouterLink>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
</script>
