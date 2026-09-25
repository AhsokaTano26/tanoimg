<script setup>
import {computed} from 'vue';
import {sanitizeAnnouncement} from '../announcement-html.js';
const props=defineProps({content:{type:String,default:''}});
const safeHtml=computed(()=>sanitizeAnnouncement(props.content));
</script>
<template><div class="announcement-content" v-html="safeHtml"></div></template>
