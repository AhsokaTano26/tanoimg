import { test } from 'node:test';
import assert from 'node:assert/strict';
import { request } from '../src/runtime.js';
test('rate-limit errors preserve Retry-After for the upload queue', async () => {
  const original=globalThis.fetch;
  try {
    globalThis.fetch=async()=>new Response(JSON.stringify({success:false,message:'slow down'}),{status:429,headers:{'Retry-After':'60','Content-Type':'application/json'}});
    await assert.rejects(request('/api/upload/public'),error=>error.status===429 && error.retryAfterMs===60000);
  } finally {globalThis.fetch=original;}
});
