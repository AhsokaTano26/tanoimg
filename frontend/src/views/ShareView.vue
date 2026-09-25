<script setup>
import { onBeforeUnmount, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { imageLinks } from '../image-links.js';
import { request, toast } from '../runtime.js';

const route = useRoute();
const share = ref(null), busy = ref(false), error = ref('');
let controller;
async function load() {
  controller?.abort();
  const current = controller = new AbortController();
  share.value = null; busy.value = true; error.value = '';
  try { share.value = await request(`/api/shares/${encodeURIComponent(route.params.token)}`,{signal:current.signal}); }
  catch (reason) { if (!current.signal.aborted) error.value = reason.message; }
  finally { if (controller === current) busy.value = false; }
}
async function copy(image, format) {
  try { await navigator.clipboard.writeText(imageLinks({...image,originalName:image.alt || image.originalName},location.origin)[format]); toast(`${format}已复制`); }
  catch { toast('复制失败，请检查剪贴板权限'); }
}
const expires = value => value ? new Date(value * 1000).toLocaleString() : '长期有效';
watch(() => route.params.token, load, {immediate:true});
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section class="page-view share-view">
    <div class="section-title"><div><span class="eyebrow">SHARED IMAGES</span><h1>{{ share?.title || '图片分享' }}</h1><p v-if="share">{{ share.images?.length || 0 }} 张图片 · {{ expires(share.expiresAt) }}</p><p v-else>通过专属链接查看图片。</p></div></div>
    <div v-if="busy" class="loading-panel" role="status">正在加载分享…</div>
    <div v-else-if="error" class="empty"><p role="alert">{{ error }}</p><UiButton @click="load">重新加载</UiButton></div>
    <div v-else-if="share && !share.images?.length" class="empty">这个分享目前没有可查看的图片。</div>
    <div v-else-if="share" :class="['share-gallery',{'share-gallery--single':share.images.length===1}]">
      <article v-for="image in share.images" :key="image.id" class="share-card">
        <div class="share-image"><img :src="image.url" :alt="image.alt || image.originalName || '分享图片'" loading="lazy" /></div>
        <div class="share-details"><h2>{{ image.originalName || image.filename }}</h2><p v-if="image.alt">{{ image.alt }}</p><p class="field-help">{{ image.format?.toUpperCase() }} · {{ image.width || '—' }} × {{ image.height || '—' }} · {{ (image.size / 1048576).toFixed(2) }} MB</p><p v-if="image.author" class="field-help">作者：{{ image.author }}</p><p v-if="image.license" class="field-help">授权：{{ image.license }}</p><div v-if="image.tags?.length" class="share-tags"><span v-for="tag in image.tags" :key="tag">{{ tag }}</span></div>
          <div class="inline-actions"><UiButton icon="link" @click="copy(image,'直链')">复制直链</UiButton><UiButton icon="code-xml" @click="copy(image,'HTML')">HTML</UiButton><UiButton @click="copy(image,'Markdown')">Markdown</UiButton><UiButton @click="copy(image,'BBCode')">BBCode</UiButton><a class="outline-button" :href="image.url" :download="image.originalName || image.filename">下载图片</a></div>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.share-gallery {display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:20px}.share-gallery--single {grid-template-columns:minmax(0,1fr);max-width:900px;margin:auto}.share-card {min-width:0;overflow:hidden;background:var(--surface);border:1px solid var(--border);border-radius:12px}.share-image {display:grid;place-items:center;min-height:220px;background:var(--surface-raised)}.share-image img {display:block;max-width:100%;max-height:75vh;object-fit:contain}.share-details {padding:20px}.share-details h2 {font-size:17px;overflow-wrap:anywhere}.share-details p {margin:8px 0;line-height:1.6;overflow-wrap:anywhere}.share-details .inline-actions {margin-top:16px;flex-wrap:wrap}.share-tags {display:flex;gap:6px;flex-wrap:wrap}.share-tags span {padding:4px 8px;background:var(--accent-soft);border-radius:5px;font-size:11px;color:var(--primary)}@media(max-width:720px){.share-gallery {grid-template-columns:1fr}}
</style>
