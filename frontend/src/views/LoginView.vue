<script setup>
import { ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import Icon from '../components/Icon.vue';
import { request, verifySession } from '../runtime.js';
import { loginDestination } from '../routes.js';
const route = useRoute();
const router = useRouter();
const username = ref('');
const password = ref('');
const busy = ref(false);
const error = ref('');
async function login() {
  if (busy.value) return;
  busy.value = true;
  error.value = '';
  try {
    await request('/api/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username: username.value, password: password.value }) });
    password.value = '';
    await verifySession();
    await router.replace(loginDestination(route.query.redirect));
  } catch (reason) { error.value = reason.message; }
  finally { busy.value = false; }
}
</script>
<template>
  <section class="login-page">
    <form class="login-card" @submit.prevent="login">
      <span class="dialog-symbol"><Icon name="aperture" /></span>
      <span class="eyebrow">ADMIN ACCESS</span>
      <h1>登录后台</h1><p>管理你的图片、上传权限与站点设置。</p>
      <label class="setting-field">用户名<input v-model="username" name="username" autocomplete="username" required :disabled="busy" /></label>
      <label class="setting-field">密码<input v-model="password" name="password" type="password" autocomplete="current-password" required :disabled="busy" /></label>
      <p v-if="error" class="form-error" role="alert">{{ error }}</p>
      <button class="primary-button full" type="submit" :disabled="busy">{{ busy ? '正在登录…' : '登录并进入后台' }}<Icon name="arrow-right" /></button>
      <RouterLink class="login-back" to="/">返回图片页面</RouterLink>
    </form>
  </section>
</template>
