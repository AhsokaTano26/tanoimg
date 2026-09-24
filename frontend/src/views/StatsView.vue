<script setup>
import { ref, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave, useRouter } from 'vue-router';
import Icon from '../components/Icon.vue';
import { mountPage } from '../page-controller.js';
const props = defineProps({ admin: Boolean });
const root = ref(null);
const router = useRouter();
let controller;
onMounted(() => { controller = mountPage(root.value, 'stats', props.admin, router); });
onBeforeRouteLeave(() => controller?.canLeave());
onBeforeUnmount(() => controller?.dispose());
</script>
<template>
<section ref="root" id="stats-view" class="page-view">
      <div class="section-title"><div><h1>存储统计</h1><p>查看图片数量、磁盘占用和内容审核结果。</p></div><button id="refresh-stats" class="outline-button" type="button">刷新数据 <svg class="icon" aria-hidden="true"><use href="/icons.svg#refresh-cw"></use></svg></button></div>
      <div id="stats-content"><div class="stats-row"><div><strong id="report-total">0</strong><span>活跃图片</span></div><div><strong id="report-public">0</strong><span>公开上传</span></div><div><strong id="report-private">0</strong><span>私有上传</span></div><div><strong id="report-active-size">0 MB</strong><span>活跃空间</span></div></div><div class="stats-row"><div><strong id="report-deleted">0</strong><span>已删除图片</span></div><div><strong id="report-deleted-size">0 MB</strong><span>待清理空间</span></div><div><strong id="report-moderated">0</strong><span>已审核图片</span></div><div><strong id="report-nsfw">0</strong><span>违规图片</span></div></div><section class="settings-panel"><h2>审核违规率</h2><p><strong id="report-nsfw-rate" class="metric-value">0%</strong> 已审核图片中被标记违规的比例。</p></section></div>
    </section>
</template>
