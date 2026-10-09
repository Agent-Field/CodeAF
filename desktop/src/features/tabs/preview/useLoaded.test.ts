import test from 'node:test';
import assert from 'node:assert/strict';
import { valueFor } from './useLoaded.ts';

test('a result is never returned for a different key', () => {
  const loaded = { key: 's1/a.go', value: 'package a' };
  assert.equal(valueFor(loaded, 's1/a.go'), 'package a');
  assert.equal(valueFor(loaded, 's1/b.go'), undefined);
  assert.equal(valueFor(loaded, 's2/a.go'), undefined);
  assert.equal(valueFor(loaded, undefined), undefined);
  assert.equal(valueFor(undefined, 's1/a.go'), undefined);
});
