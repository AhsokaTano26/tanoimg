<script setup>
import { ref } from 'vue';
import ContextMenu from 'primevue/contextmenu';
import Icon from '../Icon.vue';
defineProps({ items: Array });
const menu = ref();
defineExpose({ show: event => menu.value.show(event), hide: () => menu.value.hide() });
</script>
<template><ContextMenu ref="menu" :model="items" class="ui-context-menu" aria-label="图片操作"><template #item="{ item, props }"><a v-bind="props.action" :class="{ 'menu-danger':item.danger }"><Icon v-if="item.symbol" :name="item.symbol" /><span>{{ item.label }}</span></a></template></ContextMenu></template>
