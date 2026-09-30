import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { Conflict, R2Store, sealFrame } from '../src/store.js';
import { memorySql } from './sql.js';
import { openBucket, frame, rid } from './helpers.js';

let env;
before(async () => { env = await openBucket(); });
after(() => env.close());

// One identity's state is its SQLite and its R2 prefix; a "cold" store is a new object over the same two.
const open = (identity, sql = memorySql()) => ({ sql, store: new R2Store(env.bucket, sql, identity) });
const put = async (store, f, admit) => store.put(await sealFrame(f.bytes), admit);

test('put, has and get through the index; a cold store finds everything in the SQLite', async () => {
  const { sql, store } = open('id_s1');
  const f = frame([rid(1), rid(2), rid(3)]);
  assert.equal(await put(store, f), true);
  assert.deepEqual(store.has([rid(2), rid(9)]), [true, false]);
  assert.deepEqual(await store.get(rid(3)), f.objs[2]);
  const cold = open('id_s1', sql).store;
  assert.deepEqual(await cold.get(rid(1)), f.objs[0]);
  assert.equal(await cold.get(rid(99)), null);
});

test('an identity never reads another identity objects', async () => {
  await put(open('id_a').store, frame([rid(5)]));
  assert.deepEqual(open('id_b').store.has([rid(5)]), [false]);
});

test('a frame put twice is stored once and the second put says it was not new', async () => {
  const { store } = open('id_s2');
  const f = frame([rid(7)]);
  assert.equal(await put(store, f), true);
  assert.equal(await put(store, f), false);
});

test('a bad frame is refused before anything is stored', async () => {
  await assert.rejects(sealFrame(Uint8Array.of(1, 2, 3)));
});

test('a rid held with other bytes is a conflict, and the frame stores none of its objects', async () => {
  const { store } = open('id_conflict');
  await put(store, frame([rid(1)], 'first'));
  await assert.rejects(put(store, frame([rid(2), rid(1)], 'other')), Conflict);
  assert.deepEqual(store.has([rid(2)]), [false]);
});

test('two puts racing to store one rid with different bytes: exactly one wins', async () => {
  const { store } = open('id_race');
  const results = await Promise.allSettled([put(store, frame([rid(1)], 'one')), put(store, frame([rid(1)], 'two'))]);
  assert.deepEqual(results.map((r) => r.status).sort(), ['fulfilled', 'rejected']);
});

test('the same object in another frame is not a conflict', async () => {
  const { store } = open('id_twice');
  await put(store, frame([rid(1)]));
  assert.equal(await put(store, frame([rid(1), rid(2)])), true);
});

test('admit runs only for a new frame, before anything is written', async () => {
  const { store } = open('id_admit');
  const f = frame([rid(1)]);
  await assert.rejects(put(store, f, () => { throw new Error('over quota'); }), /over quota/);
  assert.deepEqual(store.has([rid(1)]), [false]);
  assert.equal(await put(store, f), true);
  await put(store, f, () => { throw new Error('a stored frame is not asked about'); });
});

test('a frame that reached R2 but not the index is indexed by the next put of it', async () => {
  const { store } = open('id_crash');
  const f = frame([rid(1)]);
  const sealed = await sealFrame(f.bytes);
  await env.bucket.put(`id_crash/f/${sealed.id}`, f.bytes); // the crash: durable, never indexed
  assert.deepEqual(store.has([rid(1)]), [false]);
  assert.equal(await put(store, f), true);
  assert.deepEqual(await store.get(rid(1)), f.objs[0]);
});

test('an identity with thousands of objects needs no memory for them: a frame of 3000 objects is found row by row', async () => {
  const { store } = open('id_many');
  const rids = Array.from({ length: 3000 }, (_, i) => rid(i + 1));
  const f = frame(rids);
  await put(store, f);
  assert.deepEqual(await store.get(rids[2999]), f.objs[2999]);
  assert.deepEqual(store.has([rids[0], rid(99999)]), [true, false]);
});
