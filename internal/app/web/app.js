import { createUploadQueue } from './upload-queue.mjs';

const $ = (selector) => document.querySelector(selector);
const state = { admin: false, page: 1, totalPages: 1, publicEnabled: false, publicConfig: null };

function updateUploadHint() {
  if (state.admin) { $('#upload-hint').textContent = '批量上传自动使用 4 路并发，可随时取消'; return; }
  const config = state.publicConfig;
  if (!config) return;
  $('#upload-hint').textContent = config.enabled ? `支持 ${config.allowedFormats.join('、').toUpperCase()} · 单张最大 ${Math.round(config.maxFileSize / 1048576)} MB` : '公开上传已关闭，请管理员登录后上传';
}

function imageCSS(url) { return url ? `url(${JSON.stringify(url)})` : 'none'; }
function applyBackground(url, blur) {
  const layer = $('#site-background');
  layer.style.backgroundImage = imageCSS(url);
  layer.style.setProperty('--background-blur', `${blur}px`);
}
function previewBackground() {
  const url = $('#background-url').value.trim();
  const blur = Number($('#background-blur').value);
  const preview = $('#background-preview');
  preview.style.setProperty('--preview-image', imageCSS(url));
  preview.style.setProperty('--preview-blur', `${blur}px`);
  $('#blur-value').textContent = `${blur} px`;
}
function fillAppearance(settings) {
  $('#background-url').value = settings.backgroundUrl || '';
  $('#background-blur').value = Number.isInteger(settings.backgroundBlur) ? settings.backgroundBlur : 0;
  previewBackground();
  applyBackground(settings.backgroundUrl || '', Number(settings.backgroundBlur) || 0);
}
async function saveAppearance(url, blur) {
  const settings = await api('/api/settings/appearance', { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ backgroundUrl: url, backgroundBlur: blur }) });
  fillAppearance(settings);
  toast('外观设置已保存');
}

async function api(url, options = {}) {
  const response = await fetch(url, { credentials: 'same-origin', ...options });
  let body;
  try { body = await response.json(); } catch { throw new Error('服务器返回了无效响应'); }
  if (!response.ok || !body.success) {
    const error = new Error(body.message || `请求失败 (${response.status})`);
    error.status = response.status;
    throw error;
  }
  return body.data;
}

let toastTimer;
function toast(message) {
  const el = $('#toast'); el.textContent = message; el.classList.add('show');
  clearTimeout(toastTimer); toastTimer = setTimeout(() => el.classList.remove('show'), 3200);
}

function showTab(tab) {
  document.querySelectorAll('.view').forEach(el => el.classList.toggle('active', el.id === `${tab}-view`));
  document.querySelectorAll('.nav-link').forEach(el => el.classList.toggle('active', el.dataset.tab === tab));
  if (tab === 'gallery') loadGallery();
  if (tab === 'settings') loadSettings();
  history.replaceState(null, '', tab === 'upload' ? '/' : `/${tab}`);
  window.scrollTo(0, 0);
}

function imageCard(image) {
  const card = document.createElement('article'); card.className = 'image-card';
  const img = document.createElement('img'); img.src = image.url; img.alt = image.originalName || '图片'; img.loading = 'lazy';
  const meta = document.createElement('div'); meta.className = 'image-meta';
  const name = document.createElement('div'); name.className = 'image-name'; name.textContent = image.originalName || image.filename;
  const sub = document.createElement('div'); sub.className = 'image-sub'; sub.textContent = `${(image.size / 1024).toFixed(1)} KB · ${image.width || '—'} × ${image.height || '—'}`;
  meta.append(name, sub);
  const actions = document.createElement('div'); actions.className = 'image-actions';
  for (const [label, value] of [['复制链接', location.origin + image.url], ['Markdown', `![${image.originalName || 'image'}](${location.origin + image.url})`]]) {
    const button = document.createElement('button'); button.textContent = label;
    button.onclick = async () => { await navigator.clipboard.writeText(value); toast('已复制到剪贴板'); };
    actions.append(button);
  }
  if (state.admin) {
    const button = document.createElement('button'); button.className = 'danger'; button.textContent = '删除';
    button.onclick = async () => { if (!confirm('确定删除这张图片？')) return; try { await api(`/api/images/${encodeURIComponent(image.id)}`, { method: 'DELETE' }); toast('已删除图片'); await loadGallery(); await loadRecent(); } catch (error) { toast(error.message); } };
    actions.append(button);
  }
  card.append(img, meta, actions); return card;
}

function renderImages(element, images) {
  element.replaceChildren();
  if (!images.length) {
    const empty = document.createElement('div'); empty.className = 'empty';
    const strong = document.createElement('strong'); strong.textContent = '这里还没有图片';
    const note = document.createElement('span'); note.textContent = '上传第一张图片，它会出现在这里。';
    empty.append(strong, note); element.append(empty); return;
  }
  images.forEach(image => element.append(imageCard(image)));
}

async function loadRecent() {
  try { const data = await api('/api/images?limit=4'); renderImages($('#recent-grid'), data.images); } catch (error) { toast(error.message); }
}
async function loadGallery() {
  try {
    const data = await api(`/api/images?page=${state.page}&limit=20`);
    renderImages($('#gallery-grid'), data.images);
    state.totalPages = data.pagination.totalPages;
    $('#page-label').textContent = `第 ${state.page} 页 · 共 ${data.pagination.total} 张`;
    $('#prev-page').disabled = state.page <= 1; $('#next-page').disabled = state.page >= state.totalPages;
  } catch (error) { toast(error.message); }
}

async function refreshAuth() {
  try { await api('/api/auth/verify'); state.admin = true; } catch { state.admin = false; }
  $('#account-btn').innerHTML = state.admin ? '退出登录 <span aria-hidden="true">↗</span>' : '管理员登录 <span aria-hidden="true">↗</span>';
  $('#settings-locked').hidden = state.admin; $('#settings-content').hidden = !state.admin;
  updateUploadHint();
}

async function loadSettings() {
  await refreshAuth(); if (!state.admin) return;
  try {
    const [stats, config, keys, blacklist, appearance] = await Promise.all([api('/api/settings/stats'), api('/api/config/public'), api('/api/apikeys'), api('/api/blacklist?limit=100'), api('/api/settings/public')]);
    fillAppearance(appearance);
    $('#stat-total').textContent = stats.totalImages; $('#stat-public').textContent = stats.publicImages;
    $('#stat-private').textContent = stats.privateImages; $('#stat-size').textContent = `${(stats.activeSize / 1048576).toFixed(1)} MB`;
    $('#public-enabled').checked = config.enabled; $('#public-max').value = Math.round(config.maxFileSize / 1048576);
    const list = $('#key-list'); list.replaceChildren();
    keys.forEach(key => {
      const row = document.createElement('div'); row.className = 'key-row';
      const info = document.createElement('div'); const name = document.createElement('strong'); name.textContent = key.name;
      const secret = document.createElement('code'); secret.textContent = key.key; info.append(name, secret);
      const copy = document.createElement('button'); copy.textContent = '复制'; copy.onclick = async () => { await navigator.clipboard.writeText(key.key); toast('密钥已复制'); };
      const remove = document.createElement('button'); remove.textContent = '删除'; remove.onclick = async () => { if (!confirm(`删除密钥「${key.name}」？`)) return; try { await api(`/api/apikeys/${encodeURIComponent(key.id)}`, {method:'DELETE'}); await loadSettings(); } catch(error) { toast(error.message); } };
      row.append(info, copy, remove); list.append(row);
    });
    const blocked = $('#blacklist-list'); blocked.replaceChildren();
    blacklist.records.forEach(record => {
      const row = document.createElement('div'); row.className = 'key-row';
      const info = document.createElement('div'); const address = document.createElement('strong'); address.textContent = record.ip;
      const reason = document.createElement('code'); reason.textContent = record.reason || '无备注'; info.append(address, reason);
      const remove = document.createElement('button'); remove.textContent = '移除'; remove.onclick = async () => { try { await api(`/api/blacklist/${encodeURIComponent(record.id)}`, {method:'DELETE'}); await loadSettings(); toast('已移出黑名单'); } catch(error) { toast(error.message); } };
      row.append(info, remove); blocked.append(row);
    });
    if (blacklist.pagination.total > blacklist.records.length) { const note = document.createElement('p'); note.textContent = `已显示前 ${blacklist.records.length} 条；更多记录可通过 API 分页查看。`; blocked.append(note); }
  } catch (error) { toast(error.message); }
}

async function uploadOne(file, signal) {
  const endpoint = state.publicEnabled && !state.admin ? '/api/upload/public' : '/api/upload/private';
  for (let attempt = 0; attempt < 3; attempt++) {
    const body = new FormData(); body.append('file', file);
    try { return await api(endpoint, { method: 'POST', body, signal }); }
    catch (error) {
      if (signal.aborted || ![429, 502, 503, 504].includes(error.status) || attempt === 2) throw error;
      await new Promise(resolve => setTimeout(resolve, 300 * 2 ** attempt));
    }
  }
}

const failedFiles = [];
function showUploadResult(file, image, error) {
  const row = document.createElement('div'); row.className = `upload-result${error ? ' error' : ''}`;
  if (image) {
    const preview = document.createElement('img'); preview.src = image.url; preview.alt = ''; preview.loading = 'lazy'; row.append(preview);
  } else {
    const icon = document.createElement('span'); icon.className = 'upload-result-icon'; icon.textContent = '!'; row.append(icon);
    failedFiles.push(file);
  }
  const info = document.createElement('div'); const title = document.createElement('strong'); title.textContent = file.name;
  const detail = document.createElement('small'); detail.textContent = error ? `上传失败：${error.message}` : location.origin + image.url;
  info.append(title, detail); row.append(info);
  if (image) {
    const copy = document.createElement('button'); copy.textContent = '复制链接';
    copy.onclick = async () => { await navigator.clipboard.writeText(location.origin + image.url); toast('链接已复制'); };
    row.append(copy);
  }
  const list = $('#upload-results'); list.prepend(row);
  while (list.childElementCount > 24) list.lastElementChild.remove();
}

const uploadQueue = createUploadQueue({
  concurrency: 4,
  upload: uploadOne,
  onResult: showUploadResult,
  onProgress: progress => {
    $('#upload-progress').hidden = false;
    $('#upload-progress-title').textContent = `${progress.stopped ? '已取消' : progress.completed === progress.total ? '上传完成' : '正在上传'} ${progress.completed.toLocaleString()} / ${progress.total.toLocaleString()}`;
    $('#upload-progress-meta').textContent = `${(progress.completed - progress.failed - progress.cancelled).toLocaleString()} 成功 · ${progress.failed.toLocaleString()} 失败${progress.cancelled ? ` · ${progress.cancelled.toLocaleString()} 取消` : ''}`;
    $('#upload-progress-bar').max = Math.max(progress.total, 1);
    $('#upload-progress-bar').value = progress.completed;
    $('#cancel-upload').disabled = progress.stopped || (progress.active === 0 && progress.pending === 0);
  },
  onIdle: progress => {
    $('#retry-failed').hidden = failedFiles.length === 0;
    if (progress.stopped) toast('已取消剩余上传');
    else toast(progress.failed ? `上传结束，${progress.failed} 张失败` : `已上传 ${progress.total} 张图片`);
    loadRecent();
  },
});

function uploadFiles(files) {
  if (!files.length) return;
  const previous = uploadQueue.snapshot();
  if (previous.active === 0 && previous.pending === 0) {
    failedFiles.length = 0;
    $('#upload-results').replaceChildren();
  }
  $('#retry-failed').hidden = true;
  if (!uploadQueue.add(files)) toast('请等待当前批次取消完成');
}

document.querySelectorAll('[data-tab]').forEach(el => el.addEventListener('click', () => showTab(el.dataset.tab)));
$('#dropzone').addEventListener('click', () => $('#file-input').click());
$('#dropzone').addEventListener('keydown', event => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); $('#file-input').click(); } });
$('#file-input').addEventListener('change', event => { uploadFiles([...event.target.files]); event.target.value = ''; });
for (const type of ['dragenter','dragover']) $('#dropzone').addEventListener(type, event => { event.preventDefault(); $('#dropzone').classList.add('dragging'); });
for (const type of ['dragleave','drop']) $('#dropzone').addEventListener(type, event => { event.preventDefault(); $('#dropzone').classList.remove('dragging'); });
$('#dropzone').addEventListener('drop', event => uploadFiles([...event.dataTransfer.files]));
document.addEventListener('paste', event => { const files = [...(event.clipboardData?.files || [])]; if (files.length) uploadFiles(files); });
$('#cancel-upload').addEventListener('click', () => uploadQueue.cancel());
$('#retry-failed').addEventListener('click', () => uploadFiles(failedFiles.splice(0)));
$('#account-btn').addEventListener('click', async () => { if (state.admin) { await api('/api/auth/logout',{method:'POST'}); state.admin=false; await refreshAuth(); showTab('upload'); toast('已退出登录'); } else $('#login-dialog').showModal(); });
$('#login-from-settings').addEventListener('click', () => $('#login-dialog').showModal());
$('#close-login').addEventListener('click', () => $('#login-dialog').close());
$('#login-form').addEventListener('submit', async event => {
  event.preventDefault(); const form = new FormData(event.target); $('#login-error').textContent = '';
  try { await api('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(form))}); $('#login-dialog').close(); event.target.reset(); await refreshAuth(); toast('登录成功'); if ($('#settings-view').classList.contains('active')) loadSettings(); else loadRecent(); }
  catch (error) { $('#login-error').textContent = error.message; }
});
$('#refresh-gallery').addEventListener('click',loadGallery);
$('#prev-page').addEventListener('click',()=>{if(state.page>1){state.page--;loadGallery();}});
$('#next-page').addEventListener('click',()=>{if(state.page<state.totalPages){state.page++;loadGallery();}});
$('#save-public').addEventListener('click',async()=>{
  const size = Number($('#public-max').value); if (!Number.isInteger(size)||size<1||size>100) { toast('文件上限需为 1–100 MB'); return; }
  try { await api('/api/config/public',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:$('#public-enabled').checked,maxFileSize:size*1048576,allowedFormats:['jpg','jpeg','png','gif','webp'],rateLimit:10})}); state.publicEnabled=$('#public-enabled').checked; state.publicConfig = { enabled: state.publicEnabled, maxFileSize: size * 1048576, allowedFormats: ['jpg','jpeg','png','gif','webp'] }; updateUploadHint(); toast('设置已保存'); } catch(error){toast(error.message);}
});
$('#background-url').addEventListener('input', previewBackground);
$('#background-blur').addEventListener('input', previewBackground);
$('#upload-background').addEventListener('click', () => $('#background-file').click());
$('#background-file').addEventListener('change', async event => {
  const file = event.target.files[0];
  event.target.value = '';
  if (!file) return;
  const status = $('#background-upload-status');
  status.textContent = '正在上传…';
  const body = new FormData(); body.append('file', file);
  try {
    const image = await api('/api/upload/private', { method: 'POST', body });
    $('#background-url').value = image.url;
    previewBackground();
    status.textContent = '上传完成，请保存外观';
  } catch (error) { status.textContent = '上传失败'; toast(error.message); }
});
$('#appearance-form').addEventListener('submit', async event => {
  event.preventDefault();
  try { await saveAppearance($('#background-url').value.trim(), Number($('#background-blur').value)); }
  catch (error) { toast(error.message); }
});
$('#reset-background').addEventListener('click', async () => {
  try { await saveAppearance('', 0); $('#background-upload-status').textContent = '支持 JPG、PNG、GIF、WebP'; }
  catch (error) { toast(error.message); }
});
$('#create-key').addEventListener('click',async()=>{const name=$('#key-name').value.trim();if(!name)return;try{await api('/api/apikeys',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name})});$('#key-name').value='';loadSettings();toast('密钥已创建');}catch(error){toast(error.message);}});
$('#password-form').addEventListener('submit', async event => {
  event.preventDefault();
  const body = Object.fromEntries(new FormData(event.target));
  try {
    await api('/api/admin/password', { method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body) });
    event.target.reset(); state.admin = false; await refreshAuth(); showTab('upload'); toast('密码已修改，请重新登录');
  } catch (error) { toast(error.message); }
});
$('#blacklist-form').addEventListener('submit', async event => {
  event.preventDefault();
  try { await api('/api/blacklist', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(Object.fromEntries(new FormData(event.target)))}); event.target.reset(); await loadSettings(); toast('地址已加入黑名单'); }
  catch(error) { toast(error.message); }
});

(async()=>{
  try { const settings=await api('/api/settings/public'); if(settings.appName){$('#brand-name').textContent=settings.appName;document.title=`${settings.appName} · 图片存储`;} applyBackground(settings.backgroundUrl || '', Number(settings.backgroundBlur) || 0); } catch {}
  try { const config=await api('/api/config/public');state.publicEnabled=config.enabled;state.publicConfig=config;updateUploadHint(); } catch {}
  await refreshAuth(); await loadRecent();
  const tab=location.pathname.slice(1); if(['gallery','settings'].includes(tab))showTab(tab);
  else window.scrollTo(0, 0);
})();
