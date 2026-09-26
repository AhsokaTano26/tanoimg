import {test} from 'node:test';
import assert from 'node:assert/strict';
import {fetchSelectionIDs,sendSelectionBatches} from '../src/image-selection.js';

test('all selection follows cursor pages with current filters and unique IDs',async()=>{
  const queries=[];
  const ids=await fetchSelectionIDs({q:'a & b',tag:'测试',visibility:'private'},async url=>{
    const query=new URL(url,'https://local.example').searchParams;queries.push(query);
    return query.get('after') ? {ids:['b','c'],nextCursor:''} : {ids:['a','b'],nextCursor:'b'};
  });
  assert.deepEqual(ids,['a','b','c']);assert.equal(queries.length,2);
  for(const query of queries){assert.equal(query.get('q'),'a & b');assert.equal(query.get('tag'),'测试');assert.equal(query.get('visibility'),'private');}
  assert.equal(queries[1].get('after'),'b');
});
test('cancelling all selection stops fetching and rejects partial selection',async()=>{
  const controller=new AbortController();let calls=0;
  await assert.rejects(fetchSelectionIDs({},async()=>{calls++;controller.abort();return {ids:['a'],nextCursor:'a'};},controller.signal),{name:'AbortError'});
  assert.equal(calls,1);
});
test('large selection batches stop at failure and only acknowledge successful IDs',async()=>{
  const ids=Array.from({length:2501},(_,i)=>String(i));const completed=[];const calls=[];
  await assert.rejects(sendSelectionBatches(ids,async batch=>{calls.push(batch);if(calls.length===2)throw new Error('failed');return {updatedCount:batch.length};},batch=>completed.push(...batch)),/failed/);
  assert.deepEqual(calls.map(batch=>batch.length),[1000,1000]);assert.deepEqual(completed,ids.slice(0,1000));
  const tail=[];await sendSelectionBatches(ids.slice(1000),async batch=>{tail.push(batch.length);return {};},()=>{});
  assert.deepEqual(tail,[1000,501]);
});
