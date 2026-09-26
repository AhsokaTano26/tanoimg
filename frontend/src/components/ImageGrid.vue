<script setup>
import { computed, defineAsyncComponent, onMounted, onBeforeUnmount, ref, watch } from 'vue';
import { useRouter } from 'vue-router';
import { request, session, site, toast, updateSite } from '../runtime.js';
import { ask } from '../dialogs.js';
import { imageLinks } from '../image-links.js';
import { fetchSelectionIDs, sendSelectionBatches } from '../image-selection.js';
import { renderEmbedTemplate } from '../embed-templates.js';
const ImageEditor=defineAsyncComponent(()=>import('./ImageEditor.vue'));
const props = defineProps({ images:Array, busy:Boolean, error:String, selectable:Boolean, recycle:Boolean, selectionFilters:Object });
const emit = defineEmits(['refresh','selection-busy']);
const router = useRouter();
const selected = ref([]), selectingAll = ref(false), allScope = ref(false), batchBusy = ref(false);
let selectionController;
watch(() => selectingAll.value || batchBusy.value, value => emit('selection-busy',value));
const selectionLocked = computed(() => props.busy || !!props.error || selectingAll.value || batchBusy.value);
const selectedSet = computed(() => new Set(selected.value));
const allSelected = computed(() => !!props.images?.length && props.images.every(image => selectedSet.value.has(image.id)));
function selectPage() { if (!selectionLocked.value) { allScope.value=false; selected.value = [...new Set((props.images || []).map(image => image.id))]; } }
function invertSelection() { if (!selectionLocked.value) { allScope.value=false; selected.value = (props.images || []).filter(image => !selectedSet.value.has(image.id)).map(image => image.id); } }
function clearSelection() { selectionController?.abort(); selectingAll.value=false; allScope.value=false; selected.value=[]; }
watch(() => props.busy, busy => { if (busy && !allScope.value) clearSelection(); });
watch(() => JSON.stringify(props.selectionFilters || {}), clearSelection);
onBeforeUnmount(() => selectionController?.abort());
async function selectAll() {
  if (selectionLocked.value) return;
  const controller = selectionController = new AbortController();
  selectingAll.value=true;
  try {
    const ids=await fetchSelectionIDs(props.selectionFilters,request,controller.signal);
    selected.value=ids; allScope.value=true;
    toast(`已选择当前筛选结果中的 ${ids.length} 张图片`);
  } catch(error) { if(!controller.signal.aborted)toast(error.message); }
  finally { if(selectionController===controller)selectingAll.value=false; }
}
async function runSelectedBatches(endpoint,method,extra,countField) {
  if(selectionLocked.value || !selected.value.length)return;
  batchBusy.value=true; const ids=[...selected.value]; let count=0;
  try {
    await sendSelectionBatches(ids, batch => request(endpoint,{method,headers:{'Content-Type':'application/json'},body:JSON.stringify({ids:batch,...extra})}), (batch,result) => {
      count+=result[countField]||0;
      const done=new Set(batch);selected.value=selected.value.filter(id=>!done.has(id));
    });
    allScope.value=false; toast(`已处理 ${count} 张图片`); emit('refresh');
  } catch(error) { toast(`已处理 ${count} 张图片，剩余 ${selected.value.length} 张未完成：${error.message}`); }
  finally {batchBusy.value=false;}
}
const selectedVisibility = ref('unlisted');
const metadata = ref({ alt:'', author:'', license:'', tags:'' });
const shareDialog = ref(false), shareTitle = ref(''), shareExpiry = ref(0), createdShareUrl = ref(''), creatingShare = ref(false), shareError = ref('');
const reportDialog = ref(false), reportReason = ref(''), reportDetails = ref(''), reportError = ref(''), reporting = ref(false);
const albumDialog = ref(false), albums = ref([]), albumPage = ref(1), albumPages = ref(1), albumID = ref(''), albumLoading = ref(false), albumSaving = ref(false), albumError = ref('');
const versions = ref([]), versionBusy = ref(false), replaceBusy = ref(false), lifecycleError = ref('');
const embedTemplates = ref(null);
watch(shareDialog, value => { if (!value) createdShareUrl.value = ''; });
watch(() => props.images, images => { const ids=new Set((images || []).map(image=>image.id)); if(!allScope.value)selected.value=selected.value.filter(id=>ids.has(id)); });
const menu = ref();
const current = ref(null);
const preview = ref(false);
const editorVisible=ref(false),editorImage=ref(null);
function openEditor(image=current.value){if(!image || props.recycle)return;editorImage.value=image;preview.value=false;editorVisible.value=true;}
function editorSaved(image){if(image.id===current.value?.id)current.value=image;emit('refresh');}
const copying = async format => {
  try { const value=typeof format==='string' ? imageLinks(current.value,location.origin)[format] : renderEmbedTemplate(format,current.value,location.origin);await navigator.clipboard.writeText(value); toast((typeof format==='string'?format:format.name)+' 已复制'); }
  catch { toast('复制失败，请检查浏览器剪贴板权限'); }
};
onMounted(async()=>{try{const data=await request('/api/embed-templates');if(Array.isArray(data))embedTemplates.value=data;}catch{embedTemplates.value=null;}});
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
  if (selectionLocked.value || !selected.value.length) return;
  if (!await ask('将选中的 '+selected.value.length+' 张图片移入回收站？',{danger:true,accept:'移入回收站'})) return;
  await runSelectedBatches('/api/images/batch','DELETE',{},'deletedCount');
}
async function updateSelectedVisibility() {
  await runSelectedBatches('/api/images/visibility','PUT',{visibility:selectedVisibility.value},'updatedCount');
}
async function saveMetadata() {
  if (!current.value) return;
  try {
    await request('/api/images/'+encodeURIComponent(current.value.id)+'/metadata',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({...metadata.value,tags:metadata.value.tags.split(',').map(tag=>tag.trim()).filter(Boolean)})});
    toast('图片信息已保存');emit('refresh');
  } catch(error) { toast(error.message); }
}
function exportSelected() {
  if (!selected.value.length) return;
  const query=new URLSearchParams();
  selected.value.forEach(id=>query.append('id',id));
  const link=document.createElement('a');
  link.href='/api/images/export?'+query.toString();
  link.download='tanoimg-images.zip';
  document.body.append(link);
  link.click();
  link.remove();
}
function openShareDialog() {
  if (!selected.value.length) return;
  shareTitle.value = selected.value.length === 1 ? (props.images.find(image => image.id === selected.value[0])?.originalName || '') : '';
  shareExpiry.value = 0; createdShareUrl.value = ''; shareError.value = ''; shareDialog.value = true;
}
async function createShare() {
  creatingShare.value = true; shareError.value = '';
  try {
    const data = await request('/api/admin/shares',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({imageIds:selected.value,title:shareTitle.value.trim(),expiresInDays:shareExpiry.value})});
    createdShareUrl.value = new URL(data.url,location.origin).href;
    selected.value = [];
    toast('分享已创建，请保存此链接');
  } catch (error) { shareError.value = error.message; }
  finally { creatingShare.value = false; }
}
async function copyShareUrl() {
  try { await navigator.clipboard.writeText(createdShareUrl.value); toast('分享链接已复制'); }
  catch { toast('复制失败，请手动复制链接'); }
}
async function loadAlbums() {
  albumLoading.value = true; albumError.value = '';
  try {
    const data = await request(`/api/admin/albums?page=${albumPage.value}&limit=100`);
    albums.value = data.albums || []; albumPages.value = Math.max(1,data.pagination?.totalPages || 0);
    if (!albums.value.some(album=>album.id===albumID.value)) albumID.value = albums.value[0]?.id || '';
  } catch(error) { albumError.value = error.message; }
  finally { albumLoading.value = false; }
}
function openAlbumDialog() { if (!selected.value.length) return; albumPage.value = 1; albumID.value = ''; albumDialog.value = true; loadAlbums(); }
function moveAlbumPage(delta) { albumPage.value += delta; loadAlbums(); }
async function addToAlbum() {
  if (!albumID.value || !selected.value.length) return;
  albumSaving.value = true; albumError.value = '';
  try {
    const data = await request(`/api/admin/albums/${encodeURIComponent(albumID.value)}`);
    const ids = [...new Set([...(data.images || []).map(image=>image.id),...selected.value])];
    await request(`/api/admin/albums/${encodeURIComponent(albumID.value)}/images`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({imageIds:ids})});
    selected.value = []; albumDialog.value = false; toast('图片已加入相册');
  } catch(error) { albumError.value = error.message; }
  finally { albumSaving.value = false; }
}
function openReport() { reportReason.value = ''; reportDetails.value = ''; reportError.value = ''; reportDialog.value = true; }
async function sendReport() {
  if (!current.value || !reportReason.value) { reportError.value = '请选择举报原因'; return; }
  reporting.value = true; reportError.value = '';
  try {
    await request(`/api/images/${encodeURIComponent(current.value.id)}/report`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({reason:reportReason.value,details:reportDetails.value.trim()})});
    reportDialog.value = false; toast('举报已提交，感谢反馈');
  } catch (error) { reportError.value = error.message; }
  finally { reporting.value = false; }
}
const items = computed(() => [
  {label:'查看详情',symbol:'arrow-up-right',command:()=>router.push(`/image/${encodeURIComponent(current.value.id)}`)},
  {label:'编辑图片',symbol:'sliders-horizontal',command:()=>openEditor()},
  ...((embedTemplates.value || ['直链','HTML','Markdown','BBCode'].map(label=>({name:label,legacy:true}))).map(template=>({label:template.name,symbol:template.name==='直链'?'link':'code-xml',command:()=>copying(template.legacy?template.name:template)}))),
  {separator:true},
  {label:'设为全局背景',symbol:'images',disabled:!session.admin,command:()=>setAppearance('backgroundUrl')},
  {label:'设为网站 Logo',symbol:'aperture',disabled:!session.admin,command:()=>setAppearance('appLogo')},
  {separator:true},
  ...(!session.admin ? [{label:'举报图片',symbol:'triangle-alert',command:openReport}] : []),
  {label:session.admin?'删除':'删除（管理员登录）',symbol:'trash-2',danger:true,disabled:!session.admin,command:()=>remove()},
]);
function openMenu(event,image) {
  current.value=image;
  const rect=event.currentTarget.getBoundingClientRect();
  menu.value.show({pageX:event.pageX || rect.left+window.scrollX+16,pageY:event.pageY || rect.bottom+window.scrollY,preventDefault:()=>event.preventDefault(),stopPropagation:()=>event.stopPropagation()});
}
function select(image,value) {
  if (selectionLocked.value) return;
  selected.value = value ? [...new Set([...selected.value,image.id])] : selected.value.filter(id=>id!==image.id);
}
const imageSrc = image => image?.revision ? `${image.url}?r=${image.revision}` : image?.url;
const thumbUrl = image => ['jpg','jpeg','png','gif','apng'].includes(image.format?.toLowerCase()) && image.filename ? `/t/${encodeURIComponent(image.filename)}${image.revision ? `?r=${image.revision}` : ''}` : imageSrc(image);
function thumbFailed(event,image) { const original = new URL(imageSrc(image),location.origin).href; if (event.target.src !== original) event.target.src = original; }
async function loadVersions() {
  if (!current.value || !session.admin) return;
  versionBusy.value = true; lifecycleError.value = '';
  try { const data = await request(`/api/admin/images/${encodeURIComponent(current.value.id)}/versions`); versions.value = data.versions || []; }
  catch (error) { lifecycleError.value = error.message; }
  finally { versionBusy.value = false; }
}
async function replaceImage(files) {
  const file = files?.[0]; if (!file || !current.value) return;
  if (!await ask(`使用「${file.name}」替换当前图片？原直链将继续指向新内容。`,{title:'替换图片',accept:'替换图片'})) return;
  replaceBusy.value = true; lifecycleError.value = '';
  try {
    const body = new FormData(); body.append('file',file);
    current.value = await request(`/api/admin/images/${encodeURIComponent(current.value.id)}/replace`,{method:'POST',body});
    toast('图片已替换，原直链保持不变'); emit('refresh'); await loadVersions();
  } catch (error) { lifecycleError.value = error.message; }
  finally { replaceBusy.value = false; }
}
async function rollbackVersion(version) {
  if (!current.value || !await ask(`恢复版本 ${version.revision}？当前内容会作为新版本保存。`,{title:'恢复历史版本',accept:'恢复版本'})) return;
  versionBusy.value = true; lifecycleError.value = '';
  try {
    current.value = await request(`/api/admin/images/${encodeURIComponent(current.value.id)}/rollback/${encodeURIComponent(version.id)}`,{method:'POST'});
    toast('历史版本已恢复'); emit('refresh'); await loadVersions();
  } catch (error) { lifecycleError.value = error.message; }
  finally { versionBusy.value = false; }
}
function openPreview(image) { current.value=image;metadata.value={alt:image.alt||'',author:image.author||'',license:image.license||'',tags:(image.tags||[]).join(', ')};versions.value=[];lifecycleError.value='';preview.value=true;if(session.admin && !props.recycle)loadVersions(); }
</script>
<template>
  <section v-if="selectable && session.admin" class="gallery-batch" aria-label="图片批量选择"><div class="gallery-select-controls"><span role="status" aria-live="polite">已选择 {{ selected.length }} 张{{ allScope ? '（跨页）' : '（本页）' }}</span><UiButton icon="check" :disabled="selectionLocked || !images?.length || (allSelected && !allScope)" @click="selectPage">全选本页</UiButton><UiButton icon="images" :loading="selectingAll" :disabled="selectionLocked || !images?.length" @click="selectAll">全选全部</UiButton><UiButton icon="refresh-cw" :disabled="selectionLocked || !images?.length" @click="invertSelection">反选本页</UiButton><UiButton icon="x" :disabled="batchBusy || (!selected.length && !selectingAll)" @click="clearSelection">取消选择</UiButton><small>全选全部按当前筛选条件跨页选择；更改筛选后清空选择。</small></div><div class="gallery-selection"><UiSelect v-model="selectedVisibility" aria-label="批量可见性" :options="[{label:'公开',value:'public'},{label:'不列出',value:'unlisted'},{label:'真正私有',value:'private'}]" /><UiButton :disabled="selectionLocked || !selected.length" @click="updateSelectedVisibility">设置可见性</UiButton><UiButton icon="images" :disabled="selectionLocked || !selected.length || selected.length>1000" @click="openAlbumDialog">加入相册</UiButton><UiButton icon="link" :disabled="selectionLocked || !selected.length || selected.length>100" @click="openShareDialog">创建分享</UiButton><UiButton icon="download" :disabled="selectionLocked || !selected.length || selected.length>1000" @click="exportSelected">导出 ZIP</UiButton><UiButton :disabled="selectionLocked || !selected.length" variant="danger" :loading="batchBusy" @click="batchRemove">删除所选</UiButton></div><p v-if="selected.length>100" class="field-help">创建分享最多 100 张；加入相册与单次 ZIP 导出最多 1000 张。删除和设置可见性支持自动分批处理全部所选。</p></section>
  <p v-if="error" class="form-error" role="alert">{{ error }}</p>
  <div :class="['gallery-grid',{'is-empty':!images?.length}]" :aria-busy="busy">
    <div v-if="!images?.length" class="empty"><strong>{{ busy ? '正在加载图片…' : recycle ? '回收站为空' : '这里还没有图片' }}</strong><span v-if="!busy">{{ recycle ? '删除的图片会在这里显示，清空前可以恢复。' : '上传第一张图片，开始记录你的灵感。' }}</span></div>
    <article v-for="image in images" :key="image.id" :class="['image-card',{'is-selected':selectedSet.has(image.id)}]" @contextmenu.prevent="!recycle && openMenu($event,image)">
      <div v-if="selectable && session.admin" class="image-select"><UiCheckbox :modelValue="selectedSet.has(image.id)" :disabled="selectionLocked" :label="'选择 '+image.originalName" @update:modelValue="select(image,$event)" /></div>
      <UiButton class="image-preview" :aria-label="'预览 '+image.originalName" @click="openPreview(image)" @keydown.shift.f10.prevent="openMenu($event,image)"><img :src="thumbUrl(image)" :alt="image.originalName" loading="lazy" @error="thumbFailed($event,image)"></UiButton>
      <div class="image-meta"><div class="image-name">{{ image.originalName || image.filename }}</div><div class="image-sub">{{ (image.size/1024).toFixed(1) }} KB · {{ image.width || '—' }} × {{ image.height || '—' }}<span v-if="selectable" class="visibility-tag">{{ image.visibility==='public' ? '公开' : image.visibility==='private' ? '真正私有' : '不列出' }}</span></div></div>
      <div class="image-actions">
        <UiButton v-if="recycle" icon="rotate-ccw" @click="restore(image)">恢复图片</UiButton>
        <template v-else><UiButton icon="link" @click="current=image;copying('直链')">复制直链</UiButton><UiButton icon="ellipsis" :aria-label="'更多操作 '+image.originalName" @click="openMenu($event,image)">更多</UiButton></template>
      </div>
    </article>
  </div>
  <UiMenu ref="menu" :items="items" />
  <UiDialog v-model:visible="preview" title="图片预览" width="1000px"><img v-if="current" class="preview-image" :src="imageSrc(current)" :alt="current.alt || current.originalName"><p class="field-help">{{ current?.originalName }}</p><UiButton v-if="!recycle" icon="sliders-horizontal" @click="openEditor(current)">裁剪 / 旋转 / 涂黑</UiButton><div v-if="selectable && session.admin" class="image-metadata-form"><label>替代文字<UiInput v-model="metadata.alt" maxlength="500" /></label><label>作者<UiInput v-model="metadata.author" maxlength="200" /></label><label>授权方式<UiInput v-model="metadata.license" maxlength="200" /></label><label>标签（以逗号分隔）<UiInput v-model="metadata.tags" maxlength="500" /></label><UiButton variant="primary" @click="saveMetadata">保存信息</UiButton></div><section v-if="selectable && session.admin && !recycle" class="image-metadata-form"><div><strong>替换与历史版本</strong><p class="field-help">替换文件必须与原图格式一致；原直链保持不变。</p></div><UiFile label="选择替换图片" :disabled="replaceBusy || versionBusy" @select="replaceImage" /><p v-if="lifecycleError" role="alert" class="form-error">{{ lifecycleError }}</p><div class="image-version-list"><p v-if="versionBusy" role="status">正在读取历史版本…</p><p v-else-if="!versions.length" class="field-help">暂无可恢复的历史版本。</p><article v-for="version in versions" :key="version.id"><span>版本 {{ version.revision }} · {{ version.originalName }} · {{ (version.size/1048576).toFixed(2) }} MB<br><small>{{ version.createdAt }}</small></span><UiButton :disabled="versionBusy || replaceBusy" @click="rollbackVersion(version)">恢复此版本</UiButton></article></div></section></UiDialog>
  <ImageEditor v-if="editorVisible" v-model:visible="editorVisible" :image="editorImage" :admin="session.admin" @saved="editorSaved" />
  <UiDialog v-model:visible="shareDialog" :title="createdShareUrl?'分享已创建':'创建图片分享'">
    <div v-if="createdShareUrl" class="form-stack"><p>分享链接仅在创建时显示，请现在保存。</p><label class="form-field"><span>分享链接</span><UiInput :model-value="createdShareUrl" readonly aria-label="新建分享链接" /></label><div class="inline-actions"><UiButton icon="link" variant="primary" @click="copyShareUrl">复制链接</UiButton><UiButton @click="shareDialog=false">完成</UiButton></div></div>
    <form v-else class="form-stack" @submit.prevent="createShare"><p>将 {{ selected.length }} 张图片放入一个分享页。链接可访问这些图片，包括不公开列出和仅自己可见的图片。</p><label class="form-field"><span>标题</span><UiInput v-model="shareTitle" :maxlength="200" aria-label="分享标题" placeholder="可选" /></label><label class="form-field"><span>有效期</span><UiSelect v-model="shareExpiry" :options="[{label:'长期有效',value:0},{label:'1 天',value:1},{label:'7 天',value:7},{label:'30 天',value:30},{label:'365 天',value:365}]" aria-label="分享有效期" /></label><p v-if="shareError" role="alert" class="form-error">{{ shareError }}</p><UiButton type="submit" variant="primary" :loading="creatingShare" :disabled="selectionLocked || !selected.length">创建分享</UiButton></form>
  </UiDialog>
  <UiDialog v-model:visible="reportDialog" title="举报图片"><form class="form-stack" @submit.prevent="sendReport"><p>举报 {{ current?.originalName || current?.filename }}。举报信息将交由管理员处理。</p><label class="form-field"><span>原因</span><UiSelect v-model="reportReason" :options="[{label:'请选择原因',value:''},{label:'版权问题',value:'copyright'},{label:'隐私问题',value:'privacy'},{label:'滥用内容',value:'abuse'},{label:'垃圾内容',value:'spam'},{label:'其他',value:'other'}]" aria-label="举报原因" /></label><label class="form-field"><span>补充说明</span><UiInput v-model="reportDetails" type="textarea" :rows="3" :maxlength="1000" aria-label="举报说明" placeholder="可选，最多 1000 字" /></label><p v-if="reportError" role="alert" class="form-error">{{ reportError }}</p><div class="inline-actions"><UiButton type="submit" variant="primary" :loading="reporting">提交举报</UiButton><UiButton @click="reportDialog=false">取消</UiButton></div></form></UiDialog>
  <UiDialog v-model:visible="albumDialog" title="加入相册"><div class="form-stack"><p>将选中的 {{ selected.length }} 张图片加入相册。已在相册中的图片会保留原顺序。</p><p v-if="albumError" role="alert" class="form-error">{{ albumError }}</p><p v-if="albumLoading" role="status">正在读取相册…</p><template v-else-if="albums.length"><label class="form-field"><span>目标相册</span><UiSelect v-model="albumID" :options="albums.map(album=>({label:album.title,value:album.id}))" aria-label="目标相册" /></label><div class="inline-actions"><UiButton :disabled="albumPage<=1" @click="moveAlbumPage(-1)">上一页相册</UiButton><span>{{ albumPage }} / {{ albumPages }}</span><UiButton :disabled="albumPage>=albumPages" @click="moveAlbumPage(1)">下一页相册</UiButton></div><UiButton variant="primary" :loading="albumSaving" @click="addToAlbum">加入相册</UiButton></template><p v-else class="empty">尚无相册。请先在相册管理中创建。</p><RouterLink to="/admin/albums" class="outline-button" @click="albumDialog=false">管理相册</RouterLink></div></UiDialog>
</template>
<style scoped>
.gallery-batch {display:grid;gap:12px;margin-bottom:24px;padding:16px;border:1px solid var(--border);border-radius:12px;background:var(--surface)}
.gallery-select-controls {display:flex;align-items:center;flex-wrap:wrap;gap:8px}
.gallery-select-controls>span {margin-right:8px;font-weight:700;font-variant-numeric:tabular-nums}
.gallery-select-controls small {color:var(--secondary);line-height:1.5}
.gallery-batch .gallery-selection {margin:0;padding:0;border:0;background:transparent}
.image-card.is-selected {outline:2px solid var(--primary);outline-offset:-2px}
@media(max-width:640px) {.gallery-select-controls>span,.gallery-select-controls small {width:100%}}

.image-version-list {grid-column:1/-1;display:grid;gap:8px}.image-version-list article {display:flex;align-items:center;justify-content:space-between;gap:12px;padding:10px;border:1px solid var(--border);border-radius:8px;min-width:0}.image-version-list span {overflow-wrap:anywhere;min-width:0}.image-version-list small {color:var(--secondary)}
</style>
