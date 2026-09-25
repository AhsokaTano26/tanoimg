<script setup>
import {onMounted, ref} from 'vue';
import FormField from '../components/FormField.vue';
import ImageGrid from '../components/ImageGrid.vue';
import Pagination from '../components/Pagination.vue';
import {useImages} from '../composables/useImages.js';
import {request, toast} from '../runtime.js';

const {images, page, pages, total, busy, error, load, go} = useImages('/api/images/deleted');
const retentionDays = ref(30);
const retentionLoading = ref(true);
const retentionError = ref('');
const saving = ref(false);

async function loadRetention() {
  retentionLoading.value = true;
  retentionError.value = '';
  try {
    const config = await request('/api/admin/retention');
    retentionDays.value = config.days;
  } catch (reason) {
    retentionError.value = reason.message;
  } finally {
    retentionLoading.value = false;
  }
}

async function saveRetention() {
  const days = Number(retentionDays.value);
  if (!Number.isInteger(days) || days < 0 || days > 3650) {
    toast('保留天数须为 0–3650');
    return;
  }
  saving.value = true;
  try {
    const config = await request('/api/admin/retention', {
      method: 'PUT',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({days}),
    });
    retentionDays.value = config.days;
    toast(days === 0 ? '已关闭回收站自动清理' : `回收站将保留 ${days} 天`);
  } catch (reason) {
    toast(reason.message);
  } finally {
    saving.value = false;
  }
}

onMounted(() => { load(); loadRetention(); });
</script>

<template>
  <section class="page-view">
    <div class="section-title">
      <div>
        <span class="eyebrow">RECYCLE BIN</span>
        <h1>回收站</h1>
        <p>图片可在保留期内恢复。到期后会自动永久清理。</p>
      </div>
      <RouterLink to="/admin/gallery" class="outline-button">返回图库</RouterLink>
    </div>
    <form class="settings-panel form-stack" novalidate @submit.prevent="saveRetention">
      <h2>自动清理</h2>
      <p>默认保留 30 天；填 0 可关闭自动清理。历史记录若没有删除时间，会保留到手动清理。</p>
      <FormField v-model="retentionDays" type="number" label="保留天数" :min="0" :max="3650" :disabled="retentionLoading" />
      <p v-if="retentionError" class="field-error" role="alert">{{ retentionError }}</p>
      <UiButton type="submit" variant="primary" :loading="saving" :disabled="retentionLoading">保存保留期</UiButton>
    </form>
    <ImageGrid recycle :images="images" :busy="busy" :error="error" @refresh="load" />
    <Pagination :page="page" :pages="pages" :total="total" :busy="busy" @change="go" />
  </section>
</template>
