<script setup>
import {computed,ref,onMounted,onBeforeUnmount} from 'vue';
import FormField from './FormField.vue';
import {request,toast} from '../runtime.js';
import {registerPasskey,passkeySupported,passkeyError,cancelPasskey} from '../passkey.js';
const emit=defineEmits(['status']);
const status=ref(null),loading=ref(true),loadError=ref(''),busy=ref(false),error=ref('');
const dialog=ref(false),mode=ref(''),target=ref(null),password=ref(''),code=ref(''),name=ref('');
const setup=ref(null),setupOpen=ref(false),setupCode=ref('');
const recoveryOpen=ref(false),recoveryCodes=ref([]),saved=ref(false);
const supported=passkeySupported();
const titles={setup:'绑定 TOTP 验证器',disable:'关闭 TOTP 二次验证',recovery:'重新生成恢复码',register:'添加 Passkey',delete:'删除 Passkey'};
const dialogTitle=computed(()=>titles[mode.value]||'身份确认');
const post=body=>({method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
async function load(){loading.value=true;loadError.value='';try{status.value=await request('/api/admin/security');emit('status',status.value);}catch(reason){loadError.value=reason.message;}finally{loading.value=false;}}
function open(action,key){mode.value=action;target.value=key;password.value='';code.value='';name.value='';error.value='';dialog.value=true;}
function closeProof(){password.value='';code.value='';}
function showRecovery(codes){recoveryCodes.value=codes;saved.value=false;recoveryOpen.value=true;}
async function submit(){
 if(busy.value)return;if(!password.value){error.value='请填写当前密码';return;}
 if(status.value.totpEnabled&&!code.value.trim()){error.value='请输入动态码或恢复码';return;}
 if(mode.value==='register'&&!name.value.trim()){error.value='请给 Passkey 起一个名称';return;}
 busy.value=true;error.value='';const proof={password:password.value,code:code.value.trim(),name:name.value.trim()};
 try{
  if(mode.value==='setup'){setup.value=await request('/api/admin/totp/setup',post(proof));setupCode.value='';setupOpen.value=true;}
  if(mode.value==='disable'){await request('/api/admin/totp',{...post(proof),method:'DELETE'});toast('TOTP 已关闭');}
  if(mode.value==='recovery'){showRecovery((await request('/api/admin/recovery-codes',post(proof))).recoveryCodes);}
  if(mode.value==='register'){await registerPasskey(proof);toast('Passkey 已添加');}
  if(mode.value==='delete'){await request('/api/admin/passkeys/'+encodeURIComponent(target.value.id),{...post(proof),method:'DELETE'});toast('Passkey 已删除');}
  dialog.value=false;closeProof();await load();
 }catch(reason){error.value=passkeyError(reason);code.value='';}finally{busy.value=false;}
}
async function enable(){busy.value=true;error.value='';try{const data=await request('/api/admin/totp/enable',post({challenge:setup.value.challenge,code:setupCode.value.trim()}));setupOpen.value=false;setup.value=null;setupCode.value='';showRecovery(data.recoveryCodes);await load();toast('TOTP 二次验证已启用');}catch(reason){error.value=reason.message;}finally{busy.value=false;}}
async function copy(value){try{await navigator.clipboard.writeText(value);toast('已复制，请妥善保存');}catch{toast('无法复制，请手动选取内容');}}
function download(){const url=URL.createObjectURL(new Blob(['TanoImg 一次性恢复码\n每个恢复码只能使用一次，请离线妥善保存。\n\n'+recoveryCodes.value.join('\n')],{type:'text/plain;charset=utf-8'}));const a=document.createElement('a');a.href=url;a.download='tanoimg-recovery-codes.txt';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000);}
onMounted(load);onBeforeUnmount(()=>{cancelPasskey();closeProof();setup.value=null;recoveryCodes.value=[];});
</script>
<template>
 <section v-if="loading&&!status" class="settings-panel" role="status">正在加载登录保护…</section>
 <section v-else-if="loadError" class="settings-panel"><p role="alert">{{ loadError }}</p><UiButton @click="load">重新加载</UiButton></section>
 <template v-else-if="status">
  <section class="settings-panel form-stack"><div class="security-heading"><div><h2>TOTP 二次验证</h2><p>密码验证后，再输入验证器每 30 秒更新的 6 位动态码。</p></div><span class="status-chip" :class="status.totpEnabled?'success':'muted'">{{ status.totpEnabled?'已启用':'未启用' }}</span></div><template v-if="status.totpEnabled"><p>剩余 {{ status.recoveryRemaining }} 个一次性恢复码。无法使用验证器时，可在密码登录的第二步使用恢复码。</p><div class="inline-actions"><UiButton :disabled="busy" @click="open('recovery')">重新生成恢复码</UiButton><UiButton variant="danger" :disabled="busy" @click="open('disable')">关闭二次验证</UiButton></div></template><UiButton v-else icon="shield-check" variant="primary" :disabled="busy" @click="open('setup')">绑定验证器</UiButton></section>
  <section class="settings-panel form-stack"><div class="security-heading"><div><h2>Passkey</h2><p>使用指纹、面容或设备 PIN 直接登录，无需输入密码和 TOTP。</p></div><span class="status-chip">{{ status.passkeys.length }} 个凭证</span></div><p v-if="!status.passkeyAvailable" class="security-note">尚未配置 Passkey。请在部署环境设置 TANOIMG_PUBLIC_URL 为本站 HTTPS 地址并重启服务。</p><p v-else-if="!supported" class="security-note">此浏览器或连接不支持 Passkey。请使用支持的浏览器访问 {{ status.publicURL }}。</p><p v-else-if="status.publicURL" class="field-help">凭证绑定站点：{{ status.publicURL }}。每台设备可单独添加，最多 20 个。</p><div v-if="status.passkeys.length" class="key-list"><article v-for="key in status.passkeys" :key="key.id" class="key-row"><div><strong>{{ key.name }}</strong><p>添加于 {{ key.createdAt }}<br>最近使用：{{ key.lastUsedAt||'尚未使用' }}</p></div><UiButton variant="danger" :disabled="busy" @click="open('delete',key)">删除</UiButton></article></div><UiButton icon="key-round" variant="primary" :disabled="!status.passkeyAvailable||!supported||busy||status.passkeys.length>=20" @click="open('register')">添加 Passkey</UiButton></section>
 </template>
 <UiDialog v-model:visible="dialog" :title="dialogTitle" :closable="!busy" :closeOnEscape="!busy" @hide="closeProof"><form class="form-stack" novalidate @submit.prevent="submit"><p class="dialog-message">为保护账户，请确认当前密码{{ status?.totpEnabled?'和动态码（也可使用一次性恢复码）':'' }}。成功变更后，其他设备需要重新登录。</p><p v-if="mode==='disable'" class="security-note">关闭后，密码登录将不再要求动态码，现有恢复码同时失效。</p><p v-if="mode==='recovery'" class="security-note">重新生成后，所有旧恢复码立即失效。</p><p v-if="mode==='delete'" class="security-note">删除「{{ target?.name }}」后，该凭证将无法登录。</p><FormField v-if="mode==='register'" v-model="name" label="Passkey 名称" placeholder="例如：我的 MacBook" :disabled="busy" /><FormField v-model="password" type="password" label="当前密码" autocomplete="current-password" :disabled="busy" /><FormField v-if="status?.totpEnabled" v-model="code" label="动态码或恢复码" autocomplete="one-time-code" :disabled="busy" /><p v-if="error" class="form-error" role="alert">{{ error }}</p><div class="inline-actions"><UiButton type="submit" variant="primary" :loading="busy">{{ mode==='register'?'验证并添加':'确认并继续' }}</UiButton><UiButton :disabled="busy" @click="dialog=false">取消</UiButton></div></form></UiDialog>
 <UiDialog v-model:visible="setupOpen" title="扫描二维码绑定验证器" :closable="!busy" :closeOnEscape="!busy" @hide="setup=null;setupCode='' "><form v-if="setup" class="form-stack" novalidate @submit.prevent="enable"><p class="dialog-message">使用验证器扫描二维码，然后输入动态码完成绑定。关闭此窗口不会启用二次验证。</p><img :src="setup.qr" width="256" height="256" alt="TOTP 绑定二维码" class="totp-qr"><div class="security-secret"><span>无法扫码？手动输入密钥</span><code>{{ setup.secret }}</code><UiButton @click="copy(setup.secret)">复制密钥</UiButton></div><div class="form-field"><label for="setup-otp">6 位动态码</label><UiInput id="setup-otp" v-model="setupCode" inputmode="numeric" autocomplete="one-time-code" maxlength="6" placeholder="000000" :disabled="busy" /></div><p v-if="error" class="form-error" role="alert">{{ error }}</p><UiButton type="submit" variant="primary" :loading="busy">验证并启用</UiButton></form></UiDialog>
 <UiDialog v-model:visible="recoveryOpen" title="保存一次性恢复码" :closable="false" :closeOnEscape="false"><div class="form-stack"><p class="dialog-message">恢复码只在此处显示一次。每个只能使用一次，请下载或保存到安全的位置，不要与他人分享。</p><div class="recovery-code-grid"><code v-for="item in recoveryCodes" :key="item">{{ item }}</code></div><div class="inline-actions"><UiButton @click="download">下载恢复码</UiButton><UiButton @click="copy(recoveryCodes.join('\n'))">复制全部</UiButton></div><UiCheckbox v-model="saved" label="我已妥善保存恢复码" /><UiButton variant="primary" :disabled="!saved" @click="recoveryOpen=false;recoveryCodes=[]">完成</UiButton></div></UiDialog>
</template>
