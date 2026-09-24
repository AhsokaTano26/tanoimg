import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createUploadQueue } from '../../../frontend/src/upload-queue.mjs';

test('a large batch never starts more than four uploads and finishes once', async () => {
  let active = 0;
  let peak = 0;
  let idleCalls = 0;
  let finish;
  const idle = new Promise(resolve => { finish = resolve; });
  const queue = createUploadQueue({
    concurrency: 4,
    upload: async file => {
      active++;
      peak = Math.max(peak, active);
      await new Promise(resolve => setTimeout(resolve, 1));
      active--;
      return file;
    },
    onIdle: snapshot => { idleCalls++; finish(snapshot); },
  });
  queue.add(Array.from({ length: 1000 }, (_, index) => index));
  const result = await idle;
  assert.equal(peak, 4);
  assert.equal(result.total, 1000);
  assert.equal(result.completed, 1000);
  assert.equal(result.failed, 0);
  assert.equal(idleCalls, 1);
});

test('cancel drops queued files and aborts active uploads', async () => {
  let started = 0;
  let finish;
  const idle = new Promise(resolve => { finish = resolve; });
  const queue = createUploadQueue({
    concurrency: 2,
    upload: (_file, signal) => new Promise((_resolve, reject) => {
      started++;
      signal.addEventListener('abort', () => reject(new Error('aborted')), { once: true });
    }),
    onIdle: finish,
  });
  queue.add(Array.from({ length: 100 }, (_, index) => index));
  await new Promise(resolve => setImmediate(resolve));
  queue.cancel();
  const result = await idle;
  assert.equal(started, 2);
  assert.equal(result.completed, 100);
  assert.equal(result.cancelled, 100);
});
