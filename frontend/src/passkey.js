import { browserSupportsWebAuthn, startAuthentication, startRegistration, WebAuthnAbortService } from '@simplewebauthn/browser';
import { request } from './runtime.js';
const post = body => ({ method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(body) });
export const passkeySupported = () => window.isSecureContext && browserSupportsWebAuthn();
export const cancelPasskey = () => WebAuthnAbortService.cancelCeremony();
export function passkeyError(error) {
  if (['NotAllowedError','AbortError'].includes(error.name) || error.code==='ERROR_CEREMONY_ABORTED') return '验证已取消或超时，请重新尝试。';
  if(error.name==='InvalidStateError') return '此设备已绑定该账户，请使用已有 Passkey 或选择另一台设备。';
  if(error.name==='SecurityError') return '当前域名与 Passkey 配置不一致，请从配置的 HTTPS 地址访问。';
  return error.message || 'Passkey 验证失败';
}
export async function loginWithPasskey() {
  const data = await request('/api/auth/passkey/begin',post({}));
  const credential = await startAuthentication({ optionsJSON:data.options.publicKey });
  return request('/api/auth/passkey/finish',post({ challenge:data.challenge,credential }));
}
export async function registerPasskey(proof) {
  const data = await request('/api/admin/passkeys/register/begin',post(proof));
  const credential = await startRegistration({ optionsJSON:data.options.publicKey });
  return request('/api/admin/passkeys/register/finish',post({ challenge:data.challenge,credential }));
}
