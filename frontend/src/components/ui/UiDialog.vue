<script setup>
import Dialog from 'primevue/dialog';
import Icon from '../Icon.vue';
defineProps({ title: String, width: { default:'520px' } });
const visible = defineModel('visible', { type: Boolean });
</script>
<template><Dialog v-model:visible="visible" modal :header="title" :style="{ width, maxWidth:'calc(100vw - 32px)' }" :draggable="false" class="ui-dialog" :closeButtonProps="{ 'aria-label':'关闭弹窗' }"><template #closeicon><Icon name="x" /></template><slot /><template v-if="$slots.footer" #footer><slot name="footer" /></template></Dialog></template>
