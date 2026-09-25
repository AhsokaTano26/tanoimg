<script setup>
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import { request, toast } from '../runtime.js';
import { ask } from '../dialogs.js';
import TurnstileWidget from './TurnstileWidget.vue';
import { dragRectangle, editorOutputFormat, rotateRectangleClockwise, rotatedSize } from '../image-editor.mjs';

const props=defineProps({image:Object,admin:Boolean});
const visible=defineModel('visible',{type:Boolean});
const emit=defineEmits(['saved']);
const dialogVisible=computed({get:()=>visible.value,set:value=>{if(!value&&!saving.value)visible.value=false;}});
const canvas=ref(null), loaded=ref(false), loading=ref(false), error=ref(''), saving=ref(false);
const originalSize=ref({width:0,height:0}), rotation=ref(0), crop=ref(null), redactions=ref([]), draft=ref(null), mode=ref('crop');
const visibility=ref('unlisted'), saved=ref(null), guestConfig=ref(null), challenge=ref(null), challengeToken=ref(''), widget=ref(null);
const dimensions=computed(()=>rotatedSize(originalSize.value,rotation.value));
const format=computed(()=>editorOutputFormat(props.image?.format));
const canReplace=computed(()=>props.admin && format.value.replaceable && loaded.value);
const canSaveNew=computed(()=>loaded.value && (props.admin || guestConfig.value?.enabled && challenge.value && (!challenge.value.enabled || !!challengeToken.value)));
let sourceImage=null,loadSerial=0,frame=0,saveKey='';

function clear() {
  loadSerial++; if(frame)cancelAnimationFrame(frame);frame=0;sourceImage=null;loaded.value=false;loading.value=false;error.value='';saved.value=null;
  rotation.value=0;crop.value=null;redactions.value=[];draft.value=null;guestConfig.value=null;challenge.value=null;challengeToken.value='';saveKey='';
}
async function load() {
  clear(); if(!visible.value || !props.image)return;
  const serial=loadSerial;loading.value=true;visibility.value=props.image.visibility || 'unlisted';
  try {
    const url=new URL(props.image.url,location.origin);
    if(url.origin!==location.origin)throw new Error('源图片跨域，浏览器无法安全地编辑');
    const image=new Image();image.decoding='async';
    const loadedImage=new Promise((resolve,reject)=>{image.onload=resolve;image.onerror=()=>reject(new Error('无法读取原图'));});
    image.src=url.href;await loadedImage;
    if(serial!==loadSerial)return;
    if(!image.naturalWidth || !image.naturalHeight)throw new Error('图片没有可编辑的像素');
    if(image.naturalWidth*image.naturalHeight>32000000)throw new Error('原图超过 3200 万像素，请使用桌面图片工具编辑');
    sourceImage=image;originalSize.value={width:image.naturalWidth,height:image.naturalHeight};loaded.value=true;
    await nextTick();scheduleDraw();
    if(!props.admin){
      const [config,turnstile]=await Promise.all([request('/api/config/public'),request('/api/turnstile')]);
      if(serial!==loadSerial)return;
      guestConfig.value=config;challenge.value=turnstile;
    }
  } catch(cause){if(serial===loadSerial)error.value=cause.message;}
  finally{if(serial===loadSerial)loading.value=false;}
}
watch([visible,()=>props.image?.id],([shown])=>{if(shown)load();else clear();},{immediate:true});
watch(canvas,element=>{if(element && loaded.value)scheduleDraw();});
onBeforeUnmount(clear);

function drawOriented(ctx) {
  const {width,height}=originalSize.value;
  ctx.save();
  if(rotation.value===1){ctx.translate(height,0);ctx.rotate(Math.PI/2);}
  else if(rotation.value===2){ctx.translate(width,height);ctx.rotate(Math.PI);}
  else if(rotation.value===3){ctx.translate(0,width);ctx.rotate(3*Math.PI/2);}
  ctx.drawImage(sourceImage,0,0,width,height);
  ctx.restore();
}
function draw() {
  if(!loaded.value || !canvas.value || !sourceImage)return;
  const {width,height}=dimensions.value;
  const scale=Math.min(1,1200/width,850/height);
  const element=canvas.value;
  element.width=Math.max(1,Math.round(width*scale));element.height=Math.max(1,Math.round(height*scale));
  const ctx=element.getContext('2d');if(!ctx)return;
  ctx.scale(scale,scale);drawOriented(ctx);
  ctx.fillStyle='#111';for(const rect of redactions.value)ctx.fillRect(rect.x,rect.y,rect.width,rect.height);
  if(crop.value){
    const rect=crop.value;ctx.fillStyle='rgba(0,0,0,.55)';
    ctx.fillRect(0,0,width,rect.y);ctx.fillRect(0,rect.y,rect.x,rect.height);
    ctx.fillRect(rect.x+rect.width,rect.y,width-rect.x-rect.width,rect.height);
    ctx.fillRect(0,rect.y+rect.height,width,height-rect.y-rect.height);
    ctx.strokeStyle='#76d9ff';ctx.lineWidth=Math.max(2,2/scale);ctx.strokeRect(rect.x,rect.y,rect.width,rect.height);
  }
  if(draft.value){const rect=dragRectangle(draft.value.start,draft.value.end,dimensions.value);ctx.fillStyle=mode.value==='redact'?'rgba(0,0,0,.7)':'rgba(118,217,255,.18)';ctx.fillRect(rect.x,rect.y,rect.width,rect.height);ctx.strokeStyle=mode.value==='redact'?'#fff':'#76d9ff';ctx.lineWidth=Math.max(2,2/scale);ctx.strokeRect(rect.x,rect.y,rect.width,rect.height);}
}
function scheduleDraw(){if(!frame)frame=requestAnimationFrame(()=>{frame=0;draw();});}
function point(event){const box=canvas.value.getBoundingClientRect(),size=dimensions.value;return{x:(event.clientX-box.left)/box.width*size.width,y:(event.clientY-box.top)/box.height*size.height};}
function pointerDown(event){if(!loaded.value || saving.value)return;event.currentTarget.setPointerCapture?.(event.pointerId);const start=point(event);draft.value={start,end:start};scheduleDraw();}
function pointerMove(event){if(!draft.value)return;draft.value={...draft.value,end:point(event)};scheduleDraw();}
function pointerUp(event){if(!draft.value)return;const rect=dragRectangle(draft.value.start,point(event),dimensions.value);draft.value=null;if(rect.width>=4 && rect.height>=4){if(mode.value==='crop')crop.value=rect;else redactions.value=[...redactions.value,rect];saveKey='';}scheduleDraw();}
function pointerCancel(){draft.value=null;scheduleDraw();}
function rotate(){const oldSize=dimensions.value;crop.value=crop.value?rotateRectangleClockwise(crop.value,oldSize):null;redactions.value=redactions.value.map(rect=>rotateRectangleClockwise(rect,oldSize));rotation.value=(rotation.value+1)%4;saveKey='';scheduleDraw();}
function reset(){rotation.value=0;crop.value=null;redactions.value=[];draft.value=null;saveKey='';scheduleDraw();}
function removeLastRedaction(){redactions.value=redactions.value.slice(0,-1);saveKey='';scheduleDraw();}
function clearCrop(){crop.value=null;saveKey='';scheduleDraw();}

async function outputFile(replace=false) {
  const {width,height}=dimensions.value;
  const area=crop.value || {x:0,y:0,width,height};
  if(area.width*area.height>16000000)throw new Error('导出区域超过 1600 万像素，请先裁剪后保存');
  const output=document.createElement('canvas');output.width=area.width;output.height=area.height;
  try{
    const ctx=output.getContext('2d');if(!ctx)throw new Error('浏览器无法创建图片画布');
    ctx.translate(-area.x,-area.y);drawOriented(ctx);ctx.fillStyle='#111';for(const rect of redactions.value)ctx.fillRect(rect.x,rect.y,rect.width,rect.height);
    if(typeof output.toBlob!=='function')throw new Error('当前浏览器不支持图片导出');
    const blob=await new Promise((resolve,reject)=>output.toBlob(value=>value?resolve(value):reject(new Error('无法导出图片')),format.value.mime,0.92));
    if(replace && blob.type!==format.value.mime)throw new Error('浏览器无法输出与原图相同的格式，请另存为新图片');
    const extension=blob.type==='image/jpeg'?'jpg':blob.type==='image/webp'?'webp':'png';
    const base=(props.image.originalName||'image').replace(/\.[^.]+$/,'').replace(/[\\/:*?"<>|]/g,'_').slice(0,100)||'image';
    return new File([blob],`${base}-edited.${extension}`,{type:blob.type});
  }finally{output.width=0;output.height=0;}
}
function uploadKey(){if(!saveKey)saveKey=`edit_${[...crypto.getRandomValues(new Uint8Array(16))].map(value=>value.toString(16).padStart(2,'0')).join('')}`;return saveKey;}
async function reconcileNew(key){const data=await request('/api/uploads/reconcile',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({keys:[key]})});return data.results?.[0];}
async function save(replace=false){
  if(saving.value || !loaded.value)return;
  if(replace && (!canReplace.value || !await ask(redactions.value.length?'用编辑后的图片替换原图？原链接会显示新内容，但历史版本可能保留未遮挡的原图。':'用编辑后的图片替换原图？原链接会显示新内容。',{title:'替换原图',accept:'替换图片'})))return;
  if(!replace && !canSaveNew.value){error.value='当前无法上传编辑后的图片';return;}
  saving.value=true;error.value='';
  try{
    const file=await outputFile(replace),body=new FormData();body.append('file',file);
    let image,recovered=false;
    if(replace){
      try{image=await request(`/api/admin/images/${encodeURIComponent(props.image.id)}/replace`,{method:'POST',body});}
      catch(cause){if([undefined,500,502,503,504].includes(cause.status)){try{const fresh=await request(`/api/images/${encodeURIComponent(props.image.id)}`);if(fresh.revision>props.image.revision){image=fresh;recovered=true;}}catch{}}if(!image)throw cause;}
    }else{
      body.append('tags',(props.image.tags||[]).join(','));body.append('alt',props.image.alt||'');body.append('author',props.image.author||'');body.append('license',props.image.license||'');
      const key=uploadKey(),headers={'Idempotency-Key':key};
      if(!props.admin && challenge.value?.enabled)headers['X-Turnstile-Token']=challengeToken.value;
      const url=props.admin?`/api/upload/private?visibility=${encodeURIComponent(visibility.value)}`:'/api/upload/public';
      try{image=await request(url,{method:'POST',headers,body});}
      catch(cause){if([undefined,408,409,500,502,503,504].includes(cause.status)){try{const state=await reconcileNew(key);if(state?.status==='complete'){image={id:state.imageId,url:state.url};recovered=true;}else if(state?.status==='pending')throw new Error('服务器仍在处理，请稍后重试同一次保存');}catch(checkError){if(checkError.message.startsWith('服务器仍在处理'))throw checkError;}}if(!image)throw cause;}
      finally{if(!props.admin && challenge.value?.enabled){challengeToken.value='';widget.value?.reset();}}
    }
    const {receipt,...safeImage}=image;
    saved.value={image:safeImage,receipt:props.admin?'':receipt||'',recovered};
    emit('saved',safeImage);toast(replace?'原图片已替换':'已保存为新图片');
  }catch(cause){error.value=cause.message;}
  finally{saving.value=false;}
}
async function copy(value){try{await navigator.clipboard.writeText(value);toast('已复制');}catch{toast('复制失败，请手动复制');}}
const savedUrl=computed(()=>saved.value?.image.url?new URL(saved.value.image.url,location.origin).href:'');
</script>

<template>
  <UiDialog v-model:visible="dialogVisible" title="图片编辑" width="1120px">
    <div v-if="saved" class="editor-saved"><h3>{{ saved.image.id===image?.id?'原图片已替换':'新图片已保存' }}</h3><p v-if="saved.recovered && !admin" class="form-error">上传已完成，但原响应丢失，无法恢复一次性自删凭据。</p><label class="form-field"><span>图片 ID</span><UiInput :model-value="saved.image.id" readonly aria-label="编辑后图片 ID" /></label><label v-if="savedUrl" class="form-field"><span>图片链接</span><UiInput :model-value="savedUrl" readonly aria-label="编辑后图片链接" /></label><label v-if="saved.receipt" class="form-field"><span>一次性自删凭据 · 关闭后无法找回</span><UiInput :model-value="saved.receipt" readonly aria-label="一次性自删凭据" /></label><div class="inline-actions"><UiButton v-if="saved.receipt" icon="clipboard" @click="copy(saved.image.id)">复制图片 ID</UiButton><UiButton v-if="savedUrl" icon="link" @click="copy(savedUrl)">复制链接</UiButton><UiButton v-if="saved.receipt" icon="clipboard" @click="copy(saved.receipt)">复制自删凭据</UiButton><UiButton variant="primary" @click="visible=false">完成</UiButton></div></div>
    <div v-else class="editor-layout">
      <div class="editor-workspace"><p v-if="loading" role="status">正在读取原图…</p><div v-if="loaded" class="editor-canvas-wrap"><canvas ref="canvas" aria-label="图片编辑画布：拖动选择区域" @pointerdown="pointerDown" @pointermove="pointerMove" @pointerup="pointerUp" @pointercancel="pointerCancel" /></div><p v-if="loaded" class="field-help">{{ mode==='crop'?'在图片上拖动选择裁剪区域。':'在图片上拖动涂黑需要遮挡的区域。' }} 导出保留当前画布分辨率。</p><p v-if="image && !format.replaceable" class="field-help">动画或特殊格式会导出为 PNG 单帧，无法覆盖原文件。</p></div>
      <div class="editor-tools"><h3>编辑工具</h3><div class="editor-tool-row"><UiButton :variant="mode==='crop'?'primary':'secondary'" :disabled="!loaded || saving" @click="mode='crop'">裁剪</UiButton><UiButton :variant="mode==='redact'?'primary':'secondary'" :disabled="!loaded || saving" @click="mode='redact'">涂黑遮挡</UiButton></div><div class="editor-tool-row"><UiButton icon="rotate-ccw" :disabled="!loaded || saving" @click="rotate">顺时针旋转 90°</UiButton><UiButton :disabled="!crop || saving" @click="clearCrop">清除裁剪</UiButton><UiButton :disabled="!redactions.length || saving" @click="removeLastRedaction">撤销上次涂黑</UiButton><UiButton icon="refresh-cw" :disabled="!loaded || saving" @click="reset">重置编辑</UiButton></div><p v-if="loaded" class="field-help">输出 {{ crop?.width || dimensions.width }} × {{ crop?.height || dimensions.height }} 像素 · {{ redactions.length }} 处遮挡</p><label v-if="admin" class="form-field"><span>新图片可见性</span><UiSelect v-model="visibility" :disabled="saving" :options="[{label:'公开',value:'public'},{label:'不公开列出',value:'unlisted'},{label:'仅自己可见',value:'private'}]" aria-label="编辑后新图片可见性" /></label><div v-else-if="challenge?.enabled" class="editor-challenge"><span>访客验证</span><TurnstileWidget v-if="challenge.siteKey" ref="widget" :site-key="challenge.siteKey" @update:token="challengeToken=$event" /></div><p v-else-if="guestConfig && !guestConfig.enabled" class="field-help">公共上传未开放。你仍可在浏览器中预览编辑效果。</p><p v-if="error" role="alert" class="form-error">{{ error }}</p><div class="editor-save-actions"><UiButton icon="image-up" variant="primary" :loading="saving" :disabled="!canSaveNew" @click="save(false)">另存为新图片</UiButton><UiButton v-if="admin" icon="refresh-cw" :loading="saving" :disabled="!canReplace" @click="save(true)">替换原图</UiButton></div><p v-if="admin && !format.replaceable" class="field-help">原图格式无法由浏览器保持，支持另存为 PNG。</p></div>
    </div>
  </UiDialog>
</template>

<style scoped>
.editor-layout{display:grid;grid-template-columns:minmax(0,1fr) 270px;gap:20px}.editor-workspace{min-width:0}.editor-canvas-wrap{display:grid;place-items:center;min-height:240px;max-height:70vh;overflow:auto;background:#15171b;border:1px solid var(--border);border-radius:8px}.editor-canvas-wrap canvas{display:block;max-width:100%;max-height:62vh;touch-action:none;cursor:crosshair}.editor-tools{display:grid;align-content:start;gap:14px}.editor-tools h3,.editor-saved h3{margin:0}.editor-tool-row{display:flex;flex-wrap:wrap;gap:8px}.editor-save-actions{display:grid;gap:8px}.editor-challenge{display:grid;gap:8px}.editor-saved{display:grid;gap:14px}.editor-saved .inline-actions{flex-wrap:wrap}@media(max-width:800px){.editor-layout{grid-template-columns:1fr}.editor-tools{grid-row:1}.editor-canvas-wrap{max-height:55vh}.editor-canvas-wrap canvas{max-height:50vh}}
</style>
