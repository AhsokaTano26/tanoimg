<script setup>
import Select from 'primevue/select';
defineProps({ options: Array });
const model = defineModel();
</script>
<template><Select v-model="model" :options="options" optionLabel="label" optionValue="value" class="ui-select" appendTo="body" /></template>
