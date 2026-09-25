import {test} from 'node:test';
import assert from 'node:assert/strict';
import {renderEmbedTemplate} from '../src/embed-templates.js';

const image={url:'/i/photo.png',alt:'A "quote" <tag>',width:640,height:480,filename:'photo.png'};
test('embed templates substitute metadata and escape HTML attributes',()=>{
  const result=renderEmbedTemplate({body:'<img src="{url}" alt="{alt}" width="{width}" height="{height}">'},image,'https://example.test');
  assert.equal(result,'<img src="https://example.test/i/photo.png" alt="A &quot;quote&quot; &lt;tag&gt;" width="640" height="480">');
});
test('embed templates escape Markdown alt text and URL parentheses',()=>{
  const result=renderEmbedTemplate({body:'![{alt}]({url})'},{...image,alt:'[caption]',url:'/i/a(b).png'},'https://example.test');
  assert.equal(result,'![\\[caption\\]](https://example.test/i/a%28b%29.png)');
});
