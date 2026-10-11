import assert from 'node:assert/strict';
import test from 'node:test';
import { createLoadedCache, resourceKey } from './loadedCache.ts';

const deferred = <T,>() => {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
};

test('parallel mounts share a request, then reopen reads synchronously with no request', async () => {
  const cache = createLoadedCache();
  const answer = deferred<string>();
  let calls = 0;
  const load = () => { calls++; return answer.promise; };
  const first = cache.load('tab', 'r1', load);
  const concurrent = cache.load('tab', 'r1', load);
  assert.equal(first, concurrent);
  answer.resolve('last reply');
  await first;
  assert.equal(cache.read('tab'), 'last reply');
  assert.equal(await cache.load('tab', 'r1', load), 'last reply');
  assert.equal(calls, 1);
});

test('revision changes refresh in parallel without erasing the known content or accepting a late revision', async () => {
  const cache = createLoadedCache();
  cache.seed('tab', 'loaded in the workspace');
  let calls = 0;
  await cache.load('tab', 'r1', async () => { calls++; return 'unexpected'; });
  assert.equal(calls, 0);
  const old = deferred<string>();
  const next = deferred<string>();
  const oldRead = cache.load('tab', 'r2', () => old.promise);
  assert.equal(cache.read('tab'), 'loaded in the workspace');
  const newRead = cache.load('tab', 'r3', () => next.promise);
  next.resolve('new revision');
  await newRead;
  old.resolve('late old revision');
  await oldRead;
  assert.equal(cache.read('tab'), 'new revision');
});

test('different kinds, sessions and targets never borrow contents, and missing data stays absent', async () => {
  const cache = createLoadedCache();
  const key = resourceKey('file', 's1', 'a');
  cache.seed(key, 'package a');
  for (const other of [resourceKey('diff', 's1', 'a'), resourceKey('file', 's2', 'a'), resourceKey('file', 's1', 'b'), undefined]) assert.equal(cache.read(other), undefined);
  await assert.rejects(cache.load('missing', '', async () => { throw new Error('offline'); }));
  assert.equal(cache.read('missing'), undefined);
  assert.equal(await cache.load('missing', '', async () => 'connected'), 'connected');
});

test('the bounded cache discards the least recently used resource', async () => {
  const cache = createLoadedCache();
  for (let index = 0; index < 256; index++) cache.seed(String(index), index);
  await cache.load('0', '', async () => -1);
  cache.seed('new', 256);
  assert.equal(cache.read('0'), 0);
  assert.equal(cache.read('1'), undefined);
});
