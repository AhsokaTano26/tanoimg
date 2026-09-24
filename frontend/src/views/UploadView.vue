<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import Icon from '../components/Icon.vue';
import { mountPage } from '../page-controller.js';
const props = defineProps({ admin: Boolean });
const root = ref(null);
const router = useRouter();
let controller;
onMounted(() => { controller = mountPage(root.value, 'upload', props.admin, router); });
onBeforeRouteLeave(() => controller?.canLeave());
onBeforeUnmount(() => controller?.dispose());
</script>
<template>
<section ref="root" id="upload-view" class="page-view">
      <div class="page-heading"><span class="eyebrow">UPLOAD &amp; SHARE</span><h1>{{ admin ? "上传图片" : "公共上传" }}</h1><p>{{ admin ? "批量上传到你的图库，随时复制链接分享。" : "将图片上传至公共图库，生成可以分享的链接。" }}</p></div>
      <div id="dropzone" class="dropzone" tabindex="0" role="button" aria-label="选择图片上传">
        <input id="file-input" type="file" accept="image/*" multiple hidden>
        <div class="upload-icon" aria-hidden="true"><svg class="icon" aria-hidden="true"><use href="/icons.svg#image-up"></use></svg></div>
        <h2>将图片拖放至此</h2>
        <p>也可以选择本地图片，或直接粘贴上传</p>
        <span class="upload-button"><svg class="icon" aria-hidden="true"><use href="/icons.svg#plus"></use></svg>选择图片</span>
        <small id="upload-hint">支持 JPG、PNG、GIF、WebP 等常见格式</small>
      </div>
      <div class="upload-notes"><span><b>01</b> 本地存储</span><span><b>02</b> 批量上传</span><span><b>03</b> 即刻分享</span></div>
      <div id="upload-progress" class="upload-progress" hidden>
        <div class="upload-progress-top"><div><strong id="upload-progress-title">准备上传</strong><span id="upload-progress-meta">0 成功 · 0 失败</span></div><div class="upload-progress-actions"><button id="retry-failed" class="outline-button" type="button" hidden>重试失败项</button><button id="cancel-upload" class="text-button" type="button">取消剩余上传</button></div></div>
        <progress id="upload-progress-bar" max="1" value="0" aria-label="批量上传进度"></progress>
      </div>
      <div id="upload-results" class="upload-results"></div>
      <details v-if="admin" id="url-upload-panel" class="settings-panel url-upload-panel"><summary><span><svg class="icon" aria-hidden="true"><use href="/icons.svg#link"></use></svg> 从链接导入图片</span><span><svg class="icon" aria-hidden="true"><use href="/icons.svg#plus"></use></svg></span></summary><form id="url-upload-form"><label class="setting-field">图片 URL<textarea id="url-list" rows="4" placeholder="https://example.com/photo.jpg" required></textarea></label><div class="inline-actions"><button class="primary-button" type="submit">开始下载</button><span id="url-upload-status" class="field-help" role="status"></span></div></form></details>
      <div class="section-head"><h2>最近上传</h2><RouterLink class="text-button" :to="admin ? '/admin/gallery' : '/'">查看图库 <span aria-hidden="true"><svg class="icon" aria-hidden="true"><use href="/icons.svg#arrow-right"></use></svg></span></RouterLink></div>
      <div id="recent-grid" class="gallery-grid"></div>
    </section>
</template>
