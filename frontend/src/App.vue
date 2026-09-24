<script setup>
import { computed, ref, watch, onMounted } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import Icon from './components/Icon.vue';
import { session, site, notice, request, loadSite, toast } from './runtime.js';
const route = useRoute();
const router = useRouter();
const adminArea = computed(() => !!route.meta.requiresAuth && session.admin);
const theme = ref(document.documentElement.dataset.theme || 'dark');
const loggingOut = ref(false);
const announcement = ref(null);
const imageDialog = ref(null);
const announcementDialog = ref(null);
const nav = [
  { path: '/admin/gallery', name: '图片管理', icon: 'images' },
  { path: '/admin/upload', name: '上传图片', icon: 'upload' },
  { path: '/admin/recycle', name: '回收站', icon: 'trash-2' },
  { path: '/admin/stats', name: '存储统计', icon: 'chart-no-axes-combined' },
  { path: '/admin/api', name: 'API 指南', icon: 'code-xml' },
  { path: '/admin/settings', name: '管理设置', icon: 'sliders-horizontal' },
];
function toggleTheme() {
  theme.value = theme.value === 'dark' ? 'light' : 'dark';
  document.documentElement.dataset.theme = theme.value;
  document.querySelector('meta[name="theme-color"]').content = theme.value === 'dark' ? '#0c0e12' : '#f4f5f7';
  try { localStorage.setItem('tanoimg-theme', theme.value); } catch {}
}
async function logout() {
  loggingOut.value = true;
  try {
    // Let upload-page navigation guards resolve before ending the session.
    const failure = await router.push('/');
    if (failure) return;
    await request('/api/auth/logout', { method: 'POST' });
    session.admin = false; session.username = '';
    toast('已退出登录');
  } catch (error) { toast(error.message); }
  finally { loggingOut.value = false; }
}
watch(() => site.announcement, value => {
  announcement.value = value?.enabled ? value : null;
  if (value?.enabled && value.content && value.displayType !== 'banner') {
    try {
      if (sessionStorage.getItem('tanoimg-announcement') === value.content) return;
      sessionStorage.setItem('tanoimg-announcement', value.content);
    } catch {}
    announcementDialog.value?.showModal();
  }
});
onMounted(loadSite);
</script>
<template>
  <div :class="adminArea ? 'admin-shell' : 'public-shell'">
    <div id="site-background" class="site-background" aria-hidden="true" :style="{ backgroundImage: site.backgroundUrl ? 'url(' + JSON.stringify(site.backgroundUrl) + ')' : 'none', '--background-blur': site.backgroundBlur + 'px' }"></div>
    <header :class="adminArea ? 'topbar' : 'public-header'">
      <RouterLink class="brand" to="/" :aria-label="site.appName + ' 首页'">
        <span class="brand-mark" :style="site.appLogo ? { backgroundImage: 'url(' + JSON.stringify(site.appLogo) + ')' } : {}"><Icon v-if="!site.appLogo" name="aperture" /></span><span>{{ site.appName }}</span>
      </RouterLink>
      <template v-if="adminArea">
        <div class="nav-caption">ADMIN WORKSPACE</div>
        <nav class="nav" aria-label="后台导航">
          <RouterLink v-for="item in nav" :key="item.path" :to="item.path" class="nav-link" active-class="active"><Icon :name="item.icon" />{{ item.name }}</RouterLink>
        </nav>
        <div class="sidebar-note"><RouterLink to="/"><Icon name="arrow-up-right" /> 查看公共页面</RouterLink></div>
      </template>
      <nav v-else class="public-nav" aria-label="公共导航">
        <RouterLink to="/" exact-active-class="active"><Icon name="images" />图片</RouterLink>
        <RouterLink to="/upload" active-class="active"><Icon name="upload" />公共上传</RouterLink>
      </nav>
      <div class="top-actions">
        <button class="account-btn" id="theme-toggle" :aria-label="theme === 'dark' ? '切换浅色模式' : '切换深色模式'" @click="toggleTheme"><Icon :name="theme === 'dark' ? 'sun' : 'moon'" /></button>
        <button v-if="adminArea" class="account-btn" :disabled="loggingOut" @click="logout">{{ loggingOut ? '正在退出…' : '退出登录' }}</button>
        <RouterLink v-else class="account-btn header-login" :to="session.admin ? '/admin/gallery' : '/login'">{{ session.admin ? '进入后台' : '登录' }}<Icon name="arrow-up-right" /></RouterLink>
      </div>
    </header>
    <main>
      <div v-if="adminArea" class="workspace-bar"><span>ADMIN WORKSPACE</span><span>{{ session.username }}</span></div>
      <div v-if="announcement?.displayType === 'banner'" class="site-announcement">{{ announcement.content }}</div>
      <RouterView v-slot="{ Component, route: current }"><component :is="Component" :key="current.path" /></RouterView>
    </main>
    <footer><span>{{ site.appName }} / IMAGE STORAGE</span><span>影像有序，灵感无界。</span></footer>
    <dialog ref="imageDialog" id="image-dialog" class="image-dialog"><button class="dialog-close" aria-label="关闭图片预览" @click="imageDialog.close()"><Icon name="x" /></button><img id="image-detail" alt=""><div id="image-detail-meta"></div></dialog>
    <dialog ref="announcementDialog" id="announcement-dialog"><button class="dialog-close" aria-label="关闭公告" @click="announcementDialog.close()"><Icon name="x" /></button><h2>站点公告</h2><p>{{ announcement?.content }}</p></dialog>
    <div class="toast" :class="{ show: notice.visible }" role="status" aria-live="polite">{{ notice.message }}</div>
  </div>
</template>
