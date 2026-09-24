<script setup>
defineProps({ page:Number, pages:Number, total:Number, busy:Boolean });
defineEmits(['change']);
</script>
<template><nav class="pagination" aria-label="图片分页"><UiButton :disabled="busy || page<=1" @click="$emit('change',page-1)">上一页</UiButton><span>第 {{ page }} 页 · 共 {{ total }} 张</span><UiButton :disabled="busy || page>=pages" @click="$emit('change',page+1)">下一页</UiButton></nav></template>
