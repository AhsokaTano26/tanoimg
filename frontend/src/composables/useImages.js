import { ref, onBeforeUnmount } from 'vue';
import { request, toast } from '../runtime.js';
export function useImages(endpoint = '/api/images', limit = 20) {
  const images = ref([]), page = ref(1), total = ref(0), pages = ref(1), busy = ref(false), error = ref('');
  let controller;
  async function load() {
    controller?.abort();
    const current = controller = new AbortController();
    busy.value = true; error.value = '';
    try {
      const separator = endpoint.includes('?') ? '&' : '?';
      const data = await request(`${endpoint}${separator}page=${page.value}&limit=${limit}`, { signal:current.signal });
      if (current.signal.aborted) return;
      images.value = data.images; total.value = data.pagination.total; pages.value = Math.max(1,data.pagination.totalPages);
      if (page.value > pages.value) { page.value=pages.value; return load(); }
    } catch (reason) { if (!current.signal.aborted) { error.value=reason.message; toast(reason.message); } }
    finally { if (controller === current) busy.value=false; }
  }
  async function go(value) { page.value=value; await load(); }
  onBeforeUnmount(() => controller?.abort());
  return { images,page,total,pages,busy,error,load,go };
}
