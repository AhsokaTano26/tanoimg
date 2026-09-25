<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue';

const props = defineProps({ siteKey: {type:String,required:true} });
const emit = defineEmits(['update:token']);
const container = ref(null), error = ref('');
let widgetID, disposed = false;
let scriptPromise;
function loadScript() {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  if (scriptPromise) return scriptPromise;
  scriptPromise = new Promise((resolve,reject) => {
    let script = document.querySelector('script[data-tanoimg-turnstile]');
    if (!script) {
      script = document.createElement('script');
      script.dataset.tanoimgTurnstile = 'true';
      script.src = 'https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit';
      script.async = true;
      document.head.append(script);
    }
    script.addEventListener('load',() => window.turnstile ? resolve(window.turnstile) : reject(new Error('验证组件无法加载')),{once:true});
    script.addEventListener('error',() => {script.remove();reject(new Error('验证组件无法加载'));},{once:true});
  }).catch(reason => { scriptPromise = null; throw reason; });
  return scriptPromise;
}
async function render() {
  error.value = ''; emit('update:token','');
  try {
    const api = await loadScript();
    if (disposed || !container.value) return;
    if (widgetID !== undefined) api.remove(widgetID);
    widgetID = api.render(container.value,{
      sitekey:props.siteKey,
      callback:token => emit('update:token',token),
      'expired-callback':() => emit('update:token',''),
      'error-callback':() => {emit('update:token','');error.value='验证失败，请重试。';},
      'unsupported-callback':() => {emit('update:token','');error.value='当前浏览器无法完成验证。';},
    });
  } catch (reason) { if (!disposed) error.value = reason.message; }
}
function reset() { emit('update:token',''); if (widgetID !== undefined && window.turnstile) window.turnstile.reset(widgetID); }
defineExpose({ reset });
onMounted(render);
onBeforeUnmount(() => { disposed = true; emit('update:token',''); if (widgetID !== undefined && window.turnstile) window.turnstile.remove(widgetID); });
</script>
<template><div class="turnstile-widget"><div ref="container"></div><p v-if="error" role="alert" class="form-error">{{ error }} <UiButton @click="render">重新加载验证</UiButton></p></div></template>
