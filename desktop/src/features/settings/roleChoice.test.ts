import test from 'node:test';
import assert from 'node:assert/strict';
import { resolveRoleChoice } from './roleChoice.ts';

test('an effort click during an unfinished model save keeps the newly chosen model', () => {
  assert.deepEqual(resolveRoleChoice({ effort: 'high' }, { model: 'b' }, 'a'), { model: 'b', effort: 'high' });
});

test('an effort click with nothing queued uses the saved model', () => {
  assert.deepEqual(resolveRoleChoice({ effort: 'low' }, undefined, 'a'), { model: 'a', effort: 'low' });
});

test('a model change clears the effort; a reset names an empty model', () => {
  assert.deepEqual(resolveRoleChoice({ model: 'c' }, { model: 'b', effort: 'high' }, 'a'), { model: 'c', effort: undefined });
  assert.deepEqual(resolveRoleChoice({ model: '' }, undefined, 'a'), { model: '', effort: undefined });
});
