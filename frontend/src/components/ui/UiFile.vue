<script setup>
import { ref } from 'vue';
import FileUpload from 'primevue/fileupload';
import Icon from '../Icon.vue';
defineProps({ label: { default:'选择图片' }, multiple: Boolean, accept: { default:'image/*' }, disabled: Boolean });
const emit = defineEmits(['select']);
const picker = ref();
function selected(event) { emit('select', event.files); picker.value.clear(); }
</script>
<template><FileUpload ref="picker" mode="basic" :chooseLabel="label" :multiple="multiple" :accept="accept" :disabled="disabled" customUpload auto @select="selected" class="ui-file"><template #chooseicon><Icon name="plus" /></template></FileUpload></template>
