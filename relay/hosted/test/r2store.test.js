import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { R2Store, forgetIndexes } from '../src/r2store.js';
import { openBucket, frame, rid } from './helpers.js';

let env;
before(async () => { env = await openBucket(); });
after(() => env.close());

test('put, has, get through the lazy index; a cold isolate rebuilds from the frames', async () => {
  const s = new R2Store(env.bucket, 'id_s1');
  const f = frame([rid(1), rid(2), rid(3)]);
  const r = await s.put(f.bytes);
  assert.equal(r.objects, 3);
  assert.deepEqual(await s.has([rid(2), rid(9)]), [true, false]);
  assert.deepEqual(await s.get(rid(3)), f.objs[2]);
  forgetIndexes(); // a new isolate: snapshot only
  assert.deepEqual(await new R2Store(env.bucket, 'id_s1').get(rid(1)), f.objs[0]);
  assert.equal(await new R2Store(env.bucket, 'id_s1').get(rid(99)), null);
});

test('an identity never reads another identity objects', async () => {
  await new R2Store(env.bucket, 'id_a').put(frame([rid(5)]).bytes);
  assert.deepEqual(await new R2Store(env.bucket, 'id_b').has([rid(5)]), [false]);
});

test('a frame put twice is idempotent and a bad frame is refused', async () => {
  const s = new R2Store(env.bucket, 'id_s2');
  const f = frame([rid(7)]);
  assert.equal((await s.put(f.bytes)).frame, (await s.put(f.bytes)).frame);
  await assert.rejects(s.put(Uint8Array.of(1, 2, 3)));
});
