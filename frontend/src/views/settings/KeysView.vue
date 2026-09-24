<script setup>
import {ref} from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import FormField from '../../components/FormField.vue';
import {useSettings,jsonOptions} from '../../composables/useSettings.js';
import {request,toast} from '../../runtime.js';
import {ask,askText} from '../../dialogs.js';
const {model,busy,error,load}=useSettings('/api/apikeys');
const name=ref(''),working=ref(false);
async function create(){if(!name.value.trim()){toast('请输入密钥名称');return;}working.value=true;try{await request('/api/apikeys',jsonOptions({name:name.value.trim()}));name.value='';await load();toast('密钥已创建');}catch(reason){toast(reason.message);}finally{working.value=false;}}
async function edit(key,action){
 let body={},method='PUT';
 if(action==='rename'){const value=await askText('输入新的密钥名称',key.name);if(!value?.trim())return;body.name=value.trim();}
 if(action==='toggle')body.enabled=!key.enabled;
 if(action==='reset'){if(!await ask('重置后旧密钥立即失效。',{title:'重置 API 密钥',danger:true}))return;body.regenerate=true;}
 if(action==='delete'){if(!await ask('删除「'+key.name+'」？此操作不可撤销。',{danger:true}))return;method='DELETE';}
 try{await request('/api/apikeys/'+encodeURIComponent(key.id),{...jsonOptions(body),method});await load();toast('密钥已更新');}catch(reason){toast(reason.message);}
}
async function copy(value){try{await navigator.clipboard.writeText(value);toast('密钥已复制');}catch{toast('复制失败');}}
</script>
<template><SettingsPage title="API 密钥" description="为脚本和第三方客户端管理上传凭证。" :busy="busy" :error="error" @retry="load"><section class="settings-panel"><form class="key-create" novalidate @submit.prevent="create"><FormField v-model="name" label="密钥名称" placeholder="例如：桌面客户端" /><UiButton type="submit" variant="primary" :loading="working">创建密钥</UiButton></form><div class="key-list"><article v-for="key in model" :key="key.id" class="key-row"><div><strong>{{ key.name }}</strong><span class="status-chip" :class="key.enabled?'success':'muted'">{{ key.enabled?'已启用':'已停用' }}</span><code>{{ key.key }}</code></div><div class="row-actions"><UiButton @click="copy(key.key)">复制</UiButton><UiButton @click="edit(key,'rename')">重命名</UiButton><UiButton @click="edit(key,'toggle')">{{ key.enabled?'停用':'启用' }}</UiButton><UiButton @click="edit(key,'reset')">重置</UiButton><UiButton variant="danger" @click="edit(key,'delete')">删除</UiButton></div></article><p v-if="!model.length" class="empty">尚未创建 API 密钥。</p></div></section></SettingsPage></template>
