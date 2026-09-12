<template>
  <section class="card space-y-4 p-5" aria-labelledby="personal-features-title">
    <div><h3 id="personal-features-title" class="text-lg font-semibold">新增功能与额度期限</h3><p class="mt-1 text-sm text-gray-500">设置仅影响新请求和新批次，不追改历史额度。迁移与经营规则验证后再启用。</p></div>
    <div v-if="loading" role="status">加载中…</div>
    <div v-else-if="error && !policy" role="alert" class="text-red-600">{{ error }} <button type="button" class="btn btn-secondary" @click="load">重试</button></div>
    <template v-else-if="policy">
      <div class="grid gap-4 md:grid-cols-2">
        <label class="flex items-center gap-2"><input v-model="policy.model_plaza_v2_enabled" type="checkbox" />独立模型广场 V2</label>
        <label class="flex items-center gap-2"><input v-model="policy.player_invitations_enabled" type="checkbox" />玩家一次性邀请</label>
        <label><span class="input-label">邀请码有效期（秒，启用前必须明确配置）</span><input v-model.number="policy.invitation_ttl_seconds" class="input" type="number" min="0" step="1" /></label>
        <div class="text-sm text-gray-500">初始机会为每用户一次；确认预留、成功注册才消耗，取消或过期释放。不会补发历史返利。</div>
        <label><span class="input-label">签到临时额度截止（北京时间）</span><input v-model="policy.source_expiry.checkin" class="input" type="time" /></label>
        <label><span class="input-label">管理员发放临时额度截止（北京时间）</span><input v-model="policy.source_expiry.admin_grant" class="input" type="time" /></label>
      </div>
      <p class="text-sm text-gray-500">到期时间与签到刷新时间分离。银行兑换期限在银行设置中配置；商城、订阅、预支债务不继承此设置。每笔新额度将固定下一个截止点。</p>
      <p v-if="error" class="text-sm text-red-600" role="alert">{{ error }}</p>
      <p v-if="auth.user?.permission_mode !== 'enforce'" class="text-sm text-amber-700">权限仍处于兼容／影子模式；先在专用测试库完成显式超管迁移，再开放设置写入。</p>
      <div class="flex flex-wrap items-center justify-between gap-3"><span class="text-xs text-gray-500 break-all">版本 {{ policy.version }}</span><button type="button" class="btn btn-primary" :disabled="saving || auth.user?.permission_mode !== 'enforce'" @click="save">{{ saving ? '保存中…' : '确认对新批次生效' }}</button></div>
    </template>
  </section>
</template>
<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { apiClient } from '@/api/client'
import { useAuthStore } from '@/stores/auth'
import { useAppStore } from '@/stores/app'
interface Policy { model_plaza_v2_enabled: boolean; player_invitations_enabled: boolean; invitation_ttl_seconds: number; source_expiry: Record<string,string>; version: string }
const auth=useAuthStore(), app=useAppStore()
const policy=ref<Policy|null>(null), loading=ref(false), saving=ref(false), error=ref('')
async function load(){ loading.value=true; error.value='';try{ const {data}=await apiClient.get<Policy>('/admin/settings/personal-features');policy.value=data }catch{error.value='读取功能设置失败，请检查权限与服务状态。'}finally{loading.value=false} }
async function save(){ if(!policy.value || saving.value)return;saving.value=true;error.value='';try{const {data}=await apiClient.put<Policy>('/admin/settings/personal-features',policy.value);policy.value=data;app.showSuccess('设置已保存；存量批次保持原合同。')}catch{error.value='保存失败或版本已变化。请刷新设置并重新确认，不会自动覆盖其他管理员修改。'}finally{saving.value=false} }
onMounted(load)
</script>
