import { ref, onMounted, onBeforeUnmount } from 'vue';
import { request,toast } from '../runtime.js';
export function useSettings(endpoint, transform = value=>value) {
  const model=ref(null), busy=ref(true), saving=ref(false), error=ref('');
  const controller=new AbortController();
  async function load(){busy.value=true;error.value='';try{model.value=transform(await request(endpoint,{signal:controller.signal}));}catch(reason){if(!controller.signal.aborted)error.value=reason.message;}finally{busy.value=false;}}
  async function save(body=model.value,target=endpoint) {saving.value=true;try{const data=await request(target,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),signal:controller.signal});toast('设置已保存');return data;}catch(reason){if(!controller.signal.aborted)toast(reason.message);return null;}finally{saving.value=false;}}
  onMounted(load);onBeforeUnmount(()=>controller.abort());
  return {model,busy,saving,error,load,save};
}
export const jsonOptions = body => ({method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
export const conversionOptions=[{label:'保留原格式',value:'none'},{label:'WebP',value:'webp'},{label:'PNG',value:'png'},{label:'JPEG',value:'jpg'}];
export const processingFields = config => ({...config,compressionQuality:config.compressionQuality || 80,convert:config.convertToWebp?'webp':config.convertToPng?'png':config.convertToJpg?'jpg':'none'});
export const processingBody = config => ({enableCompression:!!config.enableCompression,compressionQuality:config.compressionQuality,convertToWebp:config.convert==='webp',convertToPng:config.convert==='png',convertToJpg:config.convert==='jpg'});
