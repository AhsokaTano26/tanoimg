<script setup>
import {ref} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import Icon from '../components/Icon.vue';
import FormField from '../components/FormField.vue';
import {request,verifySession} from '../runtime.js';
import {loginDestination} from '../routes.js';
const route=useRoute(),router=useRouter(),username=ref(''),password=ref(''),busy=ref(false),error=ref('');
async function login(){if(busy.value)return;if(!username.value.trim()||!password.value){error.value='请填写用户名和密码';return;}busy.value=true;error.value='';try{await request('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({username:username.value,password:password.value})});password.value='';await verifySession();await router.replace(loginDestination(route.query.redirect));}catch(reason){error.value=reason.message;}finally{busy.value=false;}}
</script>
<template><section class="login-page"><form class="login-card form-stack" novalidate @submit.prevent="login"><span class="dialog-symbol"><Icon name="aperture" /></span><div><span class="eyebrow">ADMIN ACCESS</span><h1>登录后台</h1><p>管理你的图片、上传权限与站点设置。</p></div><FormField v-model="username" label="用户名" :disabled="busy" /><FormField v-model="password" label="密码" type="password" :disabled="busy" /><p v-if="error" class="form-error" role="alert">{{ error }}</p><UiButton type="submit" variant="primary" icon="arrow-right" :loading="busy">登录并进入后台</UiButton><RouterLink class="login-back" to="/">返回图片页面</RouterLink></form></section></template>
