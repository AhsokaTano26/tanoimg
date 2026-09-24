<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import Icon from '../components/Icon.vue';
import { mountPage } from '../page-controller.js';
const props = defineProps({ admin: Boolean });
const root = ref(null);
const router = useRouter();
let controller;
onMounted(() => { controller = mountPage(root.value, 'api', props.admin, router); });
onBeforeRouteLeave(() => controller?.canLeave());
onBeforeUnmount(() => controller?.dispose());
</script>
<template>
<section ref="root" id="api-view" class="page-view">
      <div class="section-title"><div><h1>API 使用说明</h1><p>使用管理员会话或 API Key 批量上传与管理图片。</p></div></div>
      <div class="api-grid"><section class="settings-panel"><h2>私有上传</h2><p>请求头传入 API Key，表单字段为 <code>file</code> 或 <code>image</code>。适合脚本和自动化客户端。</p><pre><code>curl -H 'X-API-Key: sk-...' \
  -F 'file=@photo.jpg' \
  https://img.example.com/api/upload/private</code></pre></section><section class="settings-panel"><h2>URL 上传</h2><p>发送一个 URL 或 URL 数组，服务端下载后保存。大量 URL 可使用流式进度接口。</p><pre><code>POST /api/upload/url
{"url":"https://example.com/photo.jpg"}

POST /api/upload/urls
{"urls":["https://example.com/a.jpg"]}</code></pre></section><section class="settings-panel"><h2>公开上传</h2><p>管理员开启公开上传后，无须密钥。格式、大小、频率和内容审核由站点配置控制。</p><pre><code>POST /api/upload/public
Content-Type: multipart/form-data
file=@photo.jpg</code></pre></section><section class="settings-panel"><h2>响应与批量建议</h2><p>响应为 JSON；成功时图片信息位于 <code>data</code>。API Key 批量上传建议使用 4 路并发；收到 429 时按 Retry-After 重试。</p><pre><code>{"success":true,"data":{"url":"/i/uuid.jpg"}}</code></pre></section></div>
    </section>
</template>
