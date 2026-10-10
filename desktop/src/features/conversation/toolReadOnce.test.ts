import assert from 'node:assert/strict';
import { test } from 'node:test';
import { toolReadOnce } from './toolReadOnce.ts';

test('a key is loaded once, and a failure can be tried again', async () => {
  const cache = new Map<string, Promise<string>>();
  let calls = 0;
  const load = () => {
    calls += 1;
    return Promise.resolve('body');
  };
  const [first, second] = await Promise.all([
    toolReadOnce(cache, 's\0g1', load),
    toolReadOnce(cache, 's\0g1', load),
  ]);
  assert.equal(first, 'body');
  assert.equal(second, 'body');
  assert.equal(await toolReadOnce(cache, 's\0g1', load), 'body');
  assert.equal(calls, 1);

  let fails = 0;
  const boom = () => {
    fails += 1;
    return Promise.reject(new Error('gone'));
  };
  await assert.rejects(() => toolReadOnce(cache, 's\0bad', boom), /gone/);
  await assert.rejects(() => toolReadOnce(cache, 's\0bad', boom), /gone/);
  assert.equal(fails, 2);
});
