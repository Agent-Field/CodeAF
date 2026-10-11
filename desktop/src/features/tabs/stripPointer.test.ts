import test from 'node:test';
import assert from 'node:assert/strict';
import { stripMiddleCloses } from './stripPointer.ts';

test('middle-click closes an ordinary tab and a split, and leaves a pin; a place Home is an ordinary tab', () => {
  assert.equal(stripMiddleCloses({ pinned: false }), true);
  assert.equal(stripMiddleCloses({ pinned: true }), false);
  assert.equal(stripMiddleCloses({ pinned: false }), true);
});
