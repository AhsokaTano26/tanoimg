import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
export default defineConfig({
  plugins: [vue()],
  build: { outDir: '../internal/app/web', emptyOutDir: true, rollupOptions: { output: { manualChunks(id) { if(id.includes("node_modules/@primeuix/themes")) return "theme"; if(id.includes("node_modules/primevue") || id.includes("node_modules/@prime")) return "components"; if(id.includes("node_modules/@vue") || id.includes("node_modules/vue")) return "vue"; } } } },
});
