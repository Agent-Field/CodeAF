import test from 'node:test';
import assert from 'node:assert/strict';
import { stripMiddleCloses } from './stripPointer.ts';

test('middle-click closes an ordinary tab and a split, and leaves a pin and a place Home', () => {
  assert.equal(stripMiddleCloses({ pinned: false, kind: 'conversation' }), true);
  assert.equal(stripMiddleCloses({ pinned: false, kind: 'conversation', place: undefined }), true);
  assert.equal(stripMiddleCloses({ pinned: true, kind: 'conversation' }), false);
  assert.equal(stripMiddleCloses({ pinned: true, kind: 'inbox' }), false);
  assert.equal(stripMiddleCloses({ pinned: true, kind: 'home', place: 'pl_reading' }), false);
  assert.equal(stripMiddleCloses({ pinned: false, kind: 'home', place: 'root' }), true);
});
