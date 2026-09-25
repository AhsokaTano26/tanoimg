<script setup>
import { onMounted, ref } from 'vue';
import { request, toast } from '../runtime.js';
import { ask } from '../dialogs.js';

const shares = ref([]), page = ref(1), total = ref(0), busy = ref(false), error = ref(''), revoking = ref('');
const limit = 20;
const date = value => value ? new Date(value * 1000).toLocaleString() : '长期有效';
function status(item) {
  if (item.revokedAt) return '已撤销';
  if (item.expiresAt && item.expiresAt <= Date.now() / 1000) return '已过期';
  return '有效';
}
async function load() {
  busy.value = true; error.value = '';
  try {
    const data = await request(`/api/admin/shares?page=${page.value}&limit=${limit}`);
    shares.value = data.shares || [];
    total.value = data.pagination?.total || 0;
    if (page.value > 1 && !shares.value.length) { page.value--; await load(); }
  } catch (reason) { error.value = reason.message; }
  finally { busy.value = false; }
}
function move(delta) { page.value += delta; load(); }
async function revoke(item) {
  if (!await ask('撤销后，此分享链接及其中的图片文件链接将立即失效。',{title:'撤销分享',accept:'撤销分享',danger:true})) return;
  revoking.value = item.id;
  try { await request(`/api/admin/shares/${encodeURIComponent(item.id)}`,{method:'DELETE'}); toast('分享已撤销'); await load(); }
  catch (reason) { error.value = reason.message; }
  finally { revoking.value = ''; }
}
onMounted(load);
</script>

<template>
  <section class="page-view"><div class="section-title"><div><span class="eyebrow">SHARES</span><h1>分享管理</h1><p>查看分享状态并撤销链接。分享链接只在创建时显示，服务器不保存可再次查看的令牌。</p></div><div class="inline-actions"><RouterLink to="/admin/gallery" class="outline-button">返回图库</RouterLink><UiButton icon="refresh-cw" :loading="busy" @click="load">刷新</UiButton></div></div>
    <p v-if="error" role="alert" class="form-error">{{ error }} <UiButton @click="load">重试</UiButton></p>
    <div v-if="busy && !shares.length" class="loading-panel" role="status">正在读取分享…</div>
    <div v-else-if="!shares.length && !error" class="empty">暂无分享。前往图库选择图片，即可创建分享页。</div>
    <div v-else class="share-management-list"><article v-for="item in shares" :key="item.id" class="settings-panel share-management-item"><div><h2>{{ item.title || '未命名分享' }}</h2><p>{{ item.imageCount }} 张图片 · {{ status(item) }}</p><small>创建于 {{ item.createdAt }} · 到期时间 {{ date(item.expiresAt) }}</small></div><UiButton v-if="status(item)==='有效'" variant="danger" :loading="revoking===item.id" @click="revoke(item)">撤销</UiButton></article></div>
    <nav class="share-management-pages" aria-label="分享分页"><span>第 {{ page }} 页 · 共 {{ total }} 个分享</span><UiButton :disabled="busy || page<=1" @click="move(-1)">上一页</UiButton><UiButton :disabled="busy || page*limit>=total" @click="move(1)">下一页</UiButton></nav>
  </section>
</template>

<style scoped>
.share-management-list {display:grid;gap:12px}.share-management-item {display:flex;align-items:center;justify-content:space-between;gap:20px}.share-management-item h2 {font-size:16px;margin:0 0 8px}.share-management-item p {font-size:13px;color:var(--secondary);margin:0 0 6px}.share-management-item small {font-size:12px;color:var(--secondary)}.share-management-pages {display:flex;align-items:center;justify-content:flex-end;flex-wrap:wrap;gap:8px;margin-top:20px;font-size:13px;color:var(--secondary)}
</style>
