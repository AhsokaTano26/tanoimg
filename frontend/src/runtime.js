import { reactive } from 'vue';

export const session = reactive({ admin: false, username: '', ready: false });
export const site = reactive({ appName: 'TanoImg', appLogo: '', backgroundUrl: '', backgroundBlur: 0, announcement: null });
export const notice = reactive({ message: '', visible: false });
let toastTimer;
export function toast(message) {
  notice.message = message;
  notice.visible = true;
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => { notice.visible = false; }, 3200);
}
let onUnauthorized = () => {};
export function setUnauthorizedHandler(handler) { onUnauthorized = handler; }
export async function request(url, options = {}) {
  const response = await fetch(url, { credentials: 'same-origin', ...options });
  let body;
  try { body = await response.json(); } catch { throw new Error('服务器返回了无效响应'); }
  if (!response.ok || !body.success) {
    if (response.status === 401 && !url.startsWith('/api/auth/')) {
      session.admin = false;
      session.username = '';
      onUnauthorized();
    }
    const error = new Error(body.message || `请求失败 (${response.status})`);
    error.status = response.status;
    throw error;
  }
  return body.data;
}
export async function verifySession() {
  try {
    const auth = await request('/api/auth/verify');
    session.admin = true;
    session.username = auth.user.username;
  } catch (error) {
    session.admin = false;
    session.username = '';
    if (error.status !== 401) throw error;
  } finally { session.ready = true; }
  return session.admin;
}
export function updateSite(settings) {
  Object.assign(site, settings);
  document.title = `${site.appName} · 图片存储`;
}
export async function loadSite() {
  try { updateSite(await request('/api/settings/public')); } catch (error) { toast(error.message); }
}
