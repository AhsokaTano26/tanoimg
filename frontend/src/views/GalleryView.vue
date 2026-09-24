<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import Icon from '../components/Icon.vue';
import { mountPage } from '../page-controller.js';
const props = defineProps({ admin: Boolean });
const root = ref(null);
const router = useRouter();
let controller;
onMounted(() => { controller = mountPage(root.value, 'gallery', props.admin, router); });
onBeforeRouteLeave(() => controller?.canLeave());
onBeforeUnmount(() => controller?.dispose());
</script>
<template>
<section ref="root" id="gallery-view" class="page-view">
      <div class="section-title"><div><span class="eyebrow">{{ admin ? "LIBRARY" : "PUBLIC GALLERY" }}</span><h1>{{ admin ? "图片管理" : "图片" }}</h1><p>{{ admin ? "管理全部图片，删除的图片可在回收站恢复。" : "浏览公开的影像，保存值得分享的灵感。" }}</p></div><div class="inline-actions"><RouterLink v-if="!admin" to="/upload" class="primary-button"><Icon name="upload" />公共上传</RouterLink><RouterLink v-if="admin" to="/admin/recycle" class="outline-button">回收站 <svg class="icon" aria-hidden="true"><use href="/icons.svg#arrow-right"></use></svg></RouterLink><button id="refresh-gallery" class="outline-button" type="button">刷新图库 <svg class="icon" aria-hidden="true"><use href="/icons.svg#refresh-cw"></use></svg></button></div></div>
      <div v-if="admin" id="gallery-selection" class="gallery-selection"><span id="selected-count">已选择 0 张</span><button id="delete-selected" class="outline-button" type="button" disabled>删除所选</button></div>
      <div id="gallery-grid" class="gallery-grid"></div>
      <div class="pagination"><button id="prev-page" class="outline-button">上一页</button><span id="page-label">第 1 页</span><button id="next-page" class="outline-button">下一页</button></div>
    </section>
</template>
