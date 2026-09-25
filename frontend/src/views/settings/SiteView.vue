<script setup>
import {ref} from 'vue';
import SettingsPage from '../../components/SettingsPage.vue';
import FormField from '../../components/FormField.vue';
import ImagePicker from '../../components/ImagePicker.vue';
import {useSettings} from '../../composables/useSettings.js';
import {updateSite,toast} from '../../runtime.js';
const {model,busy,saving,error,load,save}=useSettings('/api/settings',value=>({...value,announcement:{enabled:false,content:'',displayType:'banner',...value.announcement}}));
const picker=ref(false);
async function submit(){if(!model.value.appName?.trim()){toast('请输入站点名称');return;}const {appName,appLogo,siteUrl,announcement}=model.value;const saved=await save({appName,appLogo,siteUrl,announcement});if(saved)updateSite(saved);}
</script>
<template><SettingsPage title="站点信息与公告" description="设置品牌标识、站点地址和访客公告。" :busy="busy" :error="error" @retry="load"><form class="settings-panel form-stack" novalidate @submit.prevent="submit"><div class="settings-form-grid"><FormField v-model="model.appName" label="站点名称" required /><FormField v-model="model.siteUrl" label="站点地址" placeholder="https://img.example.com" /><FormField v-model="model.appLogo" label="网站 Logo URL" /><div class="logo-choice"><img v-if="model.appLogo" :src="model.appLogo" alt="Logo 预览"><UiButton icon="images" @click="picker=true">从图库选择 Logo</UiButton></div></div><div class="form-divider"></div><FormField v-model="model.announcement.enabled" type="checkbox" label="显示站点公告" /><FormField v-model="model.announcement.content" type="textarea" label="公告内容" help="支持安全 HTML：段落、标题、加粗、链接、列表等；脚本、图片和自定义样式会被过滤。" /><FormField v-model="model.announcement.displayType" type="select" label="公告形式" :options="[{label:'页面横幅',value:'banner'},{label:'弹窗',value:'modal'}]" /><UiButton type="submit" variant="primary" :loading="saving">保存站点信息</UiButton></form><ImagePicker v-model:visible="picker" @select="model.appLogo=$event.url" /></SettingsPage></template>
