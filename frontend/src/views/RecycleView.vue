<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import Icon from '../components/Icon.vue';
import { mountPage } from '../page-controller.js';
const props = defineProps({ admin: Boolean });
const root = ref(null);
const router = useRouter();
let controller;
onMounted(() => { controller = mountPage(root.value, 'recycle', props.admin, router); });
onBeforeRouteLeave(() => controller?.canLeave());
onBeforeUnmount(() => controller?.dispose());
</script>
<template>
<section ref="root" id="recycle-view" class="page-view">
      <div class="section-title"><div><h1>回收站</h1><p>在清空回收站前，删除的图片都可以恢复。</p></div><RouterLink to="/admin/gallery" class="outline-button"><svg class="icon" aria-hidden="true"><use href="/icons.svg#arrow-left"></use></svg> 返回图库</RouterLink></div>
      <div id="recycle-content"><div id="recycle-grid" class="gallery-grid"></div><div class="pagination"><button id="recycle-prev" class="outline-button" type="button">上一页</button><span id="recycle-page-label">第 1 页</span><button id="recycle-next" class="outline-button" type="button">下一页</button></div></div>
    </section>
</template>
