import test from 'node:test';
import assert from 'node:assert/strict';
import { dragRectangle, rotateRectangleClockwise, rotatedSize, editorOutputFormat } from '../src/image-editor.mjs';

test('crop and redaction rectangles stay aligned after four clockwise rotations', () => {
  let size={width:400,height:300};
  let rect={x:25,y:40,width:120,height:70};
  for (let turn=0;turn<4;turn++) {
    rect=rotateRectangleClockwise(rect,size);
    size=rotatedSize(size,1);
    assert.ok(rect.x>=0 && rect.y>=0 && rect.x+rect.width<=size.width && rect.y+rect.height<=size.height);
  }
  assert.deepEqual(rect,{x:25,y:40,width:120,height:70});
  assert.deepEqual(size,{width:400,height:300});
});

test('drag geometry clamps to image bounds in either direction', () => {
  assert.deepEqual(dragRectangle({x:500,y:200},{x:-10,y:20},{width:400,height:300}),{x:0,y:20,width:400,height:180});
  assert.deepEqual(dragRectangle({x:120.7,y:90.2},{x:20.1,y:10.8},{width:400,height:300}),{x:20,y:10,width:101,height:81});
});

test('only browser encodings matching the original are eligible for replacement', () => {
  assert.deepEqual(editorOutputFormat('jpg'),{mime:'image/jpeg',extension:'jpg',replaceable:true});
  assert.deepEqual(editorOutputFormat('gif'),{mime:'image/png',extension:'png',replaceable:false});
});
