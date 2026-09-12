<template>
  <BaseDialog :show="show" title="开启自动签到" width="normal" close-on-click-outside @close="$emit('close')">
    <div class="space-y-4">
      <div class="rounded-lg border border-primary-200 bg-primary-50 p-4 text-sm leading-6 text-primary-900 dark:border-primary-900/60 dark:bg-primary-950/30 dark:text-primary-100">
        <p>开启后，进入站点时自动按<b>直接领取</b>完成本周期签到。</p>
        <p class="mt-2">自动模式不参与普通/超级博弈；本次临时奖励手续费为 <b>{{ feeBps / 100 }}%</b>，从临时基础奖内扣除，不额外扣永久余额。永久基础奖不变。</p>
        <p class="mt-2">可随时关闭。费率或收费对象变化时，本同意失效并要求重新确认。</p>
      </div>
      <label class="flex items-start gap-3 rounded-lg border border-gray-200 p-3 text-sm text-gray-700 dark:border-dark-600 dark:text-gray-200">
        <input v-model="accepted" type="checkbox" class="mt-0.5 h-4 w-4 rounded border-gray-300 text-primary-600" data-test="checkin-consent-checkbox" />
        <span>我已阅读并同意以上自动签到与手续费条款。</span>
      </label>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn-secondary" @click="$emit('close')">取消</button>
        <button type="button" class="btn-primary" :disabled="!accepted" data-test="checkin-consent-confirm" @click="$emit('confirm')">
          确认开启
        </button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'

const props = defineProps<{ show: boolean; feeBps: number }>()
defineEmits<{ (event: 'confirm'): void; (event: 'close'): void }>()
const accepted = ref(false)
watch(() => props.show, (shown) => { if (shown) accepted.value = false })
</script>
