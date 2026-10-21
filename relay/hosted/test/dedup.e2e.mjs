// Dedup by Has: a client asks which rids the relay already holds before it sends a frame, so the cost of
// asking must be one request per 1000 rids (never one per object), must never confirm another identity's
// objects, and must touch no R2 at all (the index is SQLite). Live relay for the requests, in-process
// store for the R2 calls. Not part of `npm test`; `npm run e2e` runs it.
import assert from 'node:assert/strict';
import { newIdentity, newDevice } from './party.js';
import { frame, rid, openBucket, countingBucket } from './helpers.js';
import { call } from './client.js';
import { R2Store, sealFrame } from '../src/store.js';
import { memorySql } from './sql.js';

const OBJECTS = 2500;
const PER_FRAME = 500;
const BATCH = 1000; // routes.js MAX_HAS
const rids = Array.from({ length: OBJECTS }, (_, i) => rid(i + 1));
const strangers = Array.from({ length: OBJECTS }, (_, i) => rid(i + 1 + OBJECTS));
const batches = (all) => Array.from({ length: Math.ceil(all.length / BATCH) }, (_, i) => all.slice(i * BATCH, (i + 1) * BATCH));
const frames = Array.from({ length: OBJECTS / PER_FRAME }, (_, i) => frame(rids.slice(i * PER_FRAME, (i + 1) * PER_FRAME)));

/** askHas sends one Has per batch and answers the flat answer, the request count and each batch's latency. */
async function askHas(dev, all) {
  const have = [];
  const ms = [];
  for (const rs of batches(all)) {
    const t0 = performance.now();
    const res = await call(dev, 'POST', '/v1/store/has', { rids: rs });
    ms.push(Math.round(performance.now() - t0));
    assert.equal(res.status, 200);
    have.push(...res.json.have);
  }
  return { have, requests: ms.length, ms };
}

const mine = await newDevice(await newIdentity());
const theirs = await newDevice(await newIdentity());
for (const f of frames) assert.equal((await call(mine, 'POST', '/v1/store/frames', f.bytes)).status, 200);

// (a) one request per 1000 rids, by the client's count and by the relay's own stats.
const before = (await call(mine, 'GET', '/v1/store/stats')).json.has;
const own = await askHas(mine, rids);
assert.equal(own.requests, 3, '2500 rids are 3 has requests');
assert.equal((await call(mine, 'GET', '/v1/store/stats')).json.has - before, 3, 'the relay counted 3');
// (b) every object is held.
assert.equal(own.have.length, OBJECTS);
assert.ok(own.have.every(Boolean), 'all 2500 answered true');
// (c) no cross-identity confirmation, and a rid nobody stored is a plain no.
const other = await askHas(theirs, rids);
assert.ok(other.have.every((h) => h === false), 'another identity is never told yes');
assert.ok((await askHas(mine, strangers)).have.every((h) => h === false), 'unknown rids are no');
// A batch over the cap is refused, so a client must split it.
assert.equal((await call(mine, 'POST', '/v1/store/has', { rids: rids.slice(0, BATCH) })).status, 200);
assert.equal((await call(mine, 'POST', '/v1/store/has', { rids: rids.slice(0, BATCH + 1) })).json.err, 'too_many');
console.log(`has: ${own.requests} requests for ${OBJECTS} rids, batch ms ${own.ms.join('/')}`);

// (d) the same Has path in process, over a counting bucket: its R2 calls by class (A writes/lists, B reads).
const env = await openBucket();
try {
  const sql = memorySql();
  for (const f of frames) await new R2Store(env.bucket, sql, 'id_dedup').put(await sealFrame(f.bytes));
  const counted = countingBucket(env.bucket);
  const store = new R2Store(counted.bucket, sql, 'id_dedup');
  batches(rids).forEach((rs, i) => {
    const calls = { ...counted.calls };
    assert.ok(store.has(rs).every(Boolean));
    const made = Object.fromEntries(Object.entries(counted.calls).filter(([k, n]) => n !== (calls[k] ?? 0)));
    console.log(`has batch ${i + 1} (${rs.length} rids): R2 calls ${JSON.stringify(made)}`);
  });
  assert.deepEqual(counted.calls, {}, 'Has reads the SQLite index and makes no R2 call of either class');
} finally {
  await env.close();
}
console.log('dedup: all pass');
