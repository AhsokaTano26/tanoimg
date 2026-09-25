<script setup>
import { computed, onMounted, ref } from 'vue';
import { request, toast } from '../runtime.js';
import FormField from '../components/FormField.vue';

const days = ref(30);
const stats = ref(null);
const pricing = ref({ currency:'USD', egressPerGiB:0 });
const loading = ref(false), saving = ref(false), error = ref(''), pricingError = ref('');
const symbols = { USD:'$', CNY:'¥', EUR:'€', GBP:'£', JPY:'¥' };
const categoryNames = { original:'原图', thumbnail:'缩略图', share:'分享文件' };
const money = computed(() => { const currency=stats.value?.pricing?.currency || pricing.value.currency; return `${symbols[currency] || ''}${Number(stats.value?.estimatedEgressCost || 0).toFixed(2)} ${currency}`; });
const size = bytes => bytes >= 1073741824 ? `${(bytes / 1073741824).toFixed(2)} GiB` : `${(bytes / 1048576).toFixed(2)} MiB`;

async function load() {
  loading.value = true; error.value = '';
  try { stats.value = await request(`/api/admin/transfer-stats?days=${days.value}`); }
  catch (cause) { error.value = cause.message; }
  finally { loading.value = false; }
}
async function loadPricing() {
  pricingError.value = '';
  try { pricing.value = await request('/api/admin/transfer-pricing'); }
  catch (cause) { pricingError.value = cause.message; }
}
async function savePricing() {
  const rate = Number(pricing.value.egressPerGiB);
  if (!Number.isFinite(rate) || rate < 0 || rate > 1000000) { pricingError.value = '每 GiB 单价须为 0–1,000,000'; return; }
  saving.value = true; pricingError.value = '';
  try {
    pricing.value = await request('/api/admin/transfer-pricing', { method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({currency:pricing.value.currency,egressPerGiB:rate}) });
    await load(); toast('费用估算设置已保存');
  } catch (cause) { pricingError.value = cause.message; }
  finally { saving.value = false; }
}
onMounted(() => { loadPricing(); load(); });
</script>

<template>
  <section class="page-view transfer-view">
    <div class="section-title"><div><span class="eyebrow">TRANSFER</span><h1>传输用量</h1><p>统计应用返回的图片流量，并按自定义单价估算费用。</p></div><UiButton icon="refresh-cw" :loading="loading" @click="load">刷新</UiButton></div>
    <section class="settings-panel"><div class="transfer-heading"><h2>流量概览</h2><UiSelect v-model="days" aria-label="统计时间范围" :options="[{label:'最近 7 天',value:7},{label:'最近 30 天',value:30},{label:'最近 90 天',value:90},{label:'最近 365 天',value:365}]" @update:model-value="load" /></div>
      <p v-if="error" role="alert" class="form-error">{{ error }} <UiButton @click="load">重试</UiButton></p>
      <p v-else-if="loading && !stats" role="status">正在读取传输统计…</p>
      <template v-else-if="stats"><div class="stats-row"><div><strong>{{ stats.requests.toLocaleString() }}</strong><span>图片请求</span></div><div><strong>{{ size(stats.bytes) }}</strong><span>传输数据</span></div><div><strong>{{ money }}</strong><span>估算流出费用</span></div></div>
        <p class="field-help">只统计经应用返回的原图、缩略图和分享文件。CDN、反向代理及服务商账单可能不同。</p>
        <p v-if="stats.droppedEvents" class="form-error">有 {{ stats.droppedEvents }} 次事件未计入统计。</p>
      </template>
    </section>
    <section class="settings-panel"><h2>费用估算</h2><p class="field-help">填写服务商的流出流量单价，仅用于页面估算。</p><form class="transfer-form" @submit.prevent="savePricing"><FormField v-model="pricing.currency" label="货币" type="select" :options="['USD','CNY','EUR','GBP','JPY'].map(value=>({label:value,value}))" /><FormField v-model="pricing.egressPerGiB" label="每 GiB 单价" type="number" :min="0" :max="1000000" :step="0.01" /><p v-if="pricingError" role="alert" class="form-error">{{ pricingError }}</p><UiButton type="submit" variant="primary" :loading="saving">保存单价</UiButton></form></section>
    <section class="settings-panel"><h2>每日明细</h2><p v-if="!stats?.daily?.length" class="empty">当前时间范围暂无传输记录。</p><div v-else class="transfer-list"><div v-for="row in stats.daily" :key="`${row.day}-${row.category}`"><strong>{{ row.day }}</strong><span>{{ categoryNames[row.category] || row.category }}</span><span>{{ row.requests.toLocaleString() }} 次</span><span>{{ size(row.bytes) }}</span></div></div></section>
  </section>
</template>

<style scoped>
.transfer-view{display:grid;gap:20px}.transfer-heading{display:flex;align-items:center;justify-content:space-between;gap:16px}.transfer-heading h2{margin:0}.transfer-heading .ui-select{width:170px}.transfer-form{display:grid;gap:14px;max-width:540px;margin-top:20px}.transfer-form>.ui-button{justify-self:start}.transfer-list{display:grid;gap:8px}.transfer-list>div{display:grid;grid-template-columns:1fr 1fr 1fr 1fr;gap:10px;align-items:center;padding:12px;border:1px solid var(--border);border-radius:8px;font-size:13px}.transfer-list span{color:var(--secondary)}@media(max-width:640px){.transfer-heading{align-items:flex-start;flex-wrap:wrap}.transfer-list>div{grid-template-columns:1fr 1fr}}
</style>
