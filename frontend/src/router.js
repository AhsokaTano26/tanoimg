import {createRouter,createWebHistory} from 'vue-router';
import {routeRecords,accessGuard} from './routes.js';
import {session,verifySession,setUnauthorizedHandler} from './runtime.js';
import GalleryView from './views/GalleryView.vue';
import LoginView from './views/LoginView.vue';
import AdminLayout from './components/AdminLayout.vue';
import NotFoundView from './views/NotFoundView.vue';
export const router=createRouter({
 history:createWebHistory(),
 routes:routeRecords({
  gallery:GalleryView,login:LoginView,layout:AdminLayout,notFound:NotFoundView,share:()=>import('./views/ShareView.vue'),imageDetail:()=>import('./views/ImageDetailView.vue'),
  upload:()=>import('./views/UploadView.vue'),recycle:()=>import('./views/RecycleView.vue'),stats:()=>import('./views/StatsView.vue'),transfer:()=>import('./views/TransferView.vue'),api:()=>import('./views/ApiView.vue'),
  appearance:()=>import('./views/settings/AppearanceView.vue'),site:()=>import('./views/settings/SiteView.vue'),
  'public-upload':()=>import('./views/settings/UploadSettingsView.vue'),'private-upload':()=>import('./views/settings/UploadSettingsView.vue'),
  apikeys:()=>import('./views/settings/KeysView.vue'),moderation:()=>import('./views/settings/ModerationView.vue'),
  'moderation-images':()=>import('./views/settings/ModerationImagesView.vue'),notification:()=>import('./views/settings/NotificationView.vue'),
  account:()=>import('./views/settings/AccountView.vue'),blacklist:()=>import('./views/settings/BlacklistView.vue'),
  storage:()=>import('./views/settings/StorageView.vue'),about:()=>import('./views/settings/AboutView.vue'),
  operations:()=>import('./views/settings/OperationsView.vue'),
  'embed-templates':()=>import('./views/settings/EmbedTemplatesView.vue'),
  shares:()=>import('./views/ShareManagementView.vue'),
  albums:()=>import('./views/AdminAlbumsView.vue'),
  publicAlbums:()=>import('./views/AlbumsView.vue'),
 }),
 scrollBehavior(to,from,saved){if(saved)return saved;if(to.hash)return {el:to.hash,top:32,behavior:'smooth'};return {top:0};},
});
router.beforeEach(accessGuard(session,verifySession));
setUnauthorizedHandler(()=>{if(router.currentRoute.value.meta.requiresAuth)router.replace({name:'login',query:{redirect:router.currentRoute.value.path}});});
