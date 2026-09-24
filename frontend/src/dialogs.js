import { reactive } from 'vue';
export const dialog = reactive({ visible:false, title:'', message:'', input:false, value:'', accept:'确认', danger:false });
let resolveDialog;
export function settleDialog(accepted) {
  const resolve = resolveDialog;
  resolveDialog = null;
  const result = accepted ? (dialog.input ? dialog.value : true) : (dialog.input ? null : false);
  dialog.visible = false;
  resolve?.(result);
}
export function ask(message, options = {}) {
  if (resolveDialog) settleDialog(false);
  Object.assign(dialog, { visible:true, title: options.title || '确认操作', message, input:false, value:'', accept: options.accept || '确认', danger:!!options.danger }, options);
  return new Promise(resolve => { resolveDialog = resolve; });
}
export const askText = (message, value='') => ask(message, { title:'编辑内容', input:true, value, accept:'保存' });
