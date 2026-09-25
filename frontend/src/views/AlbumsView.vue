<script setup>
import { onBeforeUnmount, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import Icon from '../components/Icon.vue';
import { imageLinks } from '../image-links.js';
import { request, toast } from '../runtime.js';

const route = useRoute();
const albums = ref([]), album = ref(null), images = ref([]);
const page = ref(1), pages = ref(1), total = ref(0), busy = ref(false), error = ref('');
let controller;
async function load() {
  controller?.abort();
  const current = controller = new AbortController();
  busy.value = true; error.value = '';
  try {
    if (route.params.id) {
      const data = await request(`/api/albums/${encodeURIComponent(route.params.id)}`,{signal:current.signal});
      if (current.signal.aborted) return;
      album.value = data.album; images.value = data.images || [];
    } else {
      const data = await request(`/api/albums?page=${page.value}&limit=20`,{signal:current.signal});
      if (current.signal.aborted) return;
      albums.value = data.albums || []; total.value = data.pagination?.total || 0; pages.value = Math.max(1,data.pagination?.totalPages || 0);
    }
  } catch (reason) { if (!current.signal.aborted) error.value = reason.message; }
  finally { if (controller === current) busy.value = false; }
}
function move(delta) { page.value += delta; load(); }
async function copy(image) {
  try { await navigator.clipboard.writeText(imageLinks({...image,originalName:image.alt || image.originalName},location.origin)['直链']); toast('图片链接已复制'); }
  catch { toast('复制失败，请检查剪贴板权限'); }
}
watch(() => route.params.id, () => { page.value = 1; album.value = null; images.value = []; load(); }, {immediate:true});
onBeforeUnmount(() => controller?.abort());
</script>

<template>
  <section class="page-view albums-view">
    <div class="section-title"><div><span class="eyebrow">ALBUMS</span><h1>{{ route.params.id ? album?.title || '相册' : '相册' }}</h1><p>{{ route.params.id ? album?.description || '按顺序浏览相册中的图片。' : '浏览公开相册。' }}</p></div><div class="inline-actions"><RouterLink v-if="route.params.id" to="/albums" class="outline-button">返回相册</RouterLink><UiButton icon="refresh-cw" :loading="busy" @click="load">刷新</UiButton></div></div>
    <div v-if="busy && !(route.params.id ? album : albums.length)" class="loading-panel" role="status">正在加载相册…</div>
    <div v-else-if="error" class="empty"><p role="alert">{{ error }}</p><UiButton @click="load">重新加载</UiButton></div>
    <template v-else-if="!route.params.id">
      <div v-if="!albums.length" class="empty">暂无公开相册。</div>
      <div v-else class="album-cards"><RouterLink v-for="item in albums" :key="item.id" :to="`/albums/${encodeURIComponent(item.id)}`" class="album-card"><span class="album-card-icon"><Icon name="images" /></span><h2>{{ item.title }}</h2><p>{{ item.description || '浏览相册图片' }}</p><small>{{ item.imageCount }} 张图片</small></RouterLink></div>
      <nav class="album-pages" aria-label="相册分页"><span>第 {{ page }} / {{ pages }} 页 · 共 {{ total }} 个相册</span><UiButton :disabled="busy || page<=1" @click="move(-1)">上一页</UiButton><UiButton :disabled="busy || page>=pages" @click="move(1)">下一页</UiButton></nav>
    </template>
    <template v-else-if="album"><p v-if="album.visibility==='unlisted'" class="field-help">此相册未在公开列表展示，可通过当前链接访问。</p><p v-if="!images.length" class="empty">这个相册目前没有可查看的图片。</p><div v-else class="album-images"><article v-for="image in images" :key="image.id" class="album-image"><img :src="image.url" :alt="image.alt || image.originalName || '相册图片'" loading="lazy" /><div><h2>{{ image.originalName || image.filename }}</h2><p v-if="image.alt">{{ image.alt }}</p><small>{{ image.width || '—' }} × {{ image.height || '—' }}</small><div class="inline-actions"><UiButton icon="link" @click="copy(image)">复制直链</UiButton><a class="outline-button" :href="image.url" :download="image.originalName || image.filename">下载</a></div></div></article></div></template>
  </section>
</template>

<style scoped>
.album-cards {display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:16px}.album-card {display:block;padding:24px;background:var(--surface);border:1px solid var(--border);border-radius:12px;color:var(--text);text-decoration:none}.album-card:hover {border-color:var(--primary)}.album-card-icon {display:grid;place-items:center;width:48px;height:48px;border-radius:10px;background:var(--accent-soft);color:var(--primary)}.album-card h2,.album-image h2 {font-size:17px;margin:16px 0 8px}.album-card p {color:var(--secondary);line-height:1.6;overflow-wrap:anywhere}.album-card small,.album-image small {color:var(--secondary)}.album-pages {display:flex;gap:8px;align-items:center;justify-content:flex-end;flex-wrap:wrap;margin-top:20px;font-size:13px;color:var(--secondary)}.album-images {display:grid;gap:20px}.album-image {display:grid;grid-template-columns:minmax(180px,1fr) minmax(180px,1fr);gap:20px;align-items:center;padding:20px;background:var(--surface);border:1px solid var(--border);border-radius:12px}.album-image img {display:block;max-width:100%;max-height:70vh;margin:auto}.album-image p {line-height:1.6}.album-image .inline-actions {margin-top:16px}@media(max-width:760px){.album-cards {grid-template-columns:1fr}.album-image {grid-template-columns:1fr}}
</style>
