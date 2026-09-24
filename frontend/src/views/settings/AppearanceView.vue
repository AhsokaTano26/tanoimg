<script setup>
import {ref} from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import FormField from '../../components/FormField.vue';
import ImagePicker from '../../components/ImagePicker.vue';
import {useSettings} from '../../composables/useSettings.js';
import {request,updateSite,toast} from '../../runtime.js';
const {model,busy,saving,error,load,save}=useSettings('/api/settings');
const picker=ref(false),uploading=ref(false);
async function submit(){const result=await save({backgroundUrl:model.value.backgroundUrl,backgroundBlur:model.value.backgroundBlur},'/api/settings/appearance');if(result)updateSite(result);}
async function uploaded(files){if(!files[0])return;uploading.value=true;const body=new FormData();body.append('file',files[0]);try{const image=await request('/api/upload/private',{method:'POST',body});model.value.backgroundUrl=image.url;toast('背景已选好，保存后生效');}catch(reason){toast(reason.message);}finally{uploading.value=false;}}
async function reset(){model.value.backgroundUrl='';model.value.backgroundBlur=0;await submit();}
</script>
<template><SettingsPage title="外观与背景" description="从图库中挑选一张图片，为站点保留恰到好处的背景质感。" :busy="busy" :error="error" @retry="load"><form class="settings-panel" novalidate @submit.prevent="submit"><div class="appearance-layout"><div class="background-preview-wrap"><span class="field-label">背景预览</span><div class="background-preview" :style="{'--preview-image':model.backgroundUrl?'url('+JSON.stringify(model.backgroundUrl)+')':'none','--preview-blur':model.backgroundBlur+'px'}"><div class="preview-card">清晰的内容，自然的背景</div></div></div><div class="appearance-form"><FormField v-model="model.backgroundUrl" label="背景图片 URL" placeholder="图片地址或从图库选择" /><div class="inline-actions"><UiButton icon="images" @click="picker=true">从图库选择</UiButton><UiFile label="上传背景图片" :disabled="uploading" @select="uploaded" /></div><div class="blur-heading"><label id="background-blur-label">背景模糊度</label><output>{{ model.backgroundBlur }} px</output></div><UiSlider v-model="model.backgroundBlur" :min="0" :max="40" aria-labelledby="background-blur-label" /><div class="inline-actions"><UiButton type="submit" variant="primary" :loading="saving">保存外观</UiButton><UiButton @click="reset">恢复默认背景</UiButton></div></div></div></form><ImagePicker v-model:visible="picker" @select="model.backgroundUrl=$event.url" /></SettingsPage></template>
