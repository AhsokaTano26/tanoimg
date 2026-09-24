<script setup>
import { watch } from 'vue';
import { useImages } from '../composables/useImages.js';
import Pagination from './Pagination.vue';
const visible = defineModel('visible', { type:Boolean });
const emit = defineEmits(['select']);
const { images,page,pages,total,busy,error,load,go } = useImages('/api/images',24);
watch(visible, value => { if(value) load(); });
function choose(image) { emit('select',image); visible.value=false; }
</script>
<template><UiDialog v-model:visible="visible" title="从图库选择图片" width="800px"><p class="field-help">选择一张图片作为背景或网站 Logo。</p><p v-if="error" role="alert">{{ error }}</p><div class="picker-grid" :aria-busy="busy"><UiButton v-for="image in images" :key="image.id" class="picker-image" :aria-label="'选择 '+image.originalName" @click="choose(image)"><img :src="image.url" :alt="image.originalName" loading="lazy" /><span>{{ image.originalName }}</span></UiButton></div><p v-if="!busy && !images.length" class="empty">图库中还没有图片。</p><Pagination :page="page" :pages="pages" :total="total" :busy="busy" @change="go" /></UiDialog></template>
