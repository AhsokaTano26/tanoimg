<script setup>
import { computed, ref, watch, onMounted, onBeforeUnmount } from 'vue';
import { onBeforeRouteLeave } from 'vue-router';
import Icon from './Icon.vue';
import TurnstileWidget from './TurnstileWidget.vue';
import { request, toast } from '../runtime.js';
import { ask } from '../dialogs.js';
import { createUploadQueue } from '../upload-queue.mjs';
import { hashBlobSHA256 } from '../sha256-file.mjs';
const props=defineProps({ admin:Boolean, compact:Boolean });
const emit=defineEmits(['uploaded']);
const visibility=ref('unlisted'), tags=ref('');
const config=ref(null), urls=ref(''), urlMode=ref(false), dragging=ref(false), results=ref([]), failures=ref([]);
const progress=ref(null), loading=ref(true);
const activeStages=ref({}), reconciling=ref(false), reconcileError=ref('');
const receipts=ref([]), receiptDialog=ref(false), deleteID=ref(''), deleteReceipt=ref(''), deleting=ref(false), deleteError=ref('');
const turnstileConfig=ref(null),turnstileToken=ref(''),turnstileWidget=ref(null),verificationError=ref('');
watch(receiptDialog, visible => { if (!visible) receipts.value = []; });
const enabled=computed(()=>props.admin || (config.value?.enabled && turnstileConfig.value && !verificationError.value));
const canUpload=computed(()=>enabled.value && (props.admin || !turnstileConfig.value?.enabled || !!turnstileToken.value));
const busy=computed(()=>!!progress.value && (progress.value.active>0 || progress.value.pending>0));
const endpoint=()=>props.admin ? '/api/upload/private?visibility='+visibility.value : '/api/upload/public';
const uploadTags=()=>tags.value.split(',').map(tag=>tag.trim()).filter(Boolean);
const resumableThreshold=16*1048576;
const newUploadKey=()=>`up_${[...crypto.getRandomValues(new Uint8Array(16))].map(value=>value.toString(16).padStart(2,'0')).join('')}`;
function setStage(item,stage,done=0,total=item.file?.size||0){activeStages.value={...activeStages.value,[item.key]:{name:item.name,stage,done,total}};}
function clearStage(item){const next={...activeStages.value};delete next[item.key];activeStages.value=next;}
async function reconcileKeys(keys,signal){return (await request('/api/uploads/reconcile',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({keys}),signal})).results||[];}
const recoveredImage=result=>({id:result.imageId,url:result.url,__reconciled:true});
let disposed=false;
function pause(ms,signal){return new Promise((resolve,reject)=>{const abort=()=>{clearTimeout(timer);reject(new DOMException("上传已取消","AbortError"));};const timer=setTimeout(()=>{signal.removeEventListener("abort",abort);resolve();},ms);signal.addEventListener("abort",abort,{once:true});if(signal.aborted)abort();});}
async function uploadResumable(item,signal) {
  const base='/api/upload/resumable';
  if(!item.hash){setStage(item,'计算 SHA-256');item.hash=await hashBlobSHA256(item.file,{signal,onProgress:(done,total)=>setStage(item,'计算 SHA-256',done,total)});}
  let state;
  if(item.sessionId){try{state=await request(`${base}/${item.sessionId}`,{signal});}catch(error){if(error.status===404)item.sessionId='';throw error;}}
  else {
    setStage(item,'建立分块会话');
    state=await request(base,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({filename:item.file.name,size:item.file.size,sha256:item.hash,visibility:item.visibility,tags:item.tags}),signal});
    item.sessionId=state.id;
  }
  if(state.image)return state.image;
  const url=`${base}/${item.sessionId}`;
  let failures=0;
  while(state.offset<item.file.size) {
    if(signal.aborted)throw new DOMException('上传已取消','AbortError');
    const start=state.offset,end=Math.min(start+Math.min(state.chunkSize,8*1048576),item.file.size);
    setStage(item,'分块上传',start,item.file.size);
    try {
      state=await request(`${url}/chunk?offset=${start}`,{method:'PUT',headers:{'Content-Type':'application/octet-stream'},body:item.file.slice(start,end),signal});
      failures=0;setStage(item,'分块上传',state.offset,item.file.size);
    } catch(error) {
      if(signal.aborted)throw error;
      try {state=await request(url,{signal});} catch {throw error;}
      if(state.image)return state.image;
      if(state.offset>start){failures=0;continue;}
      if(++failures>=3 || ![undefined,408,409,429,500,502,503,504].includes(error.status))throw error;
      await pause(Math.max(error.retryAfterMs||0,400*2**(failures-1)),signal);
    }
  }
  setStage(item,'校验并保存',item.file.size,item.file.size);
  for(let attempt=0;attempt<3;attempt++){
    try {state=await request(`${url}/finalize`,{method:'POST',signal});return state.image;}
    catch(error){if(signal.aborted)throw error;try{state=await request(url,{signal});if(state.image)return state.image;}catch{}if(attempt===2 || ![undefined,408,429,500,502,503,504].includes(error.status))throw error;await pause(Math.max(error.retryAfterMs||0,500*2**attempt),signal);}
  }
}
async function upload(item,signal) {
  if(item.resumable)return uploadResumable(item,signal);
  const challengeHeaders={...(item.challengeToken?{'X-Turnstile-Token':item.challengeToken}:{}),'Idempotency-Key':item.key};
  for(let attempt=0;attempt<3;attempt++) {
    const body=item.remote?JSON.stringify({url:item.url,tags:item.tags}):new FormData();
    if(!item.remote){body.append('file',item.file);body.append('tags',item.tags.join(','));}
    const url=item.remote?(props.admin?'/api/upload/url?visibility='+item.visibility:'/api/upload/public/url'):item.endpoint;
    try {return await request(url,{method:'POST',headers:{...challengeHeaders,...(item.remote?{'Content-Type':'application/json'}:{})},body,signal});}
    catch(error){
      if(signal.aborted)throw error;
      if([undefined,408,409,429,500,502,503,504].includes(error.status)){
        try{const [state]=await reconcileKeys([item.key],signal);if(state?.status==='complete')return recoveredImage(state);if(state?.status==='pending')throw Object.assign(new Error('服务器仍在处理此项，请稍后核对上传结果'),{pending:true});}
        catch(checkError){if(checkError.pending)throw checkError;if(attempt===2)throw error;}
      }
      if(item.challengeToken || ![undefined,408,429,500,502,503,504].includes(error.status) || attempt===2)throw error;
      await pause(Math.max(error.retryAfterMs||0,300*2**attempt),signal);
    }
  }
}
const queue=createUploadQueue({
  concurrency:props.admin ? 4 : 1,upload,
  onProgress:value=>{if(!disposed) progress.value=value;},
  onResult:(item,image,error)=>{if(disposed)return;clearStage(item);const {receipt,__reconciled,...displayImage}=image || {};if(!props.admin && receipt) receipts.value.push({id:image.id,name:item.name,receipt});const row={key:item.key,name:item.name,image:image?displayImage:null,recovered:!!__reconciled || (!!image && !props.admin && !receipt),error:error?.message};const existing=results.value.find(result=>result.key===item.key);if(existing)Object.assign(existing,row);else results.value.unshift(row);results.value=results.value.slice(0,24);if(error){const {challengeToken,...retryItem}=item;failures.value.push(retryItem);}},
  onIdle:()=>{if(disposed)return;activeStages.value={};emit('uploaded');if(receipts.value.length) receiptDialog.value=true;turnstileWidget.value?.reset();toast('本批次上传结束');},
});
function enqueue(items,preserve=false) {
  if(!enabled.value) {toast('公共上传暂未开放');return;}
  if(!canUpload.value) {toast('请先完成访客验证');return;}
  if(!busy.value && !preserve && !failures.value.length)results.value=[];
  const challengeToken=!props.admin && turnstileConfig.value?.enabled ? turnstileToken.value : '';
  if(queue.add(items.map(item=>({...item,key:item.key||newUploadKey(),challengeToken}))) && challengeToken) turnstileToken.value='';
}
function filesSelected(files) {
  if(!props.admin && turnstileConfig.value?.enabled && files.length>1){toast('每次验证只能上传一张图片');return;}
  enqueue([...files].map(file=>({file,name:file.name,endpoint:endpoint(),visibility:visibility.value,tags:uploadTags(),resumable:props.admin && file.size>=resumableThreshold})));
}
function uploadURLs() {
  const values=[...new Set(urls.value.split(/\r?\n/).map(value=>value.trim()).filter(Boolean))];
  if(!values.length) {toast('请输入图片 URL');return;}
  if(values.some(value=>!/^https?:\/\//i.test(value))) {toast('图片 URL 必须以 http:// 或 https:// 开头');return;}
  if(!props.admin && turnstileConfig.value?.enabled && values.length>1){toast('每次验证只能导入一个 URL');return;}
  enqueue(values.map(url=>({remote:true,url,name:url,visibility:visibility.value,tags:uploadTags()})));urls.value='';
}
function pasted(event) {
  if(!enabled.value || document.querySelector('.p-dialog'))return;
  const files=[...(event.clipboardData?.files||[])];
  if(files.length){event.preventDefault();filesSelected(files);}
  else if(!['INPUT','TEXTAREA'].includes(event.target.tagName)) {
    const text=event.clipboardData?.getData('text/plain')?.trim();
    if(/^https?:\/\//i.test(text)){urls.value=text;urlMode.value=true;}
  }
}
async function copy(url){try{await navigator.clipboard.writeText(new URL(url,location.origin).href);toast('直链已复制');}catch{toast('复制失败');}}
async function copyText(value,label){try{await navigator.clipboard.writeText(value);toast(label+'已复制');}catch{toast('复制失败，请手动复制');}}
async function selfDelete() {
  const id=deleteID.value.trim(), receipt=deleteReceipt.value.trim();
  if (!id || !receipt) { deleteError.value='请输入图片 ID 和上传凭据'; return; }
  if (!await ask('删除后，图片将无法通过公开链接访问。确定删除吗？',{title:'删除自己上传的图片',accept:'删除图片',danger:true})) return;
  deleting.value=true;deleteError.value='';
  try {
    await request(`/api/images/${encodeURIComponent(id)}/self`,{method:'DELETE',headers:{'X-Upload-Receipt':receipt}});
    deleteID.value='';deleteReceipt.value='';results.value=results.value.filter(result=>result.image?.id!==id);
    emit('uploaded');toast('图片已删除');
  } catch(error) { deleteError.value=error.message; }
  finally { deleting.value=false; }
}
async function reconcileFailed(retryUnknown=false){
  if(!failures.value.length || reconciling.value)return;
  reconciling.value=true;reconcileError.value='';
  try{
    const regular=failures.value.filter(item=>!item.resumable);
    const states=regular.length?await reconcileKeys(regular.map(item=>item.key)):[];
    const byKey=new Map(states.map(state=>[state.key,state]));
    const retryItems=[],remaining=[];
    for(const item of failures.value){
      if(item.resumable){
        let state;
        try{state=item.sessionId?await request(`/api/upload/resumable/${item.sessionId}`):null;}
        catch(error){if(error.status!==404)throw error;item.sessionId='';}
        const result=results.value.find(result=>result.key===item.key);
        if(state?.image){if(result){result.image=state.image;result.error='';}continue;}
        if(retryUnknown)retryItems.push(item);
        else{remaining.push(item);if(result)result.error=state?`已上传 ${Math.round(state.offset/state.size*100)}%，可继续上传`:'会话已过期，可重新上传';}
        continue;
      }
      const state=byKey.get(item.key);
      if(state?.status==='complete'){
        const result=results.value.find(result=>result.key===item.key);
        if(result){result.image=recoveredImage(state);result.error='';result.recovered=true;}
      }else if(state?.status==='unknown' && retryUnknown)retryItems.push(item);
      else{
        remaining.push(item);
        const result=results.value.find(result=>result.key===item.key);
        if(result && state?.status==='pending')result.error='服务器仍在处理，请稍后再次核对';
        if(result && state?.status==='removed')result.error='原上传记录已删除，无法使用此键重试';
      }
    }
    failures.value=remaining;
    if(retryItems.length){if(!canUpload.value){failures.value.push(...retryItems);toast('请先完成访客验证');}else enqueue(retryItems,true);}
    else if(!remaining.length)toast('上传结果已核对');
  }catch(error){reconcileError.value=error.message;}
  finally{reconciling.value=false;}
}
function retry(){reconcileFailed(true);}
async function loadPublicConfig(){loading.value=true;verificationError.value='';try{const [upload,challenge]=await Promise.all([request('/api/config/public'),props.admin?Promise.resolve({enabled:false}):request('/api/turnstile')]);config.value=upload;turnstileConfig.value=challenge;if(challenge.enabled&&!challenge.siteKey)verificationError.value='访客验证尚未配置完整';}catch(error){verificationError.value=error.message;toast(error.message);}finally{loading.value=false;}}
onMounted(()=>{document.addEventListener('paste',pasted);loadPublicConfig();});
onBeforeRouteLeave(async()=>!busy.value || await ask('图片仍在上传，离开将取消剩余上传。',{title:'离开上传页面',accept:'取消上传并离开'}));
onBeforeUnmount(()=>{disposed=true;document.removeEventListener('paste',pasted);queue.cancel();failures.value=[];});
</script>
<template>
  <section :class="['upload-panel',{'upload-panel--compact':compact,dragging}]" @dragover.prevent="dragging=true" @dragleave.prevent="dragging=false" @drop.prevent="dragging=false;filesSelected($event.dataTransfer.files)">
    <div class="upload-panel-heading"><div class="upload-panel-title"><span class="upload-panel-symbol"><Icon name="image-up" /></span><div><h2>{{ admin ? '上传到图库' : '分享一张图片' }}</h2><p>{{ loading ? '正在加载上传配置…' : enabled ? '拖入图片、点击选择，或直接粘贴图片与链接。' : '公共上传暂未开放' }}</p></div></div><div class="upload-panel-actions"><UiFile label="选择图片" :multiple="admin || !turnstileConfig?.enabled" :disabled="!canUpload || loading" @select="filesSelected" /><UiButton icon="link" :disabled="!enabled || loading" @click="urlMode=!urlMode">{{ urlMode?'收起链接':'URL 上传' }}</UiButton></div></div>
    <div v-if="!admin && turnstileConfig?.enabled" class="upload-visibility"><span>访客验证</span><TurnstileWidget v-if="turnstileConfig.siteKey" ref="turnstileWidget" :site-key="turnstileConfig.siteKey" @update:token="turnstileToken=$event" /><small>{{ turnstileToken?'验证已完成，可上传一张图片。':'完成验证后即可上传。' }}</small></div><p v-if="verificationError" role="alert" class="form-error">{{ verificationError }} <UiButton @click="loadPublicConfig">重试</UiButton></p>
    <div v-if="admin" class="upload-visibility"><span>本次上传</span><UiSelect v-model="visibility" :disabled="busy" aria-label="上传可见性" :options="[{label:'公开 · 展示在公共图库',value:'public'},{label:'不公开列出 · 凭链接访问',value:'unlisted'},{label:'仅自己可见 · 需要登录',value:'private'}]" /><small>旧版私人图片对应“不公开列出”，原有直链可继续访问。</small></div>
    <div class="upload-visibility"><span>图片标签</span><UiInput v-model="tags" :disabled="busy" aria-label="上传图片标签" placeholder="例如：旅行, 日落（逗号分隔）" /><small>本次上传的所有图片将使用相同标签。</small></div>
    <form v-if="urlMode" class="url-import-form" novalidate @submit.prevent="uploadURLs"><UiInput v-model="urls" type="textarea" :rows="2" aria-label="图片 URL" placeholder="https://example.com/photo.jpg（每行一个地址）" /><UiButton type="submit" variant="primary" :disabled="!canUpload">导入链接</UiButton></form>
    <p v-if="!admin && config?.enabled" class="upload-rules">{{ config.allowedFormats?.join(' / ').toUpperCase() }} · 最大 {{ Math.round(config.maxFileSize/1048576) }} MB</p>
    <div v-if="progress" class="upload-progress"><div class="upload-progress-top"><strong>{{ busy?'正在上传':progress.stopped?'已取消':'上传完成' }} {{ progress.completed }} / {{ progress.total }}</strong><span>{{ progress.completed-progress.failed-progress.cancelled }} 成功 · {{ progress.failed }} 失败</span><div class="inline-actions"><UiButton v-if="busy" @click="queue.cancel()">取消剩余上传</UiButton><UiButton v-if="failures.length && !busy" :loading="reconciling" @click="reconcileFailed()">核对上传结果</UiButton><UiButton v-if="failures.length && !busy" :loading="reconciling" @click="retry">核对并重试</UiButton></div></div><UiProgress :value="progress.total ? progress.completed/progress.total*100 : 0" /><p v-if="reconcileError" role="alert" class="form-error">{{ reconcileError }}</p><div v-for="(item,key) in activeStages" :key="key" class="upload-stage"><span>{{ item.name }} · {{ item.stage }}</span><span v-if="item.total">{{ Math.round(item.done/item.total*100) }}%</span></div></div>
    <div v-if="results.length" class="upload-results"><div v-for="(result,index) in results" :key="index" :class="['upload-result',{error:result.error}]"><img v-if="result.image?.url" :src="result.image.url" alt=""><Icon v-else name="triangle-alert" /><div><strong>{{ result.name }}</strong><small>{{ result.error || result.image?.url }}</small><small v-if="result.recovered && !admin" class="form-error">已核对成功，但原响应丢失，无法恢复一次性自删凭据。</small></div><UiButton v-if="result.image?.url" @click="copy(result.image.url)">复制直链</UiButton></div></div>
    <section v-if="!admin" class="upload-self-delete"><h3>删除自己上传的图片</h3><p>凭上传时保存的图片 ID 和凭据即可删除，无需登录。凭据只用于这张图片。</p><form class="form-stack" @submit.prevent="selfDelete"><label class="form-field"><span>图片 ID</span><UiInput v-model="deleteID" aria-label="待删除图片 ID" autocomplete="off" /></label><label class="form-field"><span>上传凭据</span><UiInput v-model="deleteReceipt" type="password" aria-label="上传凭据" autocomplete="off" /></label><p v-if="deleteError" role="alert" class="form-error">{{ deleteError }}</p><UiButton type="submit" variant="danger" :loading="deleting">删除图片</UiButton></form></section>
  </section>
  <UiDialog v-if="!admin" v-model:visible="receiptDialog" title="保存上传凭据" width="640px"><div class="form-stack"><p>这些凭据只显示这一次。请同时保存每张图片的 ID 和凭据；关闭后无法找回。之后可在此页面使用它们删除自己上传的图片。凭据不会写入浏览器本地存储。</p><div v-for="item in receipts" :key="item.id" class="receipt-entry"><strong>{{ item.name }}</strong><label class="form-field"><span>图片 ID</span><UiInput :model-value="item.id" readonly aria-label="已上传图片 ID" /></label><label class="form-field"><span>上传凭据</span><UiInput :model-value="item.receipt" readonly aria-label="一次性上传凭据" /></label><div class="inline-actions"><UiButton @click="copyText(item.id,'图片 ID')">复制 ID</UiButton><UiButton variant="primary" @click="copyText(item.receipt,'上传凭据')">复制凭据</UiButton></div></div><UiButton @click="receiptDialog=false">我已保存</UiButton></div></UiDialog>
</template>
<style scoped>
.upload-self-delete {margin-top:28px;padding-top:24px;border-top:1px solid var(--border)}.upload-self-delete h3 {font-size:15px;margin:0 0 8px}.upload-self-delete .form-stack {max-width:520px;margin-top:18px;gap:14px}.receipt-entry {display:grid;gap:12px;padding:16px;border:1px solid var(--border);border-radius:8px;min-width:0}.receipt-entry strong {overflow-wrap:anywhere}.receipt-entry .inline-actions {flex-wrap:wrap}
.upload-stage{display:flex;justify-content:space-between;gap:12px;margin-top:8px;font-size:12px;color:var(--secondary)}.upload-stage span:first-child{overflow-wrap:anywhere}
</style>
