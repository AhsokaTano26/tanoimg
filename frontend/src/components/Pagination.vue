<script setup>
import { ref, watch } from 'vue';
const props = defineProps({ page:Number, pages:Number, total:Number, busy:Boolean, perPage:Number });
const emit = defineEmits(['change', 'per-page-change']);
const targetPage = ref(props.page || 1);
watch(() => props.page, value => { targetPage.value = value; });
function jump() {
  const value = Number(targetPage.value);
  if (!Number.isInteger(value) || value < 1 || value > Math.max(1, props.pages || 1)) {
    targetPage.value = props.page;
    return;
  }
  if (value !== props.page) emit('change', value);
}
</script>
<template>
  <nav class="pagination" aria-label="图片分页">
    <UiButton :disabled="busy || page<=1" @click="emit('change',1)">首页</UiButton>
    <UiButton :disabled="busy || page<=1" @click="emit('change',page-1)">上一页</UiButton>
    <span>第 {{ page }} / {{ Math.max(1,pages || 1) }} 页 · 共 {{ total }} 张</span>
    <UiButton :disabled="busy || page>=pages" @click="emit('change',page+1)">下一页</UiButton>
    <UiButton :disabled="busy || page>=pages" @click="emit('change',pages)">末页</UiButton>
    <form class="pagination-jump" @submit.prevent="jump">
      <label for="pagination-target">跳转到</label>
      <UiInput id="pagination-target" v-model="targetPage" type="number" :min="1" :max="Math.max(1,pages || 1)" :disabled="busy" aria-label="跳转页码" />
      <UiButton type="submit" :disabled="busy">跳转</UiButton>
    </form>
    <label v-if="perPage" class="pagination-size">每页
      <UiSelect :model-value="perPage" :options="[{label:'20 张',value:20},{label:'40 张',value:40},{label:'60 张',value:60},{label:'100 张',value:100}]" :disabled="busy" aria-label="每页图片数量" @update:model-value="emit('per-page-change',$event)" />
    </label>
  </nav>
</template>
<style scoped>
.pagination {display:flex;align-items:center;justify-content:center;gap:8px;flex-wrap:wrap}
.pagination-jump,.pagination-size {display:flex;align-items:center;gap:8px;font-size:13px;color:var(--secondary)}
.pagination-jump :deep(.ui-number) {width:78px}
.pagination-size :deep(.ui-select) {width:112px}
</style>
