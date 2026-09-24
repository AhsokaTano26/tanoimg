<script setup>
import { computed, ref, watch } from 'vue';
import { request, session, site, toast, updateSite } from '../runtime.js';
import { ask } from '../dialogs.js';
import { imageLinks } from '../image-links.js';
const props = defineProps({ images:Array, busy:Boolean, error:String, selectable:Boolean, recycle:Boolean });
const emit = defineEmits(['refresh']);
const selected = ref([]);
watch(() => props.images, images => { const ids=new Set((images || []).map(image=>image.id)); selected.value=selected.value.filter(id=>ids.has(id)); });
const menu = ref();
const current = ref(null);
const preview = ref(false);
const copying = async format => {
  try { await navigator.clipboard.writeText(imageLinks(current.value,location.origin)[format]); toast(format+' 已复制'); }
  catch { toast('复制失败，请检查浏览器剪贴板权限'); }
};
async function setAppearance(kind) {
  try {
    const saved = await request('/api/settings',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({ [kind]:current.value.url })});
    updateSite(saved); toast(kind==='appLogo' ? '网站 Logo 已更新' : '全局背景已更新');
  } catch(error) { toast(error.message); }
}
async function remove(image = current.value) {
  if (!await ask('将这张图片移入回收站？之后可以恢复。',{title:'删除图片',accept:'移入回收站',danger:true})) return;
  try { await request('/api/images/'+encodeURIComponent(image.id),{method:'DELETE'}); toast('图片已移入回收站'); emit('refresh'); }
  catch(error) { toast(error.message); }
}
async function restore(image) {
  try { await request('/api/images/'+encodeURIComponent(image.id)+'/restore',{method:'PUT'}); toast('图片已恢复'); emit('refresh'); }
  catch(error) { toast(error.message); }
}
async function batchRemove() {
  if (!await ask('将选中的 '+selected.value.length+' 张图片移入回收站？',{danger:true,accept:'移入回收站'})) return;
  try { await request('/api/images/batch',{method:'DELETE',headers:{'Content-Type':'application/json'},body:JSON.stringify({ids:selected.value})}); selected.value=[]; emit('refresh'); }
  catch(error) { toast(error.message); }
}
const items = computed(() => [
  ...['直链','HTML','Markdown','BBCode'].map(label=>({label,symbol:label==='直链'?'link':'code-xml',command:()=>copying(label)})),
  {separator:true},
  {label:'设为全局背景',symbol:'images',disabled:!session.admin,command:()=>setAppearance('backgroundUrl')},
  {label:'设为网站 Logo',symbol:'aperture',disabled:!session.admin,command:()=>setAppearance('appLogo')},
  {separator:true},
  {label:session.admin?'删除':'删除（管理员登录）',symbol:'trash-2',danger:true,disabled:!session.admin,command:()=>remove()},
]);
function openMenu(event,image) {
  current.value=image;
  const rect=event.currentTarget.getBoundingClientRect();
  menu.value.show({pageX:event.pageX || rect.left+window.scrollX+16,pageY:event.pageY || rect.bottom+window.scrollY,preventDefault:()=>event.preventDefault(),stopPropagation:()=>event.stopPropagation()});
}
function select(image,value) {
  selected.value = value ? [...selected.value,image.id] : selected.value.filter(id=>id!==image.id);
}
function openPreview(image) { current.value=image; preview.value=true; }
</script>
<template>
  <div v-if="selectable && session.admin" class="gallery-selection"><span>已选择 {{ selected.length }} 张</span><UiButton :disabled="!selected.length" variant="danger" @click="batchRemove">删除所选</UiButton></div>
  <p v-if="error" class="form-error" role="alert">{{ error }}</p>
  <div :class="['gallery-grid',{'is-empty':!images?.length}]" :aria-busy="busy">
    <div v-if="!images?.length" class="empty"><strong>{{ busy ? '正在加载图片…' : recycle ? '回收站为空' : '这里还没有图片' }}</strong><span v-if="!busy">{{ recycle ? '删除的图片会在这里显示，清空前可以恢复。' : '上传第一张图片，开始记录你的灵感。' }}</span></div>
    <article v-for="image in images" :key="image.id" class="image-card" @contextmenu.prevent="!recycle && openMenu($event,image)">
      <div v-if="selectable && session.admin" class="image-select"><UiCheckbox :modelValue="selected.includes(image.id)" :label="'选择 '+image.originalName" @update:modelValue="select(image,$event)" /></div>
      <UiButton class="image-preview" :aria-label="'预览 '+image.originalName" @click="openPreview(image)" @keydown.shift.f10.prevent="openMenu($event,image)"><img :src="image.url" :alt="image.originalName" loading="lazy"></UiButton>
      <div class="image-meta"><div class="image-name">{{ image.originalName || image.filename }}</div><div class="image-sub">{{ (image.size/1024).toFixed(1) }} KB · {{ image.width || '—' }} × {{ image.height || '—' }}<span v-if="selectable" class="visibility-tag">{{ image.uploadedByType==='public' ? '公开' : '私人' }}</span></div></div>
      <div class="image-actions">
        <UiButton v-if="recycle" icon="rotate-ccw" @click="restore(image)">恢复图片</UiButton>
        <template v-else><UiButton icon="link" @click="current=image;copying('直链')">复制直链</UiButton><UiButton icon="ellipsis" :aria-label="'更多操作 '+image.originalName" @click="openMenu($event,image)">更多</UiButton></template>
      </div>
    </article>
  </div>
  <UiMenu ref="menu" :items="items" />
  <UiDialog v-model:visible="preview" title="图片预览" width="1000px"><img v-if="current" class="preview-image" :src="current.url" :alt="current.originalName"><p class="field-help">{{ current?.originalName }}</p></UiDialog>
</template>
