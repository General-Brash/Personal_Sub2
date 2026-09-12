<template>
 <section class="card p-5 space-y-4">
  <h3 class="text-lg font-semibold">邀请机会与关系补录</h3>
  <p class="text-sm text-gray-500">名额与返利金额独立。补录仅填空关系，服务端当前时刻生效，不补发旧兑换返利、不消耗玩家名额。</p>
  <div class="grid gap-4 lg:grid-cols-2">
   <div class="space-y-3 rounded-lg border border-gray-200 dark:border-dark-600 p-3">
    <h4 class="font-medium">增加邀请机会</h4>
    <label class="block"><span class="input-label">目标用户 ID</span><input v-model.number="target" type="number" min="1" class="input" /></label>
    <label class="block"><span class="input-label">增加次数</span><input v-model.number="delta" type="number" min="1" max="10000" class="input" /></label>
    <button type="button" class="btn btn-primary" :disabled="busy || !canQuota || !reason || target<=0 || delta<=0" @click="confirmAction('quota')">预览增额</button>
   </div>
   <div class="space-y-3 rounded-lg border border-gray-200 dark:border-dark-600 p-3">
    <h4 class="font-medium">补录未绑定的邀请关系</h4>
    <label class="block"><span class="input-label">邀请人 ID</span><input v-model.number="inviter" type="number" min="1" class="input" /></label>
    <label class="block"><span class="input-label">被邀请人 ID</span><input v-model.number="invitee" type="number" min="1" class="input" /></label>
    <button type="button" class="btn btn-primary" :disabled="busy || !canRelationship || !reason || inviter<=0 || invitee<=0" @click="confirmAction('relationship')">服务端预检</button>
   </div>
  </div>
  <label class="block"><span class="input-label">真实变更原因（必填）</span><input v-model.trim="reason" class="input" /></label>
  <p v-if="error" class="text-sm text-red-600" role="alert">{{error}}</p>
  <p v-if="!canQuota && !canRelationship" class="text-sm text-amber-700">当前尚未启用权限 enforce 或没有被授予邀请管理权限。</p>
  <BaseDialog :show="!!pending" title="确认邀请管理操作" @close="pending=null">
   <p class="text-sm leading-6">{{pending?.kind==='quota'?`将为用户 #${pending?.body.target_user_id} 增加 ${pending?.body.delta} 次邀请机会。`:`将用户 #${pending?.body.invitee_user_id} 的新邀请人补录为 #${pending?.body.inviter_user_id}。`}} 不回放历史返利。</p>
   <p class="mt-3 text-sm">原因：{{pending?.body.reason}}</p>
   <div class="mt-5 flex justify-end gap-2"><button type="button" class="btn btn-secondary" @click="pending=null">取消</button><button type="button" class="btn btn-primary" :disabled="busy" @click="apply">{{busy?'提交中…':'确认执行'}}</button></div>
  </BaseDialog>
 </section>
</template>
<script setup lang="ts">
import {computed,ref} from 'vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {apiClient} from '@/api/client'
import {useAuthStore} from '@/stores/auth'
import {useAppStore} from '@/stores/app'
const auth=useAuthStore(),app=useAppStore()
const target=ref(0),delta=ref(1),inviter=ref(0),invitee=ref(0),reason=ref(''),error=ref(''),busy=ref(false)
const canQuota=computed(()=>auth.user?.permission_mode==='enforce'&&auth.canAdmin('invites.quota.adjust'))
const canRelationship=computed(()=>auth.user?.permission_mode==='enforce'&&auth.canAdmin('affiliates.relationship.create'))
const pending=ref<{kind:'quota'|'relationship';body:Record<string,string|number>}|null>(null)
async function confirmAction(kind:'quota'|'relationship'){
 error.value='';busy.value=true
 try{
  if(kind==='relationship'){const {data}=await apiClient.post<{can_create:boolean}>('/admin/affiliates/relationships/preview',{inviter_user_id:inviter.value,invitee_user_id:invitee.value});if(!data.can_create){error.value='该关系已存在或会形成环路，不能补录。';return}}
  const id=crypto.randomUUID();pending.value={kind,body:kind==='quota'?{target_user_id:target.value,delta:delta.value,reason:reason.value,idempotency_key:id,request_id:id}:{inviter_user_id:inviter.value,invitee_user_id:invitee.value,reason:reason.value,request_id:id}}
 }catch{error.value='预检失败，请检查用户 ID、权限和关系状态。'}finally{busy.value=false}
}
async function apply(){if(!pending.value||busy.value)return;busy.value=true;error.value='';try{await apiClient.post(pending.value.kind==='quota'?'/admin/invitations/quota-adjust':'/admin/affiliates/relationships',pending.value.body);pending.value=null;app.showSuccess('邀请管理操作已完成并审计。')}catch{error.value='操作失败。网络错误时可重试同一确认操作，不会重复增额。'}finally{busy.value=false}}
</script>
