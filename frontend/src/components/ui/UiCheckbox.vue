<script setup>
import { useId } from 'vue';
import Checkbox from 'primevue/checkbox';
import Icon from '../Icon.vue';
defineProps({ label: String, disabled: Boolean });
const model = defineModel({ type: Boolean });
const id = useId();
</script>
<template><label class="ui-checkbox" :for="id"><Checkbox v-model="model" :inputId="id" :disabled="disabled" binary><template #icon><Icon v-if="model" name="check" /></template></Checkbox><span>{{ label }}<slot /></span></label></template>
