<script setup>
import {computed} from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import ImageGrid from '../../components/ImageGrid.vue';
import {useSettings} from '../../composables/useSettings.js';
import {request,toast} from '../../runtime.js';
import {ask} from '../../dialogs.js';
const {model,busy,error,load}=useSettings('/api/settings/stats');
async function clear(path,label){if(!await ask(label+'会永久删除对应文件，无法恢复。',{title:label,accept:'永久删除',danger:true}))return;try{const data=await request(path,{method:'POST'});toast('已清理 '+data.deletedCount+' 张图片');await load();}catch(reason){toast(reason.message);}}
</script>
<template><SettingsPage title="存储清理" description="查看待清理空间，管理已删除和违规图片。" :busy="busy" :error="error" @retry="load"><div class="stats-row"><div><strong>{{ model.deletedImagesCount }}</strong><span>回收站图片</span></div><div><strong>{{ (model.deletedSize/1048576).toFixed(1) }} MB</strong><span>待清理空间</span></div><div><strong>{{ model.nsfwImagesCount }}</strong><span>违规图片</span></div><div><strong>{{ (model.activeSize/1048576).toFixed(1) }} MB</strong><span>活跃空间</span></div></div><section class="settings-panel form-stack"><h2>回收站</h2><p>图片可以在回收站中恢复。永久清空后将无法恢复。</p><div class="inline-actions"><RouterLink to="/admin/recycle" class="outline-button">查看回收站</RouterLink><UiButton variant="danger" @click="clear('/api/settings/hard-delete','清空回收站')">永久清空回收站</UiButton></div></section><section class="settings-panel form-stack"><h2>违规图片</h2><p>复核被标记的图片，或永久清理这些文件。</p><div class="inline-actions"><RouterLink to="/admin/moderation-images" class="outline-button">复核违规图片</RouterLink><UiButton variant="danger" @click="clear('/api/images/nsfw-clear','清空违规图片')">永久清空违规图片</UiButton></div></section></SettingsPage></template>
