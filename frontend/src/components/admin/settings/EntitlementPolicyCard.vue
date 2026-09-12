<template>
 <section class="card space-y-4 p-5">
  <h3 class="text-lg font-semibold">普通／优质用户等级政策</h3>
  <p class="text-sm text-gray-500">等级只授予消费权益，不产生后台权限。个人显式倍率优先于等级默认；手工分组与已购订阅保留。</p>
  <p v-if="error" role="alert" class="text-sm text-red-600">{{ error }}</p>
  <button v-if="!catalog" class="btn btn-secondary" type="button" @click="load">加载等级政策</button>
  <template v-else>
   <div class="flex gap-2"><button v-for="item in catalog.tiers" :key="item.tier" type="button" class="btn btn-secondary" @click="choose(item)">{{item.display_name}}</button></div>
   <div v-if="form" class="space-y-3">
    <label class="flex gap-2 items-center"><input v-model="form.enabled" type="checkbox" :disabled="form.tier==='standard'" />启用 {{form.tier}}</label>
    <label class="block"><span class="input-label">等级名称</span><input v-model="form.display_name" class="input" /></label>
    <div v-for="(group,index) in form.groups" :key="index" class="grid grid-cols-[1fr_1fr_auto] gap-2">
     <label><span class="input-label">分组 ID</span><input v-model.number="group.group_id" type="number" min="1" class="input" /></label>
     <label><span class="input-label">默认倍率（留空继承）</span><input v-model.number="group.rate_multiplier" type="number" min="0" max="1000" step="0.01" class="input" /></label>
     <button type="button" class="btn btn-secondary self-end" @click="form.groups.splice(index,1)">移除</button>
    </div>
    <button type="button" class="btn btn-secondary" @click="form.groups.push({group_id:0,source:'tier'})">增加等级分组</button>
    <label class="block"><span class="input-label">变更原因</span><input v-model.trim="reason" class="input" /></label>
    <p class="text-xs text-gray-500">保存政策对下一次权益解析生效，不覆盖用户手工倍率。版本 {{form.version}}</p>
    <button type="button" class="btn btn-primary" :disabled="saving || !catalog.capabilities.can_write || !reason" @click="save">{{saving?'保存中…':'确认保存政策'}}</button>
   </div>
  </template>
 </section>
</template>
<script setup lang="ts">
import {ref} from 'vue'
import {getEntitlementCatalog,updateEntitlementPolicy,type EntitlementCatalog,type EntitlementTierPolicy} from '@/api/adminEntitlements'
const catalog=ref<EntitlementCatalog|null>(null),form=ref<EntitlementTierPolicy|null>(null),error=ref(''),reason=ref(''),saving=ref(false)
function choose(item:EntitlementTierPolicy){form.value=JSON.parse(JSON.stringify(item));reason.value=''}
async function load(){error.value='';try{catalog.value=await getEntitlementCatalog();const item=catalog.value.tiers.find(t=>t.tier==='premium');if(item)choose(item)}catch{error.value='读取等级政策失败，请核对权限。'}}
async function save(){if(!form.value||!catalog.value?.capabilities.can_write||!reason.value)return;saving.value=true;error.value='';try{await updateEntitlementPolicy({...form.value,expected_version:form.value.version,groups:form.value.groups.map(g=>({...g,rate_multiplier:typeof g.rate_multiplier==='number'?g.rate_multiplier:undefined})),reason:reason.value});await load()}catch{error.value='保存失败或政策版本已改变，请重新加载后确认。'}finally{saving.value=false}}
</script>
