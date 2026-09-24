<script setup>
import Icon from './Icon.vue';
import {workspaceLinks,settingsLinks} from '../navigation.js';
defineEmits(['navigate']);
</script>
<template><nav class="admin-navigation" aria-label="后台导航"><span class="nav-group-label">工作空间</span><RouterLink v-for="item in workspaceLinks" :key="item.path" :to="item.path" class="nav-link" active-class="active" @click="$emit('navigate')"><Icon :name="item.icon" />{{ item.name }}</RouterLink><span class="nav-group-label">站点管理</span><RouterLink v-for="item in settingsLinks" :key="item.path" :to="item.path" class="nav-link" active-class="active" @click="$emit('navigate')"><Icon :name="item.icon" />{{ item.name }}</RouterLink></nav></template>
