import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import { Sha256, hashBlobSHA256 } from '../src/sha256-file.mjs';

test('incremental SHA-256 matches known vectors across block boundaries', () => {
  for (const value of ['', 'abc', 'a'.repeat(55), 'a'.repeat(56), 'a'.repeat(63), 'a'.repeat(64), 'a'.repeat(65), 'xyz'.repeat(10000)]) {
    const bytes = new TextEncoder().encode(value);
    const hash = new Sha256();
    for (let offset=0;offset<bytes.length;offset+=17) hash.update(bytes.subarray(offset,offset+17));
    assert.equal(hash.hex(),createHash('sha256').update(bytes).digest('hex'));
  }
});

test('blob hashing reads bounded slices', async () => {
  const bytes = new TextEncoder().encode('resumable upload content '.repeat(200));
  const blob = new Blob([bytes]);
  const progress = [];
  assert.equal(await hashBlobSHA256(blob,{chunkSize:73,onProgress:(done,total)=>progress.push([done,total])}),createHash('sha256').update(bytes).digest('hex'));
  assert.deepEqual(progress.at(-1),[bytes.length,bytes.length]);
});
