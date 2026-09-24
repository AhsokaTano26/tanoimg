<script setup>
import {ref,onMounted} from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import FormField from '../../components/FormField.vue';
import Pagination from '../../components/Pagination.vue';
import {request,toast} from '../../runtime.js';
import {jsonOptions} from '../../composables/useSettings.js';
import {ask} from '../../dialogs.js';
const data=ref({records:[],pagination:{total:0,totalPages:1}}),busy=ref(false),error=ref(''),page=ref(1),ip=ref(''),reason=ref('');
async function load(){busy.value=true;error.value='';try{data.value=await request('/api/blacklist?page='+page.value+'&limit=30');}catch(value){error.value=value.message;}finally{busy.value=false;}}
async function add(){try{await request('/api/blacklist',jsonOptions({ip:ip.value.trim(),reason:reason.value.trim()}));ip.value='';reason.value='';await load();toast('地址已加入黑名单');}catch(value){toast(value.message);}}
async function remove(record){if(!await ask('解除 '+record.ip+' 的上传封禁？计数将重新开始。',{title:'解除封禁',accept:'解除封禁'}))return;try{await request('/api/blacklist/'+encodeURIComponent(record.id),{method:'DELETE'});await load();}catch(value){toast(value.message);}}
async function go(value){page.value=value;await load();}
onMounted(load);
</script>
<template><SettingsPage title="IP 黑名单" description="查看自动封禁记录，也可以手动封禁或解除。" :busy="busy" :error="error" @retry="load"><section class="settings-panel"><RouterLink to="/admin/public-upload" class="text-button">配置自动封禁规则</RouterLink><form class="settings-form-grid" novalidate @submit.prevent="add"><FormField v-model="ip" label="IP 地址" placeholder="192.0.2.10" /><FormField v-model="reason" label="封禁原因" placeholder="可选" /><UiButton type="submit" variant="primary">加入黑名单</UiButton></form><div class="key-list"><article v-for="record in data.records" :key="record.id" class="key-row"><div><strong>{{ record.ip }}</strong><p>{{ record.reason || '手动封禁' }}</p><small>{{ record.createdAt }}</small></div><UiButton @click="remove(record)">解除封禁</UiButton></article><p v-if="!data.records.length" class="empty">当前没有被封禁的 IP。</p></div><Pagination :page="page" :pages="Math.max(1,data.pagination.totalPages)" :total="data.pagination.total" @change="go" /></section></SettingsPage></template>
