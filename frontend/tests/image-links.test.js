import { test } from 'node:test';
import assert from 'node:assert/strict';
import { imageLinks } from '../src/image-links.js';
test('sharing formats escape filenames and preserve an absolute image URL', () => {
  const result=imageLinks({url:'/i/image(test).png',originalName:'a[1]"<&\\b'},'https://img.example');
  assert.equal(result['直链'],'https://img.example/i/image(test).png');
  assert.equal(result.BBCode,'[img]https://img.example/i/image(test).png[/img]');
  assert.ok(result.HTML.includes('&quot;&lt;&amp;'));
  assert.ok(result.Markdown.includes('image%28test%29.png'));
  assert.ok(result.Markdown.includes('a\\[1\\]'));
});
