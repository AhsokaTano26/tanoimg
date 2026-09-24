<script setup>
import { watch } from 'vue';
import { dialog, settleDialog } from '../dialogs.js';
watch(() => dialog.visible, value => { if (!value) settleDialog(false); });
</script>
<template><UiDialog v-model:visible="dialog.visible" :title="dialog.title"><p class="dialog-message">{{ dialog.message }}</p><UiInput v-if="dialog.input" v-model="dialog.value" aria-label="编辑内容" @keydown.enter.prevent="settleDialog(true)" /><template #footer><UiButton @click="settleDialog(false)">取消</UiButton><UiButton :variant="dialog.danger ? 'danger' : 'primary'" @click="settleDialog(true)">{{ dialog.accept }}</UiButton></template></UiDialog></template>
