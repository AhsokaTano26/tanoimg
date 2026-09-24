<script setup>
import InputText from 'primevue/inputtext';
import InputNumber from 'primevue/inputnumber';
import Textarea from 'primevue/textarea';
defineOptions({ inheritAttrs: false });
defineProps({ type: { default:'text' } });
const model = defineModel();
</script>
<template>
  <InputNumber v-if="type==='number'" v-model="model" v-bind="$attrs" class="ui-number" :useGrouping="false" />
  <Textarea v-else-if="type==='textarea'" v-model="model" v-bind="$attrs" class="ui-input" autoResize />
  <InputText v-else v-model="model" v-bind="$attrs" :type="type" class="ui-input" />
</template>
