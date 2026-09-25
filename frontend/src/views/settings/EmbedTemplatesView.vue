<script setup>
import {onMounted,ref} from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import {request,toast} from '../../runtime.js';
import {renderEmbedTemplate} from '../../embed-templates.js';

const templates=ref([]),busy=ref(true),saving=ref(false),error=ref('');
const example={url:'/i/example.png',alt:'示例图片',width:1280,height:720,filename:'example.png'};
async function load(){busy.value=true;error.value='';try{templates.value=await request('/api/embed-templates');}catch(reason){error.value=reason.message;}finally{busy.value=false;}}
function add(){if(templates.value.length>=12){toast('最多添加 12 个模板');return;}let index=1;while(templates.value.some(item=>item.id===`custom-${index}`))index++;templates.value.push({id:`custom-${index}`,name:'新模板',body:'{url}'});}
function remove(index){if(templates.value.length<=1){toast('至少保留一个模板');return;}templates.value.splice(index,1);}
function move(index,delta){const next=index+delta;if(next<0||next>=templates.value.length)return;[templates.value[index],templates.value[next]]=[templates.value[next],templates.value[index]];}
function validate(){
  if(!templates.value.length||templates.value.length>12)return '模板数量须为 1–12 个';
  const ids=new Set();
  for(const item of templates.value){
    if(!/^[a-z][a-z0-9-]{0,31}$/.test(item.id)||ids.has(item.id))return '模板 ID 须唯一，以小写字母开头，且仅含小写字母、数字或短横线';
    ids.add(item.id);
    if(!item.name||item.name.trim()!==item.name||[...item.name].length>40)return '模板名称须为 1–40 字且不能有首尾空格';
    if(!item.body.includes('{url}')||new TextEncoder().encode(item.body).length>2048)return '模板内容必须包含 {url}，且不能超过 2048 字节';
    for(const match of item.body.matchAll(/\{([^{}]*)\}/g)){if(!['url','alt','width','height','filename'].includes(match[1]))return `不支持占位符 ${match[0]}`;}
  }
  return '';
}
async function save(){const problem=validate();if(problem){error.value=problem;return;}saving.value=true;error.value='';try{templates.value=await request('/api/admin/embed-templates',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(templates.value)});toast('嵌入代码模板已保存');}catch(reason){error.value=reason.message;}finally{saving.value=false;}}
const preview=item=>renderEmbedTemplate(item,example,location.origin);
onMounted(load);
</script>

<template><SettingsPage title="嵌入代码模板" description="配置图片菜单里的复制格式。使用占位符组合直链与图片信息。" :busy="busy" :error="!templates.length?error:''" @retry="load"><section class="settings-panel form-stack"><h2>复制格式</h2><p>可用占位符：<code>{url}</code>、<code>{alt}</code>、<code>{width}</code>、<code>{height}</code>、<code>{filename}</code>。每个模板必须包含 <code>{url}</code>。</p><p v-if="error" role="alert" class="form-error">{{ error }}</p><div class="embed-template-list"><article v-for="(item,index) in templates" :key="index" class="embed-template-row"><div class="embed-template-fields"><label class="form-field"><span>ID</span><UiInput v-model="item.id" :maxlength="32" aria-label="模板 ID" /></label><label class="form-field"><span>显示名称</span><UiInput v-model="item.name" :maxlength="40" aria-label="模板名称" /></label></div><label class="form-field"><span>模板内容</span><UiInput v-model="item.body" type="textarea" :rows="3" aria-label="嵌入代码模板内容" /></label><div class="embed-template-preview"><span>示例输出</span><code>{{ preview(item) }}</code></div><div class="inline-actions"><UiButton :disabled="index===0" @click="move(index,-1)">上移</UiButton><UiButton :disabled="index===templates.length-1" @click="move(index,1)">下移</UiButton><UiButton variant="danger" :disabled="templates.length<=1" @click="remove(index)">移除</UiButton></div></article></div><div class="inline-actions"><UiButton icon="plus" :disabled="templates.length>=12" @click="add">添加模板</UiButton><UiButton variant="primary" :loading="saving" @click="save">保存模板</UiButton></div></section></SettingsPage></template>

<style scoped>
.embed-template-list {display:grid;gap:14px}.embed-template-row {display:grid;gap:14px;padding:18px;border:1px solid var(--border);border-radius:10px}.embed-template-fields {display:grid;grid-template-columns:1fr 2fr;gap:14px}.embed-template-preview {display:grid;gap:7px;font-size:12px;color:var(--secondary)}.embed-template-preview code {display:block;white-space:pre-wrap;overflow-wrap:anywhere;padding:12px;background:var(--surface-raised);border-radius:7px;color:var(--text)}@media(max-width:640px){.embed-template-fields {grid-template-columns:1fr}}
</style>
