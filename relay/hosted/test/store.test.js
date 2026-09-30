import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { Conflict, R2Store, sealFrame } from '../src/store.js';
import { openBucket, countingBucket, frame, rid } from './helpers.js';

let env;
before(async () => { env = await openBucket(); });
after(() => env.close());

const put = async (store, f) => store.put(await sealFrame(f.bytes));

test('put, has and get through the index; a cold store rebuilds from the frames', async () => {
  const s = new R2Store(env.bucket, 'id_s1');
  const f = frame([rid(1), rid(2), rid(3)]);
  assert.equal(await put(s, f), true);
  assert.deepEqual(await s.has([rid(2), rid(9)]), [true, false]);
  assert.deepEqual(await s.get(rid(3)), f.objs[2]);
  const cold = new R2Store(env.bucket, 'id_s1'); // a new object after eviction: nothing in memory
  assert.deepEqual(await cold.get(rid(1)), f.objs[0]);
  assert.equal(await cold.get(rid(99)), null);
});

test('an identity never reads another identity objects', async () => {
  await put(new R2Store(env.bucket, 'id_a'), frame([rid(5)]));
  assert.deepEqual(await new R2Store(env.bucket, 'id_b').has([rid(5)]), [false]);
});

test('a frame put twice is stored once and the second put says it was not new', async () => {
  const { bucket, calls } = countingBucket(env.bucket);
  const s = new R2Store(bucket, 'id_s2');
  const f = frame([rid(7)]);
  assert.equal(await put(s, f), true);
  assert.equal(await put(s, f), false);
  assert.equal(calls.put, 1);
});

test('a bad frame is refused before anything is stored', async () => {
  await assert.rejects(sealFrame(Uint8Array.of(1, 2, 3)));
});

test('a rid held with other bytes is a conflict, and the frame stores none of its objects', async () => {
  const s = new R2Store(env.bucket, 'id_conflict');
  await put(s, frame([rid(1)], 'first'));
  await assert.rejects(put(s, frame([rid(2), rid(1)], 'other')), Conflict);
  assert.deepEqual(await s.has([rid(2)]), [false]);
});

test('the same object in another frame is not a conflict', async () => {
  const s = new R2Store(env.bucket, 'id_twice');
  await put(s, frame([rid(1)]));
  assert.equal(await put(s, frame([rid(1), rid(2)])), true);
});

test('a burst of first requests builds the index once', async () => {
  const seed = new R2Store(env.bucket, 'id_burst');
  await put(seed, frame([rid(1)]));
  const { bucket, calls } = countingBucket(env.bucket);
  const cold = new R2Store(bucket, 'id_burst');
  await Promise.all(Array.from({ length: 20 }, () => cold.get(rid(1))));
  assert.equal(calls.list, 1);
});

test('a failed load is tried again by the next request', async () => {
  const flaky = { ...env.bucket, get: env.bucket.get.bind(env.bucket), list: env.bucket.list.bind(env.bucket), put: env.bucket.put.bind(env.bucket) };
  let failures = 1;
  flaky.list = async (o) => {
    if (failures-- > 0) throw new Error('r2 down');
    return env.bucket.list(o);
  };
  const s = new R2Store(flaky, 'id_flaky');
  await assert.rejects(s.has([rid(1)]));
  assert.deepEqual(await s.has([rid(1)]), [false]);
});

test('the snapshot is saved once enough frames were found by listing, not before', async () => {
  const writer = new R2Store(env.bucket, 'id_snap');
  for (let i = 1; i <= 33; i++) await put(writer, frame([rid(i)]));
  const { bucket, calls } = countingBucket(env.bucket);
  await new R2Store(bucket, 'id_snap').has([rid(1)]);
  assert.equal(calls.put, 1);
  const again = countingBucket(env.bucket);
  await new R2Store(again.bucket, 'id_snap').has([rid(1)]);
  assert.equal(again.calls.put, undefined);
});

test('admit runs only for a new frame, before anything is written', async () => {
  const { bucket, calls } = countingBucket(env.bucket);
  const s = new R2Store(bucket, 'id_admit');
  const f = await sealFrame(frame([rid(1)]).bytes);
  await assert.rejects(s.put(f, () => { throw new Error('over quota'); }), /over quota/);
  assert.equal(calls.put, undefined);
  assert.equal(await s.put(f), true);
  await s.put(f, () => { throw new Error('a stored frame is not asked about'); });
});
