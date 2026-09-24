import { createApp } from 'vue';
import App from './App.vue';
import { router } from './router.js';
import { verifySession, toast } from './runtime.js';
try { await verifySession(); } catch { toast('无法验证登录状态，请稍后重试'); }
router.onError(error => toast(error.message));
const app = createApp(App).use(router);
await router.isReady();
app.mount('#app');
