<script setup>
import { onMounted, reactive, ref } from 'vue';
import UploadPanel from '../components/UploadPanel.vue';
import ImageGrid from '../components/ImageGrid.vue';
import Pagination from '../components/Pagination.vue';
import { useImages } from '../composables/useImages.js';
const props=defineProps({ admin:Boolean });
const selectionBusy=ref(false);
const {images,page,pages,total,perPage,filters,busy,error,load,go,setPerPage,setFilters}=useImages(props.admin?'/api/images':'/api/images?scope=public');
const search=reactive({q:'',visibility:'',format:'',tag:'',sort:'newest'});
function applySearch(){if(selectionBusy.value)return;setFilters({...search,q:search.q.trim(),tag:search.tag.trim()});}
function clearSearch(){if(selectionBusy.value)return;Object.assign(search,{q:'',visibility:'',format:'',tag:'',sort:'newest'});setFilters({});}
onMounted(load);
</script>
<template>
  <section class="page-view">
    <div class="section-title"><div><span class="eyebrow">{{ admin?'LIBRARY':'PUBLIC GALLERY' }}</span><h1>{{ admin?'图片管理':'图片' }}</h1><p>{{ admin?'整理每一张图片。右键打开分享与管理菜单。':'浏览公开影像，也分享你的灵感。' }}</p></div><div class="inline-actions"><RouterLink v-if="admin" to="/admin/recycle" class="outline-button">回收站</RouterLink><UiButton icon="refresh-cw" :loading="busy" :disabled="selectionBusy" @click="load">刷新图库</UiButton></div></div>
    <UploadPanel v-if="!admin" id="public-upload" compact @uploaded="load" />
    <form class="gallery-filters" role="search" @submit.prevent="applySearch">
      <label>搜索文件名与描述<UiInput :disabled="selectionBusy" v-model="search.q" placeholder="文件名、替代文字、作者或许可" :maxlength="200" aria-label="搜索图片" /></label>
      <label v-if="admin">可见性<UiSelect :disabled="selectionBusy" v-model="search.visibility" :options="[{label:'全部',value:''},{label:'公开',value:'public'},{label:'不公开列出',value:'unlisted'},{label:'仅自己可见',value:'private'}]" aria-label="按可见性筛选" /></label>
      <label>格式<UiSelect :disabled="selectionBusy" v-model="search.format" :options="[{label:'全部格式',value:''},...['jpg','jpeg','png','gif','webp','avif','svg','bmp','ico','apng','tiff'].map(value=>({label:value.toUpperCase(),value}))]" aria-label="按格式筛选" /></label>
      <label>标签<UiInput :disabled="selectionBusy" v-model="search.tag" placeholder="精确标签" :maxlength="40" aria-label="按标签筛选" /></label>
      <label>排序<UiSelect :disabled="selectionBusy" v-model="search.sort" :options="[{label:'最新上传',value:'newest'},{label:'最早上传',value:'oldest'},{label:'文件最大',value:'largest'},{label:'文件最小',value:'smallest'}]" aria-label="图片排序" /></label>
      <div class="gallery-filter-actions"><UiButton type="submit" variant="primary" :disabled="busy || selectionBusy">筛选</UiButton><UiButton :disabled="busy || selectionBusy" @click="clearSearch">清除</UiButton></div>
    </form>
    <ImageGrid :images="images" :busy="busy" :error="error" :selectable="admin" :selection-filters="filters" @refresh="load" @selection-busy="selectionBusy=$event" />
    <Pagination :page="page" :pages="pages" :total="total" :per-page="perPage" :busy="busy || selectionBusy" @change="!selectionBusy && go($event)" @per-page-change="!selectionBusy && setPerPage($event)" />
  </section>
</template>
<style scoped>
.gallery-filters {display:grid;grid-template-columns:minmax(180px,2fr) repeat(4,minmax(120px,1fr)) auto;gap:12px;align-items:end;margin:0 0 24px;padding:20px;border:1px solid var(--border);border-radius:12px;background:var(--surface)}
.gallery-filters label {display:grid;gap:7px;min-width:0;font-size:12px;color:var(--secondary)}
.gallery-filter-actions {display:flex;gap:8px}
@media(max-width:1050px) {.gallery-filters {grid-template-columns:repeat(3,minmax(0,1fr))}}
@media(max-width:640px) {.gallery-filters {grid-template-columns:1fr}.gallery-filter-actions {flex-wrap:wrap}}
</style>
