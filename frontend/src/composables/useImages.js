import { ref, onBeforeUnmount } from 'vue';
import { request, toast } from '../runtime.js';
export function useImages(endpoint = '/api/images', limit = 20) {
  const images = ref([]), page = ref(1), perPage = ref(limit), filters = ref({}), total = ref(0), pages = ref(1), busy = ref(false), error = ref('');
  let controller;
  async function load() {
    controller?.abort();
    const current = controller = new AbortController();
    busy.value = true; error.value = '';
    try {
      const url = new URL(endpoint, location.origin);
      url.searchParams.set('page', String(page.value));
      url.searchParams.set('limit', String(perPage.value));
      for (const [key,value] of Object.entries(filters.value)) {
        if (value) url.searchParams.set(key, value);
      }
      const data = await request(url.pathname + url.search, { signal:current.signal });
      if (current.signal.aborted) return;
      images.value = data.images; total.value = data.pagination.total; pages.value = Math.max(1,data.pagination.totalPages);
      if (page.value > pages.value) { page.value=pages.value; return load(); }
    } catch (reason) { if (!current.signal.aborted) { error.value=reason.message; toast(reason.message); } }
    finally { if (controller === current) busy.value=false; }
  }
  async function go(value) { page.value=Math.max(1, Math.min(pages.value, Number(value) || 1)); await load(); }
  async function setPerPage(value) { perPage.value=Math.max(1, Math.min(100, Number(value) || limit)); page.value=1; await load(); }
  async function setFilters(value) { filters.value={...value}; page.value=1; await load(); }
  onBeforeUnmount(() => controller?.abort());
  return { images,page,perPage,filters,total,pages,busy,error,load,go,setPerPage,setFilters };
}
