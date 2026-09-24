<script setup>
import {ref,onMounted} from 'vue';
import {useImages} from '../../composables/useImages.js';
import {request,toast} from '../../runtime.js';
import Pagination from '../../components/Pagination.vue';
const {images,page,pages,total,busy,error,load,go}=useImages('/api/images/nsfw');
const preview=ref(null),visible=ref(false);
async function restore(image){try{await request('/api/images/'+encodeURIComponent(image.id)+'/unmark-nsfw',{method:'PUT'});toast('已取消违规标记');await load();}catch(reason){toast(reason.message);}}
onMounted(load);
</script>
<template><section class="page-view"><div class="section-title"><div><h1>违规图片复核</h1><p>仅管理员可预览。确认误判后可取消违规标记。</p></div></div><p v-if="error" role="alert">{{ error }}</p><div class="key-list"><article v-for="image in images" :key="image.id" class="key-row"><strong>{{ image.originalName }}</strong><div class="inline-actions"><UiButton @click="preview=image;visible=true">预览</UiButton><UiButton @click="restore(image)">取消违规标记</UiButton></div></article><div v-if="!images.length" class="empty">{{ busy?'正在加载…':'没有违规图片' }}</div></div><Pagination :page="page" :pages="pages" :total="total" :busy="busy" @change="go" /><UiDialog v-model:visible="visible" title="违规图片预览" width="960px"><img v-if="preview" :src="preview.url" :alt="preview.originalName" class="preview-image"></UiDialog></section></template>
