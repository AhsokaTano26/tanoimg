<script setup>
defineProps({title:String,description:String,busy:Boolean,error:String});
defineEmits(['retry']);
</script>
<template><section class="page-view"><div class="section-title"><div><span class="eyebrow">SITE MANAGEMENT</span><h1>{{ title }}</h1><p>{{ description }}</p></div></div><div v-if="busy" class="loading-panel" role="status">正在加载设置…</div><div v-else-if="error" class="empty"><p role="alert">{{ error }}</p><UiButton @click="$emit('retry')">重新加载</UiButton></div><div v-else class="settings-content"><slot /></div></section></template>
