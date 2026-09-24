<script setup>
import {ref} from 'vue';
import {useRouter} from 'vue-router';
import SettingsPage from '../../components/SettingsPage.vue';
import FormField from '../../components/FormField.vue';
import SecuritySettings from '../../components/SecuritySettings.vue';
import {session,request,toast} from '../../runtime.js';
import {jsonOptions} from '../../composables/useSettings.js';
const totpEnabled=ref(false),code=ref('');
const username=ref(session.username),oldPassword=ref(''),newPassword=ref(''),busy=ref(false);
const router=useRouter();
async function rename(){if(username.value.trim().length<3){toast('用户名至少 3 个字符');return;}busy.value=true;try{await request('/api/admin/username',{...jsonOptions({username:username.value.trim()}),method:'PUT'});session.username=username.value.trim();toast('用户名已更新');}catch(error){toast(error.message);}finally{busy.value=false;}}
async function password(){if(!oldPassword.value||newPassword.value.length<8){toast('请填写当前密码，新密码至少 8 个字符');return;}busy.value=true;try{await request('/api/admin/password',{...jsonOptions({oldPassword:oldPassword.value,newPassword:newPassword.value,code:code.value}),method:'PUT'});session.admin=false;session.username='';oldPassword.value='';newPassword.value='';code.value='';await router.replace('/login');toast('密码已更新，请重新登录');}catch(error){toast(error.message);}finally{busy.value=false;}}
</script>
<template><SettingsPage class="account-security" title="账户安全" description="管理密码、TOTP 二次验证与 Passkey。密码修改后，所有设备需要重新登录。"><SecuritySettings @status="totpEnabled=$event.totpEnabled" /><form class="settings-panel form-stack" novalidate @submit.prevent="rename"><FormField v-model="username" label="管理员用户名" /><UiButton type="submit" :loading="busy">修改用户名</UiButton></form><form class="settings-panel form-stack" novalidate @submit.prevent="password"><div class="settings-form-grid"><FormField v-model="oldPassword" type="password" label="当前密码" /><FormField v-model="newPassword" type="password" label="新密码" /></div><FormField v-if="totpEnabled" v-model="code" label="动态码或恢复码" autocomplete="one-time-code" /><UiButton type="submit" variant="primary" :loading="busy">修改密码</UiButton></form></SettingsPage></template>
