import assert from 'node:assert/strict';
import test from 'node:test';
import { nowCount } from './nowCount.ts';

test('the Now count draws nothing when unknown or zero', () => {
  assert.equal(nowCount(undefined), undefined);
  assert.equal(nowCount(0), undefined);
  assert.equal(nowCount(11), 11);
});
