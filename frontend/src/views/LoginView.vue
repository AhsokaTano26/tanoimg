<script setup>
import {ref,onMounted,onBeforeUnmount} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import Icon from '../components/Icon.vue';
import FormField from '../components/FormField.vue';
import {request,verifySession} from '../runtime.js';
import {loginDestination} from '../routes.js';
import {loginWithPasskey,passkeySupported,passkeyError,cancelPasskey} from '../passkey.js';
const route=useRoute(),router=useRouter(),username=ref(''),password=ref(''),busy=ref(false),error=ref('');
const challenge=ref(''),code=ref(''),recovery=ref(false),passkeyAvailable=ref(false);
const supported=passkeySupported();
const post=body=>({method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
onMounted(async()=>{try{passkeyAvailable.value=(await request('/api/auth/methods')).passkeyAvailable;}catch{}});
onBeforeUnmount(()=>{cancelPasskey();password.value='';code.value='';challenge.value='';});
async function complete(){await verifySession();await router.replace(loginDestination(route.query.redirect));}
async function login(){
 if(busy.value)return;
 if(challenge.value&&!code.value.trim()){error.value='请输入动态码或恢复码';return;}
 if(!challenge.value&&(!username.value.trim()||!password.value)){error.value='请填写用户名和密码';return;}
 busy.value=true;error.value='';
 try{
  if(challenge.value){await request('/api/auth/totp',post({challenge:challenge.value,code:code.value.trim()}));code.value='';await complete();}
  else {const data=await request('/api/auth/login',post({username:username.value.trim(),password:password.value}));password.value='';if(data.requiresTOTP){challenge.value=data.challenge;}else await complete();}
 }catch(reason){error.value=reason.message;}finally{busy.value=false;}
}
async function passkey(){busy.value=true;error.value='';try{await loginWithPasskey();await complete();}catch(reason){error.value=passkeyError(reason);}finally{busy.value=false;}}
function restart(){challenge.value='';code.value='';recovery.value=false;error.value='';}
</script>
<template><section class="login-page"><form class="login-card form-stack" novalidate @submit.prevent="login"><span class="dialog-symbol"><Icon :name="challenge?'shield-check':'aperture'" /></span><div><span class="eyebrow">{{ challenge?'TWO-STEP VERIFICATION':'ADMIN ACCESS' }}</span><h1>{{ challenge?'二次验证':'登录后台' }}</h1><p>{{ challenge?'输入验证器中的动态码，或使用一次性恢复码。':'管理你的图片、上传权限与站点设置。' }}</p></div><template v-if="!challenge"><FormField v-model="username" label="用户名" autocomplete="username" :disabled="busy" /><FormField v-model="password" label="密码" type="password" autocomplete="current-password" :disabled="busy" /></template><template v-else><div class="form-field"><label for="login-otp">{{ recovery?'恢复码':'6 位动态码' }}</label><UiInput id="login-otp" v-model="code" :disabled="busy" :inputmode="recovery?'text':'numeric'" autocomplete="one-time-code" :maxlength="recovery?64:6" :placeholder="recovery?'xxxxxxxx-xxxxxxxx-xxxxxxxx':'000000'" /></div><UiButton variant="ghost" :disabled="busy" @click="recovery=!recovery;code=''">{{ recovery?'使用动态码':'使用恢复码' }}</UiButton></template><p v-if="error" class="form-error" role="alert">{{ error }}</p><UiButton type="submit" variant="primary" icon="arrow-right" :loading="busy">{{ challenge?'验证并登录':'密码登录' }}</UiButton><UiButton v-if="challenge" :disabled="busy" @click="restart">重新输入账号密码</UiButton><div v-if="passkeyAvailable" class="passkey-login"><span class="auth-divider">或</span><UiButton icon="key-round" :disabled="!supported || busy" @click="passkey">使用 Passkey 登录</UiButton><small>{{ supported?'使用指纹、面容、设备 PIN 或安全密钥。':'当前浏览器或连接不支持 Passkey，请使用 HTTPS 和支持的浏览器。' }}</small></div><RouterLink class="login-back" to="/">返回图片页面</RouterLink></form></section></template>
