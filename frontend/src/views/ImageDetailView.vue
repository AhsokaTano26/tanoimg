<script setup>
import { onBeforeUnmount, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { imageLinks } from '../image-links.js';
import { request, toast } from '../runtime.js';

const route = useRoute();
const image = ref(null), busy = ref(false), error = ref('');
let controller;
async function load() {
  controller?.abort();
  const current = controller = new AbortController();
  image.value = null; busy.value = true; error.value = '';
  try { image.value = await request(`/api/images/${encodeURIComponent(route.params.id)}`, {signal:current.signal}); }
  catch (reason) { if (!current.signal.aborted) error.value = reason.message; }
  finally { if (controller === current) busy.value = false; }
}
async function copy(format) {
  if (!image.value) return;
  try {
    await navigator.clipboard.writeText(imageLinks({...image.value, originalName:image.value.alt || image.value.originalName}, location.origin)[format]);
    toast(`${format}已复制`);
  } catch { toast('复制失败，请检查剪贴板权限'); }
}
watch(() => route.params.id, load, {immediate:true});
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section class="page-view image-detail-view">
    <div class="section-title"><div><span class="eyebrow">IMAGE DETAIL</span><h1>{{ image?.originalName || '图片详情' }}</h1><p>图片资料与引用方式</p></div><RouterLink class="outline-button" to="/">返回图库</RouterLink></div>
    <div v-if="busy" class="loading-panel" role="status">正在加载图片…</div>
    <div v-else-if="error" class="empty"><p role="alert">{{ error }}</p><UiButton @click="load">重新加载</UiButton></div>
    <article v-else-if="image" class="detail-card">
      <div class="detail-image"><img :src="image.url" :alt="image.alt || image.originalName || '图片'" /></div>
      <div class="detail-body"><div class="detail-heading"><div><span class="eyebrow">{{ image.format?.toUpperCase() }} / {{ image.width }} × {{ image.height }}</span><h2>{{ image.originalName || image.filename }}</h2></div><span class="status-chip">{{ image.visibility==='public'?'公开':image.visibility==='private'?'私人':'不公开列出' }}</span></div>
        <p v-if="image.alt">{{ image.alt }}</p><dl><div><dt>大小</dt><dd>{{ (image.size/1048576).toFixed(2) }} MB</dd></div><div><dt>上传时间</dt><dd>{{ new Date(image.uploadedAt).toLocaleString() }}</dd></div><div v-if="image.author"><dt>作者</dt><dd>{{ image.author }}</dd></div><div v-if="image.license"><dt>授权</dt><dd>{{ image.license }}</dd></div></dl>
        <div v-if="image.tags?.length" class="detail-tags"><span v-for="tag in image.tags" :key="tag">{{ tag }}</span></div>
        <div class="inline-actions"><UiButton icon="link" @click="copy('直链')">复制直链</UiButton><UiButton icon="code-xml" @click="copy('HTML')">HTML</UiButton><UiButton @click="copy('Markdown')">Markdown</UiButton><UiButton @click="copy('BBCode')">BBCode</UiButton><a class="outline-button" :href="image.url" :download="image.originalName || image.filename">下载图片</a></div>
      </div>
    </article>
  </section>
</template>

<style scoped>
.image-detail-view{max-width:1120px}.detail-card{overflow:hidden;background:var(--surface);border:1px solid var(--border);border-radius:12px}.detail-image{display:grid;place-items:center;min-height:320px;max-height:75vh;background:var(--surface-raised)}.detail-image img{display:block;max-width:100%;max-height:75vh;object-fit:contain}.detail-body{padding:24px}.detail-heading{display:flex;justify-content:space-between;gap:20px;align-items:flex-start}.detail-heading h2{font-size:22px;overflow-wrap:anywhere;margin:4px 0 0}.detail-body p{line-height:1.65}.detail-body dl{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:14px 24px;margin:24px 0}.detail-body dl>div{border-top:1px solid var(--border);padding-top:10px}.detail-body dt{color:var(--text-muted);font-size:12px}.detail-body dd{margin:4px 0 0;overflow-wrap:anywhere}.detail-tags{display:flex;flex-wrap:wrap;gap:6px;margin:16px 0}.detail-tags span{padding:4px 8px;background:var(--accent-soft);color:var(--primary);border-radius:5px;font-size:11px}.inline-actions{flex-wrap:wrap;margin-top:24px}@media(max-width:680px){.detail-body dl{grid-template-columns:1fr}.detail-heading{flex-wrap:wrap}}
</style>
