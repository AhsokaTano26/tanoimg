import { createApp } from 'vue';
import App from './App.vue';
import PrimeVue from 'primevue/config';
import Aura from '@primeuix/themes/aura';
import Ui from './components/ui';
import './ui.css';
import { router } from './router.js';
import { verifySession, toast } from './runtime.js';
try { await verifySession(); } catch { toast('无法验证登录状态，请稍后重试'); }
router.onError(error => toast(error.message));
const app = createApp(App).use(PrimeVue,{theme:{preset:Aura,options:{darkModeSelector:'[data-theme="dark"]',cssLayer:{name:'primevue',order:'primevue,app'}}}}).use(Ui).use(router);
await router.isReady();
app.mount('#app');
