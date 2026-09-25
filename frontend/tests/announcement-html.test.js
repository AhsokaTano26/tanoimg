import {test} from 'node:test';
import assert from 'node:assert/strict';
import {JSDOM} from 'jsdom';
import {sanitizeAnnouncement} from '../src/announcement-html.js';

const window=new JSDOM('').window;

test('announcement supports basic formatting and safe links',()=>{
  const html=sanitizeAnnouncement('<p>欢迎 <strong>使用</strong></p><ul><li>第一项</li></ul><a href="https://example.com/help">帮助</a>',window);
  assert.match(html,/<strong>使用<\/strong>/);
  assert.match(html,/<li>第一项<\/li>/);
  assert.match(html,/href="https:\/\/example.com\/help"/);
});

test('announcement removes executable HTML and attributes',()=>{
  const html=sanitizeAnnouncement('<script>alert(1)</script><svg onload="alert(2)"></svg><a href="javascript:alert(3)" onclick="alert(4)">危险</a><img src=x onerror="alert(5)"><b style="position:fixed">文字</b>',window);
  for(const unsafe of ['<script','<svg','<img','javascript:','onclick','onerror','style=','alert(']) assert.ok(!html.includes(unsafe),unsafe);
  assert.match(html,/<a>危险<\/a>/);
  assert.match(html,/<b>文字<\/b>/);
});

test('legacy plain text is retained',()=>{
  assert.equal(sanitizeAnnouncement('第一行\n第二行',window),'第一行\n第二行');
});
