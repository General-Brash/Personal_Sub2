<template>
  <!-- 后台内嵌形态:?embedded=1 且已登录,套完整后台布局 -->
  <AppLayout v-if="isEmbedded">
    <ModelPlazaV2Content v-if="useV2" :response="v2Data" :loading="loading" :error="loadFailed" />
    <ModelPlazaContent v-else :response="data" :loading="loading" :error="loadFailed" embedded />
  </AppLayout>

  <!-- 独立形态:自带导航条(logo/站名 + 登录/回后台) -->
  <div v-else class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <PlazaNavBar />
    <main class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <ModelPlazaV2Content v-if="useV2" :response="v2Data" :loading="loading" :error="loadFailed" />
      <ModelPlazaContent v-else :response="data" :loading="loading" :error="loadFailed" />
    </main>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import PlazaNavBar from '@/components/modelPlaza/PlazaNavBar.vue'
import ModelPlazaContent from '@/components/modelPlaza/ModelPlazaContent.vue'
import ModelPlazaV2Content from '@/components/modelPlaza/ModelPlazaV2Content.vue'
import { getModelPlaza, getModelPlazaV2, type ModelPlazaResponse, type ModelPlazaV2Response } from '@/api/modelPlaza'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const appStore = useAppStore()
const authStore = useAuthStore()

// embedded=1 但未登录(如转发的链接)自动降级为独立形态。
const isEmbedded = computed(() => route.query.embedded === '1' && authStore.isAuthenticated)
// v2 默认关闭；后端开关和路由由集成方接线后可用 ?plaza_v2=1 灰度。
const useV2 = computed(() => route.query.plaza_v2 === '1')

const data = ref<ModelPlazaResponse | null>(null)
const v2Data = ref<ModelPlazaV2Response | null>(null)
const loading = ref(true)
const loadFailed = ref(false)

onMounted(async () => {
  // 独立形态导航条需要站点名/Logo;有 __APP_CONFIG__ 注入时同步命中缓存。
  void appStore.fetchPublicSettings()
  try {
    if (useV2.value) {
      v2Data.value = await getModelPlazaV2()
    } else {
      data.value = await getModelPlaza()
    }
  } catch {
    loadFailed.value = true
  } finally {
    loading.value = false
  }
})
</script>
