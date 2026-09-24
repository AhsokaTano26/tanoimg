<script setup>
import {ref} from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import {request,toast} from '../../runtime.js';
const data=ref(null),busy=ref(false);
async function check(){busy.value=true;try{data.value=await request('/api/version/check');}catch(error){toast(error.message);}finally{busy.value=false;}}
</script>
<template><SettingsPage title="关于与版本" description="轻量的 Go 图床，兼容 EasyImg 数据迁移。"><section class="settings-panel form-stack"><h2>TanoImg</h2><p>图片流式保存到本地磁盘，元数据存储于 SQLite。图标与界面组件在本地打包。</p><UiButton icon="refresh-cw" :loading="busy" @click="check">检查新版本</UiButton><p v-if="data" role="status">当前 {{ data.currentVersion }} · {{ data.error || (data.hasUpdate?'可更新至 '+data.latestVersion:'已是最新版本') }}</p></section></SettingsPage></template>
