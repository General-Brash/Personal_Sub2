<template>
  <AuthLayout>
    <div class="space-y-6">
      <div class="text-center">
        <h2 class="text-2xl font-bold text-gray-900 dark:text-white">
          {{ t('auth.ssoBridge.title') }}
        </h2>
        <p class="mt-2 text-sm text-gray-500 dark:text-dark-400">
          {{ isProcessing ? t('auth.ssoBridge.processing') : errorMessage }}
        </p>
      </div>

      <div v-if="!isProcessing && errorMessage" class="text-center">
        <button class="btn btn-primary w-full" @click="goToLogin">
          {{ t('auth.ssoBridge.backToLogin') }}
        </button>
      </div>
    </div>
  </AuthLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AuthLayout } from '@/components/layout'
import { useAuthStore, useAppStore } from '@/stores'
import { oidcSsoAuthorize } from '@/api/auth'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()

const authStore = useAuthStore()
const appStore = useAppStore()

const isProcessing = ref(true)
const errorMessage = ref('')

function resolveTx(): string {
  const raw = route.query.tx
  const value = Array.isArray(raw) ? raw[0] : raw
  return typeof value === 'string' ? value.trim() : ''
}

function goToLogin() {
  // 带上登录后回跳目标（当前 /oauth/sso-bridge?tx=...），使登录成功后能回到本页继续。
  router.replace({ path: '/login', query: { redirect: route.fullPath } })
}

function getRequestErrorMessage(error: unknown, fallback: string): string {
  const err = error as {
    message?: string
    response?: { data?: { error?: string; message?: string; detail?: string } }
  }
  return (
    err.response?.data?.error ||
    err.response?.data?.message ||
    err.response?.data?.detail ||
    err.message ||
    fallback
  )
}

onMounted(async () => {
  const tx = resolveTx()
  if (!tx) {
    errorMessage.value = t('auth.ssoBridge.invalidRequest')
    isProcessing.value = false
    appStore.showError(errorMessage.value)
    return
  }

  // 未登录：跳到登录页并保留 tx 回跳目标，交给现有登录流程处理。
  if (!authStore.isAuthenticated) {
    goToLogin()
    return
  }

  try {
    const { redirect_url } = await oidcSsoAuthorize(tx)
    // 跨域绝对 URL，必须整页跳转，不能走前端路由。
    window.location.href = redirect_url
  } catch (error) {
    errorMessage.value = getRequestErrorMessage(error, t('auth.ssoBridge.failed'))
    isProcessing.value = false
    appStore.showError(errorMessage.value)
  }
})
</script>
