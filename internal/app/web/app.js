import { createUploadQueue } from './upload-queue.mjs';

const $ = (selector) => document.querySelector(selector);
const state = { admin: false, page: 1, totalPages: 1, recyclePage: 1, recycleTotalPages: 1, publicEnabled: false, publicConfig: null, safetyProviders: {} };
const selectedImages = new Set();

function iconMarkup(name) {
  return '<svg class="icon" aria-hidden="true"><use href="/icons.svg#' + name + '"></use></svg>';
}
function applyTheme(theme) {
  document.documentElement.dataset.theme = theme;
  $('#theme-toggle').innerHTML = iconMarkup(theme === 'dark' ? 'sun' : 'moon');
  $('meta[name="theme-color"]').content = theme === 'dark' ? '#0c0e12' : '#f4f5f7';
  $('#theme-toggle').setAttribute('aria-label', theme === 'dark' ? '切换浅色模式' : '切换深色模式');
}
applyTheme(document.documentElement.dataset.theme);

function showAnnouncement(settings) {
  const announcement = settings.announcement;
  $('#announcement-banner').hidden = true;
  if (!announcement?.enabled || !announcement.content) return;
  if (announcement.displayType === 'banner') {
    $('#announcement-banner').textContent = announcement.content;
    $('#announcement-banner').hidden = false;
  } else if (sessionStorage.getItem('tanoimg-announcement') !== announcement.content) {
    $('#announcement-modal-content').textContent = announcement.content;
    $('#announcement-dialog').showModal();
    sessionStorage.setItem('tanoimg-announcement', announcement.content);
  }
}
function applyLogo(url) {
  const mark = $('.brand-mark');
  mark.style.backgroundImage = imageCSS(url);
  mark.innerHTML = url ? '' : iconMarkup('aperture');
}
function showModerationProvider() {
  const provider = $('#moderation-provider').value;
  const config = state.safetyProviders[provider] || {};
  $('#moderation-url').value = config.apiUrl || '';
  $('#moderation-upload-url').value = config.uploadUrl || '';
  $('#moderation-key').value = config.apiKey || '';
  $('#moderation-threshold').value = config.threshold ?? (provider === 'nsfw_detector' ? 0.8 : 0.5);
  $('#moderation-upload-url').parentElement.hidden = provider !== 'elysiatools';
  $('#moderation-threshold').parentElement.hidden = provider === 'elysiatools';
}
function showNotificationMethod() {
  const method = $('#notification-method').value;
  for (const option of ['webhook','telegram','email','serverchan']) $(`#notification-${option}`).hidden = option !== method;
}
function conversionChoice(config) {
  return config.convertToWebp ? 'webp' : config.convertToPng ? 'png' : config.convertToJpg ? 'jpg' : 'none';
}
function processingValues(prefix) {
  const quality = Number($(`#${prefix}-quality`).value);
  if (!Number.isInteger(quality) || quality < 1 || quality > 100) throw new Error('压缩质量需为 1–100');
  const target = $(`#${prefix}-convert`).value;
  return { enableCompression: $(`#${prefix}-compression`).checked, compressionQuality: quality, convertToWebp: target === 'webp', convertToPng: target === 'png', convertToJpg: target === 'jpg' };
}
function fillNotification(config) {
  $('#notification-enabled').checked = !!config.enabled; $('#notification-method').value = config.method || 'telegram'; showNotificationMethod();
  $('#notify-login').checked = !!config.types?.login; $('#notify-upload').checked = !!config.types?.upload; $('#notify-nsfw').checked = !!config.types?.nsfw;
  $('#webhook-url').value = config.webhook?.url || ''; $('#webhook-method').value = config.webhook?.method || 'POST'; $('#webhook-content-type').value = config.webhook?.contentType || 'application/json';
  $('#webhook-headers').value = JSON.stringify(config.webhook?.headers || {}, null, 2); $('#webhook-template').value = config.webhook?.bodyTemplate || '';
  $('#telegram-token').value = config.telegram?.token || ''; $('#telegram-chat-id').value = config.telegram?.chatId || '';
  $('#email-service').value = config.email?.service || 'gmail'; $('#email-user').value = config.email?.user || ''; $('#email-pass').value = config.email?.pass || ''; $('#email-to').value = config.email?.to || '';
  $('#serverchan-key').value = config.serverchan?.sendKey || '';
}
function notificationBody() {
  let headers;
  try { headers = JSON.parse($('#webhook-headers').value || '{}'); } catch { throw new Error('请求头必须是 JSON 对象'); }
  if (!headers || Array.isArray(headers) || typeof headers !== 'object') throw new Error('请求头必须是 JSON 对象');
  return {enabled:$('#notification-enabled').checked,method:$('#notification-method').value,types:{login:$('#notify-login').checked,upload:$('#notify-upload').checked,nsfw:$('#notify-nsfw').checked},webhook:{url:$('#webhook-url').value.trim(),method:$('#webhook-method').value,contentType:$('#webhook-content-type').value.trim(),headers,bodyTemplate:$('#webhook-template').value},telegram:{token:$('#telegram-token').value.trim(),chatId:$('#telegram-chat-id').value.trim()},email:{service:$('#email-service').value,user:$('#email-user').value.trim(),pass:$('#email-pass').value,to:$('#email-to').value.trim()},serverchan:{sendKey:$('#serverchan-key').value.trim()}};
}

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
  document.querySelectorAll('.nav-link').forEach(el => el.classList.toggle('active', el.dataset.tab === (tab === 'recycle' ? 'gallery' : tab)));
  if (tab === 'gallery') loadGallery();
  if (tab === 'recycle') loadRecycle();
  if (tab === 'stats') loadStatsView();
  if (tab === 'settings') loadSettings();
  history.replaceState(null, '', tab === 'upload' ? '/' : `/${tab}`);
  window.scrollTo(0, 0);
}

function imageCard(image, selectable = false) {
  const card = document.createElement('article'); card.className = 'image-card';
  const img = document.createElement('img'); img.src = image.url; img.alt = image.originalName || '图片'; img.loading = 'lazy';
  img.addEventListener('click', () => { $('#image-detail').src = image.url; $('#image-detail').alt = image.originalName || '图片'; $('#image-detail-meta').textContent = `${image.originalName || image.filename} · ${(image.size / 1024).toFixed(1)} KB · ${image.width || '—'} × ${image.height || '—'}`; $('#image-dialog').showModal(); });
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
    if (selectable) {
      const label = document.createElement('label'); label.className = 'image-select';
      const checkbox = document.createElement('input'); checkbox.type = 'checkbox'; checkbox.checked = selectedImages.has(image.id); checkbox.setAttribute('aria-label', `选择 ${image.originalName || image.filename}`);
      checkbox.onchange = () => { if (checkbox.checked) selectedImages.add(image.id); else selectedImages.delete(image.id); updateSelection(); };
      label.append(checkbox, document.createTextNode('选择')); card.prepend(label);
    }
    const button = document.createElement('button'); button.className = 'danger'; button.textContent = '删除';
    button.onclick = async () => { if (!confirm('确定将这张图片移入回收站？')) return; try { await api(`/api/images/${encodeURIComponent(image.id)}`, { method: 'DELETE' }); toast('已移入回收站，可从图库右上角恢复'); await loadGallery(); await loadRecent(); } catch (error) { toast(error.message); } };
    actions.append(button);
  }
  card.append(img, meta, actions); return card;
}

function renderImages(element, images) {
  element.replaceChildren();
  element.classList.toggle('is-empty', !images.length);
  if (!images.length) {
    const empty = document.createElement('div'); empty.className = 'empty';
    const strong = document.createElement('strong'); strong.textContent = '这里还没有图片';
    const note = document.createElement('span'); note.textContent = '上传第一张图片，它会出现在这里。';
    empty.append(strong, note); element.append(empty); return;
  }
  images.forEach(image => element.append(imageCard(image, element.id === 'gallery-grid')));
}

function updateSelection() {
  $('#selected-count').textContent = `已选择 ${selectedImages.size} 张`;
  $('#delete-selected').disabled = selectedImages.size === 0;
}

async function loadRecent() {
  try { const data = await api('/api/images?limit=4'); renderImages($('#recent-grid'), data.images); } catch (error) { toast(error.message); }
}
async function loadGallery() {
  try {
    selectedImages.clear(); updateSelection();
    const data = await api(`/api/images?page=${state.page}&limit=20`);
    renderImages($('#gallery-grid'), data.images);
    state.totalPages = data.pagination.totalPages;
    $('#page-label').textContent = `第 ${state.page} 页 · 共 ${data.pagination.total} 张`;
    $('#prev-page').disabled = state.page <= 1; $('#next-page').disabled = state.page >= state.totalPages;
  } catch (error) { toast(error.message); }
}

function recycleCard(image) {
  const card = document.createElement('article'); card.className = 'image-card';
  const img = document.createElement('img'); img.src = image.url; img.alt = image.originalName || '已删除的图片'; img.loading = 'lazy';
  img.onclick = () => { $('#image-detail').src = image.url; $('#image-detail').alt = img.alt; $('#image-detail-meta').textContent = image.originalName || image.filename; $('#image-dialog').showModal(); };
  const meta = document.createElement('div'); meta.className = 'image-meta';
  const name = document.createElement('div'); name.className = 'image-name'; name.textContent = image.originalName || image.filename;
  const detail = document.createElement('div'); detail.className = 'image-sub'; detail.textContent = `${(image.size / 1024).toFixed(1)} KB · ${image.width || '—'} × ${image.height || '—'}`;
  meta.append(name, detail);
  const actions = document.createElement('div'); actions.className = 'image-actions';
  const restore = document.createElement('button'); restore.className = 'outline-button'; restore.textContent = '恢复图片';
  restore.onclick = async () => {
    restore.disabled = true;
    try { await api(`/api/images/${encodeURIComponent(image.id)}/restore`, {method:'PUT'}); toast('图片已恢复到图库'); await loadRecycle(); }
    catch(error) { toast(error.message); restore.disabled = false; }
  };
  actions.append(restore); card.append(img, meta, actions); return card;
}

async function loadRecycle() {
  const grid = $('#recycle-grid');
  grid.classList.add('is-empty');
  const loading = document.createElement('div'); loading.className = 'empty'; loading.textContent = '正在加载回收站…';
  grid.replaceChildren(loading);
  $('#recycle-page-label').textContent = '正在加载…';
  $('#recycle-prev').disabled = true; $('#recycle-next').disabled = true;
  await refreshAuth();
  if (!state.admin) return;
  try {
    const data = await api(`/api/images/deleted?page=${state.recyclePage}&limit=20`);
    state.recycleTotalPages = Math.max(1, data.pagination.totalPages);
    if (state.recyclePage > state.recycleTotalPages) { state.recyclePage = state.recycleTotalPages; return loadRecycle(); }
    grid.replaceChildren();
    grid.classList.toggle('is-empty', !data.images.length);
    if (!data.images.length) {
      const empty = document.createElement('div'); empty.className = 'empty';
      const title = document.createElement('strong'); title.textContent = '回收站为空';
      const hint = document.createElement('span'); hint.textContent = '删除的图片会显示在这里，清空前可以恢复。';
      empty.append(title, hint); grid.append(empty);
    } else data.images.forEach(image => grid.append(recycleCard(image)));
    $('#recycle-page-label').textContent = `第 ${state.recyclePage} 页 · 共 ${data.pagination.total} 张`;
    $('#recycle-prev').disabled = state.recyclePage <= 1;
    $('#recycle-next').disabled = state.recyclePage >= state.recycleTotalPages;
  } catch(error) {
    const failed = document.createElement('div'); failed.className = 'empty'; failed.textContent = '回收站加载失败，请稍后重试。';
    grid.replaceChildren(failed);
    $('#recycle-page-label').textContent = '加载失败';
    toast(error.message);
  }
}

async function loadStatsView() {
  await refreshAuth();
  if (!state.admin) return;
  try {
    const data = await api('/api/settings/stats');
    const numbers = {total:data.totalImages,public:data.publicImages,private:data.privateImages,deleted:data.deletedImagesCount,moderated:data.moderatedImagesCount,nsfw:data.nsfwImagesCount};
    for (const [key, value] of Object.entries(numbers)) $(`#report-${key}`).textContent = Number(value).toLocaleString('zh-CN');
    $('#report-active-size').textContent = `${(data.activeSize / 1048576).toFixed(1)} MB`;
    $('#report-deleted-size').textContent = `${(data.deletedSize / 1048576).toFixed(1)} MB`;
    $('#report-nsfw-rate').textContent = `${data.nsfwRate.toFixed(1)}%`;
  } catch(error) { toast(error.message); }
}

async function refreshAuth() {
  try { const auth = await api('/api/auth/verify'); state.admin = true; $('#admin-username').value = auth.user.username; } catch { state.admin = false; }
  $('#account-btn').textContent = state.admin ? '退出登录' : '管理员登录';
  $('#settings-locked').hidden = state.admin; $('#settings-content').hidden = !state.admin;
  $('#stats-locked').hidden = state.admin; $('#stats-content').hidden = !state.admin;
  $('#recycle-locked').hidden = state.admin; $('#recycle-content').hidden = !state.admin;
  $('#open-recycle').hidden = !state.admin;
  $('#url-upload-panel').hidden = !state.admin;
  $('#gallery-selection').hidden = !state.admin;
  updateUploadHint();
}

async function loadSettings() {
  await refreshAuth(); if (!state.admin) return;
  try {
    const [stats, config, keys, blacklist, appearance, privateConfig, nsfw, notification] = await Promise.all([api('/api/settings/stats'), api('/api/config/public'), api('/api/apikeys'), api('/api/blacklist?limit=100'), api('/api/settings'), api('/api/config/private'), api('/api/images/nsfw?limit=20'), api('/api/notification')]);
    fillAppearance(appearance);
    $('#site-name').value = appearance.appName || 'TanoImg'; $('#site-logo').value = appearance.appLogo || ''; $('#site-url').value = appearance.siteUrl || '';
    $('#announcement-enabled').checked = !!appearance.announcement?.enabled; $('#announcement-content').value = appearance.announcement?.content || ''; $('#announcement-type').value = appearance.announcement?.displayType || 'modal';
    $('#private-max').value = Math.round(privateConfig.maxFileSize / 1048576); $('#private-homepage').checked = !!privateConfig.showOnHomepage;
    $('#private-compression').checked = !!privateConfig.enableCompression; $('#private-quality').value = privateConfig.compressionQuality || 80; $('#private-convert').value = conversionChoice(privateConfig);
    $('#deleted-count').textContent = `待清理 ${appearance.deletedImagesCount} 张`;
    $('#stat-total').textContent = stats.totalImages; $('#stat-public').textContent = stats.publicImages;
    $('#stat-private').textContent = stats.privateImages; $('#stat-size').textContent = `${(stats.activeSize / 1048576).toFixed(1)} MB`;
    $('#public-enabled').checked = config.enabled; $('#public-max').value = Math.round(config.maxFileSize / 1048576);
    $('#public-formats').value = (config.allowedFormats || []).join(','); $('#public-rate').value = config.rateLimit || 10; $('#public-concurrent').checked = !!config.allowConcurrent;
    $('#public-compression').checked = !!config.enableCompression; $('#public-quality').value = config.compressionQuality || 80; $('#public-convert').value = conversionChoice(config);
    $('#moderation-enabled').checked = !!config.contentSafety?.enabled; $('#moderation-provider').value = config.contentSafety?.provider || 'elysiatools'; $('#moderation-blacklist').checked = !!config.contentSafety?.autoBlacklistIp;
    state.safetyProviders = structuredClone(config.contentSafety?.providers || {}); showModerationProvider();
    $('#moderation-stats').textContent = `${stats.moderatedImagesCount || 0} 张已检测 · ${stats.nsfwImagesCount || 0} 张违规`;
    fillNotification(notification);
    const list = $('#key-list'); list.replaceChildren();
    keys.forEach(key => {
      const row = document.createElement('div'); row.className = 'key-row';
      const info = document.createElement('div'); const name = document.createElement('strong'); name.textContent = key.name;
      const secret = document.createElement('code'); secret.textContent = key.key; info.append(name, secret);
      const copy = document.createElement('button'); copy.textContent = '复制'; copy.onclick = async () => { await navigator.clipboard.writeText(key.key); toast('密钥已复制'); };
      const rename = document.createElement('button'); rename.textContent = '重命名'; rename.onclick = async () => { const name = prompt('新的密钥名称', key.name)?.trim(); if (!name) return; try { await api(`/api/apikeys/${encodeURIComponent(key.id)}`, {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({name})}); await loadSettings(); } catch(error){toast(error.message);} };
      const toggle = document.createElement('button'); toggle.textContent = key.enabled ? '禁用' : '启用'; toggle.onclick = async () => { try { await api(`/api/apikeys/${encodeURIComponent(key.id)}`, {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({enabled:!key.enabled})}); await loadSettings(); } catch(error){toast(error.message);} };
      const regenerate = document.createElement('button'); regenerate.textContent = '重置'; regenerate.onclick = async () => { if (!confirm(`重新生成「${key.name}」的密钥？旧密钥会立即失效。`)) return; try { await api(`/api/apikeys/${encodeURIComponent(key.id)}`, {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({regenerate:true})}); await loadSettings(); } catch(error){toast(error.message);} };
      const remove = document.createElement('button'); remove.textContent = '删除'; remove.onclick = async () => { if (!confirm(`删除密钥「${key.name}」？`)) return; try { await api(`/api/apikeys/${encodeURIComponent(key.id)}`, {method:'DELETE'}); await loadSettings(); } catch(error) { toast(error.message); } };
      const actions = document.createElement('div'); actions.className = 'row-actions'; actions.append(copy, rename, toggle, regenerate, remove); row.append(info, actions); list.append(row);
    });
    const unsafe = $('#nsfw-list'); unsafe.replaceChildren();
    if (!nsfw.images.length) { const note = document.createElement('p'); note.textContent = '没有违规图片。'; unsafe.append(note); }
    nsfw.images.forEach(image => { const row = document.createElement('div'); row.className = 'key-row'; const name = document.createElement('strong'); name.textContent = image.originalName || image.filename; const preview = document.createElement('button'); preview.textContent = '预览'; preview.onclick = () => { $('#image-detail').src = image.url; $('#image-detail-meta').textContent = image.originalName || image.filename; $('#image-dialog').showModal(); }; const restore = document.createElement('button'); restore.textContent = '取消违规'; restore.onclick = async () => { try { await api(`/api/images/${encodeURIComponent(image.id)}/unmark-nsfw`, {method:'PUT'}); await loadSettings(); await loadGallery(); } catch(error){toast(error.message);} }; const actions = document.createElement('div'); actions.className = 'row-actions'; actions.append(preview, restore); row.append(name, actions); unsafe.append(row); });
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
function showUploadResult(file, image, error, retryable = true) {
  const row = document.createElement('div'); row.className = `upload-result${error ? ' error' : ''}`;
  if (image) {
    const preview = document.createElement('img'); preview.src = image.url; preview.alt = ''; preview.loading = 'lazy'; row.append(preview);
  } else {
    const icon = document.createElement('span'); icon.className = 'upload-result-icon'; icon.innerHTML = iconMarkup('triangle-alert'); row.append(icon);
    if (retryable) failedFiles.push(file);
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
$('#theme-toggle').addEventListener('click', () => { const next = document.documentElement.dataset.theme === 'dark' ? 'light' : 'dark'; localStorage.setItem('tanoimg-theme', next); applyTheme(next); });
$('#close-image').addEventListener('click', () => $('#image-dialog').close());
$('#close-announcement').addEventListener('click', () => $('#announcement-dialog').close());
$('#delete-selected').addEventListener('click', async () => { const ids = [...selectedImages]; if (!ids.length || !confirm(`将选中的 ${ids.length} 张图片移入回收站？`)) return; try { const result = await api('/api/images/batch', {method:'DELETE', headers:{'Content-Type':'application/json'}, body:JSON.stringify({ids})}); toast(`已移入回收站 ${result.deletedCount} 张，可从图库右上角恢复`); await loadGallery(); await loadRecent(); } catch(error){toast(error.message);} });
$('#url-upload-form').addEventListener('submit', async event => { event.preventDefault(); if (!state.admin) return; const urls = [...new Set($('#url-list').value.split(/\r?\n/).map(x=>x.trim()).filter(Boolean))]; if (!urls.length) return; const button = event.submitter; button.disabled = true; let completed = 0, failed = 0; try { for (let start = 0; start < urls.length; start += 1000) { const chunk = urls.slice(start, start + 1000); const result = await api('/api/upload/url', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify({url:chunk})}); completed += result.successCount; failed += result.errorCount; $('#url-upload-status').textContent = `${Math.min(start+chunk.length, urls.length)} / ${urls.length} · ${completed} 成功 · ${failed} 失败`; result.results.forEach(item => showUploadResult({name:item.url}, item.data, null)); result.errors.forEach(item => showUploadResult({name:item.url}, null, new Error(item.error), false)); } $('#url-list').value = ''; await loadRecent(); toast(`URL 上传完成：${completed} 成功，${failed} 失败`); } catch(error){toast(error.message);} finally {button.disabled = false;} });
$('#account-btn').addEventListener('click', async () => { if (state.admin) { await api('/api/auth/logout',{method:'POST'}); state.admin=false; await refreshAuth(); showTab('upload'); toast('已退出登录'); } else $('#login-dialog').showModal(); });
$('#login-from-settings').addEventListener('click', () => $('#login-dialog').showModal());
$('#login-from-recycle').addEventListener('click', () => $('#login-dialog').showModal());
$('#login-from-stats').addEventListener('click', () => $('#login-dialog').showModal());
$('#refresh-stats').addEventListener('click', loadStatsView);
$('#check-version').addEventListener('click', async () => {
  const button = $('#check-version'); button.disabled = true;
  $('#version-status').textContent = '正在检查…';
  try {
    const data = await api('/api/version/check');
    $('#version-status').textContent = data.error ? `当前 ${data.currentVersion} · ${data.error}` : data.hasUpdate ? `当前 ${data.currentVersion} · 新版 ${data.latestVersion}` : `当前 ${data.currentVersion} · 已是最新版本`;
  } catch(error) { $('#version-status').textContent = error.message; }
  finally { button.disabled = false; }
});
$('#close-login').addEventListener('click', () => $('#login-dialog').close());
$('#login-form').addEventListener('submit', async event => {
  event.preventDefault(); const form = new FormData(event.target); $('#login-error').textContent = '';
  try { await api('/api/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(Object.fromEntries(form))}); $('#login-dialog').close(); event.target.reset(); await refreshAuth(); toast('登录成功'); if ($('#settings-view').classList.contains('active')) loadSettings(); else if ($('#stats-view').classList.contains('active')) loadStatsView(); else if ($('#recycle-view').classList.contains('active')) loadRecycle(); else loadRecent(); }
  catch (error) { $('#login-error').textContent = error.message; }
});
$('#refresh-gallery').addEventListener('click',loadGallery);
$('#open-recycle').addEventListener('click', () => { state.recyclePage = 1; showTab('recycle'); });
$('#open-recycle-settings').addEventListener('click', () => { state.recyclePage = 1; showTab('recycle'); });
$('#back-to-gallery').addEventListener('click', () => showTab('gallery'));
$('#recycle-prev').addEventListener('click', () => { if (state.recyclePage > 1) { state.recyclePage--; loadRecycle(); } });
$('#recycle-next').addEventListener('click', () => { if (state.recyclePage < state.recycleTotalPages) { state.recyclePage++; loadRecycle(); } });
$('#prev-page').addEventListener('click',()=>{if(state.page>1){state.page--;loadGallery();}});
$('#next-page').addEventListener('click',()=>{if(state.page<state.totalPages){state.page++;loadGallery();}});
$('#save-public').addEventListener('click',async()=>{
  const size = Number($('#public-max').value); if (!Number.isInteger(size)||size<1||size>100) { toast('文件上限需为 1–100 MB'); return; }
  const formats = [...new Set($('#public-formats').value.split(',').map(x=>x.trim().toLowerCase()).filter(Boolean))];
  const rateLimit = Number($('#public-rate').value);
  if (!formats.length || !Number.isInteger(rateLimit) || rateLimit<1 || rateLimit>1000) { toast('请输入有效的格式和上传频率'); return; }
  try { await api('/api/config/public',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({enabled:$('#public-enabled').checked,maxFileSize:size*1048576,allowedFormats:formats,rateLimit,allowConcurrent:$('#public-concurrent').checked})}); state.publicEnabled=$('#public-enabled').checked; state.publicConfig = { enabled: state.publicEnabled, maxFileSize: size * 1048576, allowedFormats: formats }; updateUploadHint(); toast('公开上传设置已保存'); } catch(error){toast(error.message);}
});
$('#public-processing-form').addEventListener('submit', async event => {
  event.preventDefault();
  try { await api('/api/config/public', {method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(processingValues('public'))}); toast('公开上传图片处理已保存'); }
  catch(error) { toast(error.message); }
});
$('#moderation-provider').addEventListener('change', showModerationProvider);
$('#moderation-form').addEventListener('submit', async event => {
  event.preventDefault(); const provider = $('#moderation-provider').value;
  const threshold = Number($('#moderation-threshold').value);
  if (!Number.isFinite(threshold) || threshold<0 || threshold>1) {toast('审核阈值需在 0–1 之间');return;}
  state.safetyProviders[provider] = {apiUrl:$('#moderation-url').value.trim(),uploadUrl:$('#moderation-upload-url').value.trim(),apiKey:$('#moderation-key').value.trim(),threshold};
  const contentSafety = {enabled:$('#moderation-enabled').checked,provider,autoBlacklistIp:$('#moderation-blacklist').checked,providers:state.safetyProviders};
  try { await api('/api/config/public',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({contentSafety})}); toast('审核设置已保存'); } catch(error){toast(error.message);}
});
$('#notification-method').addEventListener('change', showNotificationMethod);
$('#notification-form').addEventListener('submit', async event => { event.preventDefault(); try { const saved = await api('/api/notification', {method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(notificationBody())}); fillNotification(saved); toast('通知设置已保存'); } catch(error){toast(error.message);} });
$('#test-notification').addEventListener('click', async () => { const button = $('#test-notification'); button.disabled = true; try { await api('/api/notification/test', {method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(notificationBody())}); toast('测试通知已发送'); } catch(error){toast(error.message);} finally {button.disabled = false;} });
$('#site-form').addEventListener('submit', async event => { event.preventDefault(); const body = {appName:$('#site-name').value.trim(), appLogo:$('#site-logo').value.trim(), siteUrl:$('#site-url').value.trim(), announcement:{enabled:$('#announcement-enabled').checked, content:$('#announcement-content').value, displayType:$('#announcement-type').value}}; try { const saved = await api('/api/settings', {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body)}); $('#brand-name').textContent = saved.appName; document.title = `${saved.appName} · 图片存储`; applyLogo(saved.appLogo); showAnnouncement(saved); toast('站点信息已保存'); } catch(error){toast(error.message);} });
$('#private-form').addEventListener('submit', async event => { event.preventDefault(); const size = Number($('#private-max').value); if (!Number.isInteger(size) || size<1 || size>200) {toast('文件上限需为 1–200 MB');return;} try { await api('/api/config/private', {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({maxFileSize:size*1048576,showOnHomepage:$('#private-homepage').checked,...processingValues('private')})}); toast('私有上传设置已保存'); await loadGallery(); } catch(error){toast(error.message);} });
$('#username-form').addEventListener('submit', async event => { event.preventDefault(); try { await api('/api/admin/username', {method:'PUT', headers:{'Content-Type':'application/json'}, body:JSON.stringify({username:$('#admin-username').value.trim()})}); toast('用户名已修改'); } catch(error){toast(error.message);} });
$('#hard-delete').addEventListener('click', async () => { if (!confirm('永久删除回收站中的图片文件？此操作无法撤销。')) return; try { const result = await api('/api/settings/hard-delete', {method:'POST'}); toast(`永久删除 ${result.deletedCount} 张图片`); await loadSettings(); } catch(error){toast(error.message);} });
$('#clear-nsfw').addEventListener('click', async () => { if (!confirm('永久删除所有违规图片文件？此操作无法撤销。')) return; try { const result = await api('/api/images/nsfw-clear', {method:'POST'}); toast(`清空 ${result.deletedCount} 张违规图片`); await loadSettings(); } catch(error){toast(error.message);} });
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
  try { const settings=await api('/api/settings/public'); if(settings.appName){$('#brand-name').textContent=settings.appName;document.title=`${settings.appName} · 图片存储`;} applyBackground(settings.backgroundUrl || '', Number(settings.backgroundBlur) || 0); applyLogo(settings.appLogo || ''); showAnnouncement(settings); } catch {}
  try { const config=await api('/api/config/public');state.publicEnabled=config.enabled;state.publicConfig=config;updateUploadHint(); } catch {}
  await refreshAuth(); await loadRecent();
  const tab=location.pathname.slice(1); if(['gallery','recycle','stats','api','settings'].includes(tab))showTab(tab);
  else window.scrollTo(0, 0);
})();
