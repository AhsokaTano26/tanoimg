<script setup>
import Drawer from 'primevue/drawer';
import Icon from '../Icon.vue';
const visible=defineModel('visible',{type:Boolean});
</script>
<template><Drawer v-model:visible="visible" header="后台导航" class="ui-drawer" :closeButtonProps="{'aria-label':'关闭导航'}"><template #closeicon><Icon name="x" /></template><slot /></Drawer></template>
