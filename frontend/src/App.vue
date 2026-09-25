<script setup>
import {computed,ref,watch,onMounted} from 'vue';
import {useRoute,useRouter} from 'vue-router';
import Icon from './components/Icon.vue';
import AdminNavigation from './components/AdminNavigation.vue';
import GlobalDialogs from './components/GlobalDialogs.vue';
import AnnouncementContent from './components/AnnouncementContent.vue';
import {session,site,notice,request,loadSite,toast} from './runtime.js';
const route=useRoute(),router=useRouter();
const adminArea=computed(()=>!!route.meta.requiresAuth && session.admin);
const theme=ref(document.documentElement.dataset.theme||'dark'),loggingOut=ref(false),navigationOpen=ref(false),announcementVisible=ref(false);
const announcement=computed(()=>site.announcement?.enabled ? site.announcement:null);
function toggleTheme(){theme.value=theme.value==='dark'?'light':'dark';document.documentElement.dataset.theme=theme.value;document.querySelector('meta[name="theme-color"]').content=theme.value==='dark'?'#0c0e12':'#f4f5f7';try{localStorage.setItem('tanoimg-theme',theme.value);}catch{}}
async function logout(){loggingOut.value=true;try{const failure=await router.push('/');if(failure)return;await request('/api/auth/logout',{method:'POST'});session.admin=false;session.username='';toast('已退出登录');}catch(error){toast(error.message);}finally{loggingOut.value=false;}}
watch(announcement,value=>{if(value?.content && value.displayType!=='banner'){try{if(sessionStorage.getItem('tanoimg-announcement')===value.content)return;sessionStorage.setItem('tanoimg-announcement',value.content);}catch{}announcementVisible.value=true;}});
onMounted(loadSite);
</script>
<template><div :class="[adminArea?'admin-shell':'public-shell',{'has-background':!!site.backgroundUrl}]">
  <div class="site-background" aria-hidden="true" :style="{backgroundImage:site.backgroundUrl?'url('+JSON.stringify(site.backgroundUrl)+')':'none','--background-blur':site.backgroundBlur+'px'}"></div>
  <header :class="adminArea?'topbar':'public-header'">
    <RouterLink class="brand" to="/" :aria-label="site.appName+' 首页'"><span class="brand-mark" :style="site.appLogo?{backgroundImage:'url('+JSON.stringify(site.appLogo)+')'}:{}"><Icon v-if="!site.appLogo" name="aperture" /></span><span>{{ site.appName }}</span></RouterLink>
    <template v-if="adminArea"><div class="desktop-navigation"><AdminNavigation /></div><div class="sidebar-note"><RouterLink to="/">查看公共图库 <Icon name="arrow-up-right" /></RouterLink></div></template>
    <nav v-else class="public-nav" aria-label="公共导航"><RouterLink to="/" exact-active-class="active"><Icon name="images" />图库</RouterLink><RouterLink to="/albums" active-class="active"><Icon name="aperture" />相册</RouterLink><RouterLink :to="{path:'/',hash:'#public-upload'}"><Icon name="upload" />上传</RouterLink></nav>
    <div class="top-actions"><UiButton class="theme-button" :aria-label="theme==='dark'?'切换浅色模式':'切换深色模式'" :icon="theme==='dark'?'sun':'moon'" @click="toggleTheme" /><UiButton v-if="adminArea" :loading="loggingOut" @click="logout">退出登录</UiButton><RouterLink v-else class="outline-button header-login" :to="session.admin?'/admin/gallery':'/login'">{{ session.admin?'进入后台':'登录' }}<Icon name="arrow-up-right" /></RouterLink></div>
    <UiButton v-if="adminArea" class="mobile-menu-button" icon="sliders-horizontal" @click="navigationOpen=true">导航与设置</UiButton>
  </header>
  <UiDrawer v-model:visible="navigationOpen"><AdminNavigation @navigate="navigationOpen=false" /></UiDrawer>
  <main><div v-if="adminArea" class="workspace-bar"><span>ADMIN WORKSPACE</span><span>{{ session.username }}</span></div><div v-if="announcement?.displayType==='banner'" class="site-announcement"><AnnouncementContent :content="announcement.content" /></div><RouterView v-slot="{Component,route:current}"><div :key="current.path" class="route-page"><component :is="Component" /></div></RouterView></main>
  <footer><span>{{ site.appName }} / IMAGE STORAGE</span><span>影像有序，灵感无界。</span></footer>
  <UiDialog v-model:visible="announcementVisible" title="站点公告"><AnnouncementContent :content="announcement?.content || ''" /></UiDialog>
  <GlobalDialogs /><div class="toast" :class="{show:notice.visible}" role="status" aria-live="polite">{{ notice.message }}</div>
</div></template>
