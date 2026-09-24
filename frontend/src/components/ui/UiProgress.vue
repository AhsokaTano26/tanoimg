<script setup>
import ProgressBar from 'primevue/progressbar';
defineProps({ value: Number });
</script>
<template><ProgressBar :value="value" :showValue="false" class="ui-progress" /></template>
