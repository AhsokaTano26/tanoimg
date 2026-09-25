<script setup>
import { onMounted, ref, watch } from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import FormField from '../../components/FormField.vue';
import { useSettings, jsonOptions } from '../../composables/useSettings.js';
import { request, toast } from '../../runtime.js';
import { ask } from '../../dialogs.js';

const { model, busy, error, load } = useSettings('/api/apikeys');
const scopes = [{value:'upload:file',label:'文件上传'},{value:'upload:url',label:'URL 导入'},{value:'upload:public',label:'公开发布'}];
const policies = [{label:'标准',value:'standard'},{label:'仅私人图片',value:'private-only'},{label:'仅文件上传',value:'file-only'},{label:'小尺寸私人图片',value:'small-private'}];
const blank = () => ({name:'',scopes:scopes.map(item=>item.value),policy:'standard',expiresAt:'',dailyCount:0,dailyBytes:0});
const form = ref(blank()), editing = ref(null), working = ref(false), keyWorking = ref('');
const secret = ref(''), secretDialog = ref(false), secretTitle = ref('');
const quota = ref({dailyCount:0,dailyBytes:0}), quotaLoaded = ref(false), quotaBusy = ref(false), quotaSaving = ref(false), quotaError = ref('');
watch(secretDialog, visible => { if (!visible) secret.value = ''; });
const usage = bytes => `${(bytes / 1048576).toFixed(1)} MiB`;
function toggleScope(scope, checked) { form.value.scopes = checked ? [...new Set([...form.value.scopes,scope])] : form.value.scopes.filter(value => value !== scope); }
function validQuota(count, bytes) { return Number.isInteger(Number(count)) && count >= 0 && count <= 1e9 && Number.isInteger(Number(bytes)) && bytes >= 0 && bytes <= 1099511627776; }
function payload() {
  const value = form.value;
  if (!value.name.trim()) throw new Error('请输入密钥名称');
  if (!value.scopes.length) throw new Error('至少选择一项权限');
  if (!validQuota(value.dailyCount,value.dailyBytes)) throw new Error('每日配额超出允许范围');
  let expiresAt = '';
  if (value.expiresAt) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(value.expiresAt)) throw new Error('到期日期无效');
    const date = new Date(`${value.expiresAt}T23:59:59Z`);
    if (Number.isNaN(date.getTime()) || date.toISOString().slice(0,10) !== value.expiresAt) throw new Error('到期日期无效');
    expiresAt = date.toISOString();
  }
  return {name:value.name.trim(),scopes:value.scopes,policy:value.policy,expiresAt,dailyCount:Number(value.dailyCount),dailyBytes:Number(value.dailyBytes)};
}
function reveal(raw,title) { secret.value = raw; secretTitle.value = title; secretDialog.value = true; }
function openEdit(key) { editing.value = key.id; form.value = {name:key.name,scopes:[...(key.scopes || [])],policy:key.policy || 'standard',expiresAt:key.expiresAt?.slice(0,10) || '',dailyCount:key.dailyCount || 0,dailyBytes:key.dailyBytes || 0}; }
function closeEdit() { editing.value = null; form.value = blank(); }
async function submit() {
  let body; try { body = payload(); } catch (reason) { toast(reason.message); return; }
  working.value = true;
  try {
    if (editing.value) { await request(`/api/apikeys/${encodeURIComponent(editing.value)}`,{...jsonOptions(body),method:'PUT'}); closeEdit(); await load(); toast('密钥设置已保存'); }
    else { const data = await request('/api/apikeys',jsonOptions(body)); form.value = blank(); await load(); reveal(data.key,'新 API 密钥'); }
  } catch (reason) { toast(reason.message); }
  finally { working.value = false; }
}
async function act(key,action) {
  if (action === 'rotate' && !await ask('重置后旧密钥立即失效。新密钥仅显示一次。',{title:'重置 API 密钥',accept:'重置',danger:true})) return;
  if (action === 'delete' && !await ask(`删除「${key.name}」？此操作不可撤销。`,{title:'删除 API 密钥',accept:'删除',danger:true})) return;
  keyWorking.value = key.id;
  try {
    const result = await request(`/api/apikeys/${encodeURIComponent(key.id)}`,{...jsonOptions(action === 'rotate' ? {regenerate:true} : {enabled:!key.enabled}),method:action === 'delete' ? 'DELETE' : 'PUT'});
    await load();
    if (action === 'rotate') reveal(result.key,'已重置的 API 密钥');
    else toast(action === 'delete' ? '密钥已删除' : '密钥状态已更新');
  } catch (reason) { toast(reason.message); }
  finally { keyWorking.value = ''; }
}
async function copySecret() { try { await navigator.clipboard.writeText(secret.value); toast('密钥已复制'); } catch { toast('复制失败，请手动复制密钥'); } }
async function loadQuota() { quotaBusy.value = true; quotaError.value = ''; try { quota.value = await request('/api/admin/upload-quota'); quotaLoaded.value = true; } catch (reason) { quotaError.value = reason.message; } finally { quotaBusy.value = false; } }
async function saveQuota() {
  if (!validQuota(quota.value.dailyCount,quota.value.dailyBytes)) { quotaError.value = '每日配额超出允许范围'; return; }
  quotaSaving.value = true; quotaError.value = '';
  try { quota.value = await request('/api/admin/upload-quota',{...jsonOptions({dailyCount:Number(quota.value.dailyCount),dailyBytes:Number(quota.value.dailyBytes)}),method:'PUT'}); toast('每日 IP 配额已保存'); }
  catch (reason) { quotaError.value = reason.message; }
  finally { quotaSaving.value = false; }
}
onMounted(loadQuota);
</script>
<template>
  <SettingsPage title="API 密钥" description="为脚本和第三方客户端管理上传凭证。密钥仅在创建或重置时显示一次。" :busy="busy" :error="error" @retry="load">
    <section class="settings-panel"><h2>{{ editing?'编辑密钥':'创建密钥' }}</h2><form class="key-security-form" novalidate @submit.prevent="submit">
      <FormField v-model="form.name" label="密钥名称" placeholder="例如：桌面客户端" />
      <div class="form-field"><span class="field-label">上传权限</span><div class="format-options"><UiCheckbox v-for="scope in scopes" :key="scope.value" :model-value="form.scopes.includes(scope.value)" :label="scope.label" @update:model-value="toggleScope(scope.value,$event)" /></div></div>
      <FormField v-model="form.policy" type="select" label="使用策略" :options="policies" />
      <FormField v-model="form.expiresAt" type="date" label="到期日期" help="留空表示长期有效；日期按 UTC 计算。" />
      <div class="key-quota-grid"><FormField v-model="form.dailyCount" type="number" label="每日上传次数" :min="0" :max="1000000000" help="0 表示不限。" /><FormField v-model="form.dailyBytes" type="number" label="每日上传字节数" :min="0" :max="1099511627776" help="0 表示不限；1 MiB = 1048576 字节。" /></div>
      <div class="inline-actions"><UiButton type="submit" variant="primary" :loading="working">{{ editing?'保存设置':'创建密钥' }}</UiButton><UiButton v-if="editing" @click="closeEdit">取消编辑</UiButton></div>
    </form></section>
    <section class="settings-panel"><h2>现有密钥</h2><div class="key-list"><article v-for="key in model" :key="key.id" class="key-row"><div><strong>{{ key.name }}</strong><span class="status-chip" :class="key.enabled?'success':'muted'">{{ key.enabled?'已启用':'已停用' }}</span><code>{{ key.keyHint }}</code><p>{{ (key.scopes || []).map(scope => scopes.find(option => option.value===scope)?.label || scope).join('、') }} · {{ policies.find(option => option.value===key.policy)?.label || key.policy }}</p><small>到期：{{ key.expiresAt || '长期有效' }} · 最近使用：{{ key.lastUsedAt || '尚未使用' }}</small><small>今日 {{ key.usedTodayCount || 0 }} / {{ key.dailyCount || '不限' }} 次 · {{ usage(key.usedTodayBytes || 0) }} / {{ key.dailyBytes ? usage(key.dailyBytes) : '不限' }}</small></div><div class="row-actions"><UiButton :disabled="keyWorking===key.id" @click="openEdit(key)">编辑设置</UiButton><UiButton :disabled="keyWorking===key.id" @click="act(key,'toggle')">{{ key.enabled?'停用':'启用' }}</UiButton><UiButton :disabled="keyWorking===key.id" @click="act(key,'rotate')">重置密钥</UiButton><UiButton variant="danger" :disabled="keyWorking===key.id" @click="act(key,'delete')">删除</UiButton></div></article><p v-if="!model?.length" class="empty">尚未创建 API 密钥。</p></div></section>
    <section class="settings-panel"><h2>站点每日 IP 上传配额</h2><p class="field-help">与单个密钥的配额同时生效。0 表示不限。</p><p v-if="quotaError" role="alert" class="form-error">{{ quotaError }} <UiButton @click="loadQuota">重试</UiButton></p><p v-if="quotaBusy && !quotaLoaded" role="status">正在读取配额…</p><form v-else-if="quotaLoaded" class="key-security-form" @submit.prevent="saveQuota"><div class="key-quota-grid"><FormField v-model="quota.dailyCount" type="number" label="每个 IP 每日上传次数" :min="0" :max="1000000000" /><FormField v-model="quota.dailyBytes" type="number" label="每个 IP 每日上传字节数" :min="0" :max="1099511627776" /></div><UiButton type="submit" variant="primary" :loading="quotaSaving">保存 IP 配额</UiButton></form></section>
  </SettingsPage>
  <UiDialog v-model:visible="secretDialog" :title="secretTitle"><div class="form-stack"><p>请立即保存此密钥。关闭后无法再次查看，只能重置。</p><label class="form-field"><span>API 密钥</span><UiInput :model-value="secret" readonly aria-label="新 API 密钥" /></label><div class="inline-actions"><UiButton icon="link" variant="primary" @click="copySecret">复制密钥</UiButton><UiButton @click="secretDialog=false">完成</UiButton></div></div></UiDialog>
</template>
<style scoped>
.key-security-form {display:grid;gap:18px;margin-top:18px;max-width:680px}.key-security-form>.ui-button {justify-self:start}.key-quota-grid {display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px}.key-row {gap:16px}.key-row>div:first-child {min-width:0;flex:1}.key-row code {margin-left:12px}.key-row small {display:block;margin-top:6px}.key-row p {margin:8px 0 0}.row-actions {display:flex;gap:8px}@media(max-width:640px){.key-quota-grid {grid-template-columns:1fr}}
</style>
