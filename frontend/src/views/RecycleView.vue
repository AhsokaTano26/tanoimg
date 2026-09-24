<script setup>
import {onMounted} from 'vue';
import ImageGrid from '../components/ImageGrid.vue';
import Pagination from '../components/Pagination.vue';
import {useImages} from '../composables/useImages.js';
const {images,page,pages,total,busy,error,load,go}=useImages('/api/images/deleted');
onMounted(load);
</script>
<template><section class="page-view"><div class="section-title"><div><span class="eyebrow">RECYCLE BIN</span><h1>回收站</h1><p>在永久清空前，图片可以恢复到图库。</p></div><RouterLink to="/admin/gallery" class="outline-button">返回图库</RouterLink></div><ImageGrid recycle :images="images" :busy="busy" :error="error" @refresh="load" /><Pagination :page="page" :pages="pages" :total="total" :busy="busy" @change="go" /></section></template>
