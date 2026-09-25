<script setup>
import { computed, onMounted, ref } from 'vue';
import { request, toast } from '../runtime.js';
import { ask } from '../dialogs.js';

const list = ref([]), page = ref(1), pages = ref(1), total = ref(0), busy = ref(false), error = ref('');
const selectedID = ref(''), detailBusy = ref(false), form = ref({title:'',description:'',visibility:'unlisted'});
const detailImages = ref([]), imageIDs = ref([]), newIDs = ref(''), saving = ref(false), savingImages = ref(false);
const imageNames = computed(() => Object.fromEntries(detailImages.value.map(image => [image.id,image.originalName || image.filename || image.id])));
let detailVersion = 0;
async function loadList() {
  busy.value = true; error.value = '';
  try {
    const data = await request(`/api/admin/albums?page=${page.value}&limit=20`);
    list.value = data.albums || []; total.value = data.pagination?.total || 0; pages.value = Math.max(1,data.pagination?.totalPages || 0);
    if (page.value > pages.value) { page.value = pages.value; await loadList(); }
  } catch (reason) { error.value = reason.message; }
  finally { busy.value = false; }
}
function movePage(delta) { page.value += delta; loadList(); }
function resetForm() { detailVersion++; selectedID.value = ''; form.value = {title:'',description:'',visibility:'unlisted'}; detailImages.value = []; imageIDs.value = []; newIDs.value = ''; }
async function selectAlbum(id) {
  selectedID.value = id; detailBusy.value = true; error.value = '';
  const version = ++detailVersion;
  try {
    const data = await request(`/api/admin/albums/${encodeURIComponent(id)}`);
    if (version !== detailVersion) return;
    form.value = {title:data.album.title,description:data.album.description,visibility:data.album.visibility};
    detailImages.value = data.images || []; imageIDs.value = detailImages.value.map(image => image.id); newIDs.value = '';
  } catch (reason) { if (version === detailVersion) error.value = reason.message; }
  finally { if (version === detailVersion) detailBusy.value = false; }
}
async function saveAlbum() {
  if (!form.value.title.trim()) { error.value = '请输入相册标题'; return; }
  saving.value = true; error.value = '';
  try {
    const body = {title:form.value.title.trim(),description:form.value.description.trim(),visibility:form.value.visibility};
    const data = await request(selectedID.value ? `/api/admin/albums/${encodeURIComponent(selectedID.value)}` : '/api/admin/albums',{method:selectedID.value?'PUT':'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
    toast(selectedID.value?'相册已更新':'相册已创建'); await loadList(); await selectAlbum(data.album?.id || data.id || selectedID.value);
  } catch (reason) { error.value = reason.message; }
  finally { saving.value = false; }
}
async function removeAlbum() {
  if (!selectedID.value || !await ask('删除相册不会删除其中的图片。确定删除吗？',{title:'删除相册',accept:'删除相册',danger:true})) return;
  saving.value = true; error.value = '';
  try { await request(`/api/admin/albums/${encodeURIComponent(selectedID.value)}`,{method:'DELETE'}); resetForm(); await loadList(); toast('相册已删除'); }
  catch (reason) { error.value = reason.message; }
  finally { saving.value = false; }
}
function addIDs() {
  const ids = newIDs.value.split(/[\s,，]+/).map(value => value.trim()).filter(Boolean);
  imageIDs.value = [...new Set([...imageIDs.value,...ids])]; newIDs.value = '';
}
function removeID(id) { imageIDs.value = imageIDs.value.filter(value => value !== id); }
function moveID(index,delta) { const next = index+delta; if (next<0 || next>=imageIDs.value.length) return; const ids=[...imageIDs.value]; [ids[index],ids[next]]=[ids[next],ids[index]]; imageIDs.value=ids; }
async function saveImages() {
  if (!selectedID.value) return;
  savingImages.value = true; error.value = '';
  try {
    await request(`/api/admin/albums/${encodeURIComponent(selectedID.value)}/images`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({imageIds:imageIDs.value})});
    toast('相册图片已保存'); await selectAlbum(selectedID.value); await loadList();
  } catch (reason) { error.value = reason.message; }
  finally { savingImages.value = false; }
}
onMounted(loadList);
</script>

<template>
  <section class="page-view"><div class="section-title"><div><span class="eyebrow">ALBUMS</span><h1>相册管理</h1><p>创建相册，设置可见性，并排列图片顺序。</p></div><div class="inline-actions"><RouterLink to="/albums" class="outline-button">查看公开相册</RouterLink><UiButton icon="refresh-cw" :loading="busy" @click="loadList">刷新</UiButton></div></div>
    <p v-if="error" class="form-error" role="alert">{{ error }}</p>
    <section class="settings-panel"><div class="album-admin-heading"><h2>相册列表</h2><UiButton icon="plus" @click="resetForm">新建相册</UiButton></div><div v-if="busy && !list.length" class="loading-panel" role="status">正在读取相册…</div><p v-else-if="!list.length" class="empty">尚未创建相册。</p><div v-else class="album-admin-list"><article v-for="item in list" :key="item.id" :class="['album-admin-row',{'is-selected':selectedID===item.id}]"><div><strong>{{ item.title }}</strong><span>{{ item.imageCount }} 张 · {{ item.visibility==='public'?'公开':item.visibility==='unlisted'?'不公开列出':'仅管理员' }}</span></div><UiButton @click="selectAlbum(item.id)">管理</UiButton></article></div><nav class="album-admin-pages" aria-label="相册分页"><span>第 {{ page }} / {{ pages }} 页 · 共 {{ total }} 个</span><UiButton :disabled="busy || page<=1" @click="movePage(-1)">上一页</UiButton><UiButton :disabled="busy || page>=pages" @click="movePage(1)">下一页</UiButton></nav></section>
    <section class="settings-panel"><h2>{{ selectedID?'编辑相册':'新建相册' }}</h2><p v-if="detailBusy" role="status">正在读取相册详情…</p><form v-else class="album-admin-form" @submit.prevent="saveAlbum"><label class="form-field"><span>标题</span><UiInput v-model="form.title" :maxlength="120" aria-label="相册标题" /></label><label class="form-field"><span>描述</span><UiInput v-model="form.description" type="textarea" :rows="3" :maxlength="1000" aria-label="相册描述" /></label><label class="form-field"><span>可见性</span><UiSelect v-model="form.visibility" :options="[{label:'公开 · 出现在相册列表',value:'public'},{label:'不公开列出 · 凭链接访问',value:'unlisted'},{label:'仅管理员可见',value:'private'}]" aria-label="相册可见性" /></label><p class="field-help">公开相册仅展示公开图片；不公开列出的相册可以展示公开及不公开列出的图片。</p><div class="inline-actions"><UiButton type="submit" variant="primary" :loading="saving">{{ selectedID?'保存相册':'创建相册' }}</UiButton><UiButton v-if="selectedID" variant="danger" :disabled="saving" @click="removeAlbum">删除相册</UiButton><RouterLink v-if="selectedID && form.visibility!=='private'" :to="`/albums/${encodeURIComponent(selectedID)}`" class="outline-button">打开相册</RouterLink></div></form></section>
    <section v-if="selectedID" class="settings-panel"><h2>相册图片</h2><p class="field-help">从图库勾选图片并选择“加入相册”，或在此输入图片 ID。更改顺序后点击保存。</p><div class="album-admin-add"><UiInput v-model="newIDs" type="textarea" :rows="2" aria-label="要加入相册的图片 ID" placeholder="每行或逗号分隔一个图片 ID" /><UiButton @click="addIDs">加入列表</UiButton></div><p v-if="!imageIDs.length" class="empty">相册中尚无图片。</p><div v-else class="album-admin-images"><article v-for="(id,index) in imageIDs" :key="id"><span>{{ index+1 }}. {{ imageNames[id] || id }}</span><div class="inline-actions"><UiButton :disabled="index===0" @click="moveID(index,-1)">上移</UiButton><UiButton :disabled="index===imageIDs.length-1" @click="moveID(index,1)">下移</UiButton><UiButton variant="danger" @click="removeID(id)">移除</UiButton></div></article></div><UiButton variant="primary" :loading="savingImages" @click="saveImages">保存图片与顺序</UiButton></section>
  </section>
</template>

<style scoped>
.album-admin-heading,.album-admin-row,.album-admin-pages,.album-admin-images article {display:flex;align-items:center;justify-content:space-between;gap:12px}.album-admin-heading h2 {margin:0}.album-admin-list,.album-admin-images {display:grid;gap:8px;margin:16px 0}.album-admin-row,.album-admin-images article {padding:12px;border:1px solid var(--border);border-radius:8px}.album-admin-row.is-selected {border-color:var(--primary)}.album-admin-row span {display:block;margin-top:4px;font-size:12px;color:var(--secondary)}.album-admin-pages {justify-content:flex-end;font-size:12px;color:var(--secondary)}.album-admin-form {display:grid;gap:16px;margin-top:16px;max-width:700px}.album-admin-add {display:flex;align-items:end;gap:10px;margin:18px 0}.album-admin-add .ui-input {flex:1}.album-admin-images article span {overflow-wrap:anywhere;min-width:0}.album-admin-images .inline-actions {flex-wrap:wrap}@media(max-width:640px){.album-admin-row,.album-admin-images article,.album-admin-add {align-items:stretch;flex-direction:column}}
</style>
