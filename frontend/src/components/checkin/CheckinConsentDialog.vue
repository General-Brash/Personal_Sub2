<template>
  <BaseDialog :show="show" :title="t('checkin.consent.title')" width="normal" close-on-click-outside @close="$emit('close')">
    <div class="space-y-4">
      <div class="rounded-lg border border-primary-200 bg-primary-50 p-4 text-sm leading-6 text-primary-900 dark:border-primary-900/60 dark:bg-primary-950/30 dark:text-primary-100">
        <p>{{ t('checkin.consent.directRule') }}</p>
        <p v-if="randomEnabled" class="mt-2">{{ t('checkin.consent.randomRule') }}</p>
        <p v-if="feeBps === 0" class="mt-2">{{ t('checkin.consent.feeFree') }}</p>
        <p v-else class="mt-2">{{ t('checkin.consent.feeRule', { fee: formatBpsAsPercent(feeBps) }) }}</p>
        <p class="mt-2">{{ t('checkin.consent.ruleChanges') }}</p>
      </div>
      <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 text-sm text-gray-700 dark:border-dark-600 dark:text-gray-200">
        <input v-model="accepted" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600" data-test="checkin-consent-checkbox" />
        <span>{{ t('checkin.consent.accept') }}</span>
      </label>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn-secondary" @click="$emit('close')">{{ t('checkin.consent.cancel') }}</button>
        <button type="button" class="btn-primary" :disabled="!accepted" data-test="checkin-consent-confirm" @click="handleConfirm">
          {{ t('checkin.consent.confirm') }}
        </button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = withDefaults(defineProps<{ show: boolean; feeBps: number; randomEnabled?: boolean; policyVersion?: string }>(), {
  randomEnabled: false,
  policyVersion: '',
})
const emit = defineEmits<{
  (event: 'confirm', payload: { policyVersion: string; feeBps: number }): void
  (event: 'close'): void
}>()
const { t } = useI18n()
const accepted = ref(false)
const confirmationSnapshot = ref<{ policyVersion: string; feeBps: number } | null>(null)

function handleConfirm() {
  const snapshot = confirmationSnapshot.value
  if (!accepted.value || !props.show || !props.policyVersion || !snapshot
    || snapshot.policyVersion !== props.policyVersion || snapshot.feeBps !== props.feeBps) return
  emit('confirm', { policyVersion: snapshot.policyVersion, feeBps: snapshot.feeBps })
}

function formatBpsAsPercent(bps: number): string {
  return `${(bps / 100).toFixed(2)}%`
}
watch(accepted, (value) => {
  confirmationSnapshot.value = value
    ? { policyVersion: props.policyVersion, feeBps: props.feeBps }
    : null
}, { flush: 'sync' })
watch(() => [props.show, props.feeBps, props.randomEnabled, props.policyVersion], () => {
  accepted.value = false
  confirmationSnapshot.value = null
}, { flush: 'sync' })
</script>
