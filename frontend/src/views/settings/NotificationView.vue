<script setup>
import { ref } from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import FormField from '../../components/FormField.vue';
import { useSettings, jsonOptions } from '../../composables/useSettings.js';
import { request, toast } from '../../runtime.js';

const types=[{label:'Webhook · 通用 JSON',value:'webhook'},{label:'电子邮件',value:'email'},{label:'Telegram',value:'telegram'},{label:'Server酱',value:'serverchan'}];
const blank = type => ({
  id:`target_${[...crypto.getRandomValues(new Uint8Array(12))].map(value=>value.toString(16).padStart(2,'0')).join('')}`,
  name:types.find(item=>item.value===type)?.label||'通知目标',type,enabled:true,
  webhook:{url:'',protocol:'general',method:'POST',contentType:'application/json',headers:{},bodyTemplate:''},
  telegram:{token:'',chatId:''},email:{service:'qq',user:'',pass:'',to:''},serverchan:{sendKey:''},
});
function normalize(value){
  return {...value,types:{login:true,upload:true,nsfw:true,...value.types},channels:(value.channels||[]).map(item=>({
    ...blank(item.type),...item,
    webhook:{...blank('webhook').webhook,...item.webhook,headersText:JSON.stringify(item.webhook?.headers||{},null,2)},
    telegram:{...blank('telegram').telegram,...item.telegram},
    email:{...blank('email').email,...item.email},
    serverchan:{...blank('serverchan').serverchan,...item.serverchan},
  }))};
}
const {model,busy,saving,error,load,save}=useSettings('/api/notification',normalize);
const newType=ref('webhook'),testingID=ref('');
function add(){model.value.channels.push({...blank(newType.value),webhook:{...blank('webhook').webhook,headersText:'{}'}});}
function remove(id){model.value.channels=model.value.channels.filter(item=>item.id!==id);}
function body(){
  const channels=model.value.channels.map(item=>{
    const channel={id:item.id,name:item.name.trim(),type:item.type,enabled:!!item.enabled};
    if(item.type==='webhook'){
      const headers=JSON.parse(item.webhook.headersText||'{}');
      if(!headers || Array.isArray(headers) || typeof headers!=='object')throw new Error(`${item.name}：请求头必须是 JSON 对象`);
      channel.webhook={url:item.webhook.url.trim(),protocol:item.webhook.protocol||'general'};
      if(channel.webhook.protocol==='legacy')Object.assign(channel.webhook,{method:item.webhook.method,contentType:item.webhook.contentType,headers,bodyTemplate:item.webhook.bodyTemplate});
    }else channel[item.type]=item[item.type];
    return channel;
  });
  return {enabled:!!model.value.enabled,types:model.value.types,channels};
}
async function submit(){try{await save(body());}catch(reason){toast(reason.message);}}
async function test(item){testingID.value=item.id;try{await request(`/api/notification/test?channel=${encodeURIComponent(item.id)}`,jsonOptions(body()));toast(`${item.name} 测试通知已发送`);}catch(reason){toast(reason.message);}finally{testingID.value='';}}
</script>

<template>
  <SettingsPage title="通知推送" description="登录、上传和审核事件可同时发送到多个目标；每个目标独立重试。" :busy="busy" :error="error" @retry="load">
    <form class="notification-settings" novalidate @submit.prevent="submit">
      <section class="settings-panel form-stack"><FormField v-model="model.enabled" type="checkbox" label="启用通知" /><div class="format-options"><UiCheckbox v-model="model.types.login" label="登录通知" /><UiCheckbox v-model="model.types.upload" label="上传通知" /><UiCheckbox v-model="model.types.nsfw" label="审核通知" /></div><p class="field-help">事件类型对全部启用的目标生效。单个目标失败只会重试该目标。</p></section>
      <section class="settings-panel form-stack"><div class="notification-heading"><div><h2>通知目标</h2><p class="field-help">可同时添加多个邮件、Webhook、Telegram 或 Server酱目标。</p></div><span>{{ model.channels.length }} 个目标</span></div><p v-if="!model.channels.length" class="empty">尚未添加通知目标。</p>
        <article v-for="(item,index) in model.channels" :key="item.id" class="notification-target"><div class="notification-target-head"><strong>{{ index+1 }} · {{ item.name || '未命名目标' }}</strong><div class="inline-actions"><UiButton :loading="testingID===item.id" :disabled="!item.enabled || saving" @click="test(item)">测试此目标</UiButton><UiButton variant="danger" :disabled="saving" @click="remove(item.id)">移除</UiButton></div></div><div class="settings-form-grid"><FormField v-model="item.name" label="目标名称" required /><FormField v-model="item.enabled" type="checkbox" label="启用此目标" /></div><p class="field-help">{{ types.find(option=>option.value===item.type)?.label }}</p>
          <template v-if="item.type==='webhook'"><FormField v-model="item.webhook.url" label="Webhook URL" placeholder="https://example.com/webhook/general" required /><p v-if="item.webhook.protocol!=='legacy'" class="field-help">通用 JSON 协议：POST，Content-Type: application/json；发送 message、title、source、level、timestamp、fields、url。最终 QQ 消息不得超过 1900 字符。</p><template v-if="item.webhook.protocol==='legacy'"><p class="field-help">此目标由旧自定义 Webhook 迁移，可继续使用原请求模板。</p><div class="settings-form-grid"><FormField v-model="item.webhook.method" label="请求方法" type="select" :options="[{label:'POST',value:'POST'},{label:'PUT',value:'PUT'}]" /><FormField v-model="item.webhook.contentType" label="Content-Type" /></div><FormField v-model="item.webhook.headersText" label="请求头 JSON" type="textarea" /><FormField v-model="item.webhook.bodyTemplate" label="请求体模板" type="textarea" /></template></template>
          <div v-else-if="item.type==='email'" class="settings-form-grid"><FormField v-model="item.email.service" label="邮件服务" type="select" :options="['gmail','qq','163','outlook'].map(value=>({label:value,value}))" /><FormField v-model="item.email.user" label="发件邮箱" /><FormField v-model="item.email.pass" label="授权码" type="password" /><FormField v-model="item.email.to" label="收件邮箱" help="留空发送给自己" /></div>
          <div v-else-if="item.type==='telegram'" class="settings-form-grid"><FormField v-model="item.telegram.token" label="Bot Token" type="password" /><FormField v-model="item.telegram.chatId" label="Chat ID" /></div>
          <FormField v-else v-model="item.serverchan.sendKey" label="SendKey" type="password" />
        </article>
        <div class="notification-add"><UiSelect v-model="newType" aria-label="新增通知目标类型" :options="types" /><UiButton icon="plus" @click="add">添加目标</UiButton></div>
      </section>
      <div class="inline-actions"><UiButton type="submit" variant="primary" :loading="saving">保存通知设置</UiButton></div>
    </form>
  </SettingsPage>
</template>

<style scoped>
.notification-settings{display:grid;gap:20px}.notification-heading,.notification-target-head,.notification-add{display:flex;align-items:center;justify-content:space-between;gap:12px}.notification-heading h2{margin:0}.notification-heading>span{color:var(--secondary);font-size:13px}.notification-target{display:grid;gap:14px;padding:20px;border:1px solid var(--border);border-radius:12px}.notification-target-head strong{font-size:15px}.notification-target-head .inline-actions{flex-wrap:wrap}.notification-add{justify-content:flex-start}.notification-add .ui-select{width:min(300px,100%)}@media(max-width:680px){.notification-heading,.notification-target-head,.notification-add{align-items:stretch;flex-direction:column}.notification-target{padding:16px}}
</style>
