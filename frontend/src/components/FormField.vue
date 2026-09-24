<script setup>
import {useId} from 'vue';
const props=defineProps({label:String,type:{default:'text'},options:Array,help:String,min:Number,max:Number,step:Number,placeholder:String,disabled:Boolean,required:Boolean});
const model=defineModel();
const id=useId();
</script>
<template><div class="form-field"><UiCheckbox v-if="type==='checkbox'" v-model="model" :label="label" :disabled="disabled" /><template v-else><label :for="id">{{ label }}<span v-if="required" class="required-mark"> *</span></label><UiSelect v-if="type==='select'" v-model="model" :inputId="id" :aria-label="label" :options="options" :disabled="disabled" /><UiInput v-else v-model="model" :id="type==='number'?undefined:id" :inputId="type==='number'?id:undefined" :type="type" :min="min" :max="max" :step="step" :maxFractionDigits="step && step<1 ? 2 : 0" :placeholder="placeholder" :disabled="disabled" :aria-label="label" /></template><small v-if="help">{{ help }}</small></div></template>
