import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Lazy } from '../src/lazy.js';

test('many callers share one attempt, and get its value', async () => {
  let runs = 0;
  const lazy = new Lazy(async (x) => (runs++, x * 2));
  assert.deepEqual(await Promise.all([lazy.get(21), lazy.get(99), lazy.get(5)]), [42, 42, 42]);
  assert.equal(runs, 1);
});

test('a failure is not remembered: the next caller tries again', async () => {
  let runs = 0;
  const lazy = new Lazy(async () => {
    if (runs++ === 0) throw new Error('down');
    return 'up';
  });
  await assert.rejects(lazy.get(), /down/);
  assert.equal(await lazy.get(), 'up');
});
