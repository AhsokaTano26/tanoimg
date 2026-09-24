import { createRouter, createWebHistory } from 'vue-router';
import { routeRecords, accessGuard } from './routes.js';
import { session, verifySession, setUnauthorizedHandler } from './runtime.js';
import GalleryView from './views/GalleryView.vue';
import UploadView from './views/UploadView.vue';
import LoginView from './views/LoginView.vue';
import AdminLayout from './components/AdminLayout.vue';
import NotFoundView from './views/NotFoundView.vue';

export const router = createRouter({
  history: createWebHistory(),
  routes: routeRecords({
    gallery: GalleryView, upload: UploadView, login: LoginView, layout: AdminLayout, notFound: NotFoundView,
    recycle: () => import('./views/RecycleView.vue'),
    stats: () => import('./views/StatsView.vue'),
    api: () => import('./views/ApiView.vue'),
    settings: () => import('./views/SettingsView.vue'),
  }),
  scrollBehavior(to, from, saved) {
    if (saved) return saved;
    if (to.hash) return { el: to.hash, top: innerWidth <= 700 ? 200 : 32, behavior: 'smooth' };
    return { top: 0 };
  },
});
router.beforeEach(accessGuard(session, verifySession));
setUnauthorizedHandler(() => {
  if (router.currentRoute.value.meta.requiresAuth) {
    router.replace({ name: 'login', query: { redirect: router.currentRoute.value.path } });
  }
});
