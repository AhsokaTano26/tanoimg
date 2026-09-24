<script setup>
import { onMounted } from 'vue';
import UploadPanel from '../components/UploadPanel.vue';
import ImageGrid from '../components/ImageGrid.vue';
import Pagination from '../components/Pagination.vue';
import { useImages } from '../composables/useImages.js';
const props=defineProps({ admin:Boolean });
const {images,page,pages,total,busy,error,load,go}=useImages(props.admin?'/api/images':'/api/images?scope=public');
onMounted(load);
</script>
<template><section class="page-view"><div class="section-title"><div><span class="eyebrow">{{ admin?'LIBRARY':'PUBLIC GALLERY' }}</span><h1>{{ admin?'图片管理':'图片' }}</h1><p>{{ admin?'整理每一张图片。右键打开分享与管理菜单。':'浏览公开影像，也分享你的灵感。' }}</p></div><div class="inline-actions"><RouterLink v-if="admin" to="/admin/recycle" class="outline-button">回收站</RouterLink><UiButton icon="refresh-cw" :loading="busy" @click="load">刷新图库</UiButton></div></div><UploadPanel v-if="!admin" id="public-upload" compact @uploaded="load" /><ImageGrid :images="images" :busy="busy" :error="error" :selectable="admin" @refresh="load" /><Pagination :page="page" :pages="pages" :total="total" :busy="busy" @change="go" /></section></template>
