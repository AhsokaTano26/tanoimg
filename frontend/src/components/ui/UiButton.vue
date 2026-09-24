<script setup>
import Button from 'primevue/button';
import Icon from '../Icon.vue';
defineProps({ variant: { default: 'secondary' }, icon: String, loading: Boolean });
</script>
<template><Button type="button" :class="['ui-button','ui-button--'+variant]" :loading="loading"><Icon v-if="icon && !loading" :name="icon" /><slot /></Button></template>
