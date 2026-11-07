import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { Conflict, R2Store, sealFrame } from '../src/store.js';
import { Tenant } from '../src/tenant.js';
import { matchRoute } from '../src/routes.js';
import { DEFAULTS } from '../src/limits.js';
import { policyOf } from '../src/rules.js';
import { Flight } from '../src/flight.js';
import { Wire } from '../src/wire.js';
import { NOBODY } from '../src/watch.js';
import { decode } from '../src/frame.js';
import { memorySql } from './sql.js';
import { openBucket, frame, rid, countingBucket } from './helpers.js';

const enc = new TextEncoder();

// One tenant over an identity's SQLite and R2 prefix, the way the identity object builds it, so the
// route handlers and the stats counting can be tested without the whole Durable Object around them.
const tenantOf = (identity, sql = memorySql(), options = {}) => new Tenant({
  identity, sql, bucket: env.bucket, clock: () => 0, policy: policyOf(), limits: DEFAULTS, flight: new Flight(), arm: () => {}, watchers: NOBODY,
  ...options,
});

// runRoute sends one request through the route table to a tenant, as the identity object would.
const runRoute = async (method, path, tenant, body) => {
  const route = matchRoute(method, path);
  const request = new Request('https://relay' + path, { method, headers: { range: method === 'GET' ? body?.range ?? '' : '' } });
  return route.handler({ tenant, device: 'dev', body: method === 'GET' ? undefined : body, request }, route.args);
};

let env;
before(async () => { env = await openBucket(); });
after(() => env.close());

// One identity's state is its SQLite and its R2 prefix; a "cold" store is a new object over the same two.
const open = (identity, sql = memorySql()) => ({ sql, store: new R2Store(env.bucket, sql, identity) });
const put = async (store, f, admit) => store.put(await sealFrame(f.bytes), admit);

test('stats adds durable stored totals and active limits without changing request counters or writing on a read', async () => {
  const sql = memorySql(), queries = [];
  const countedSql = { exec: (query, ...params) => (queries.push(query), sql.exec(query, ...params)) };
  const counted = countingBucket(env.bucket);
  let now = 1_000;
  const limits = { ...DEFAULTS, storeBytes: 10_000, storeObjects: 100, framesPerDay: 10 };
  const options = { bucket: counted.bucket, clock: () => now, limits };
  const t = tenantOf('id_stats_totals', countedSql, options);
  const first = frame([rid(801), rid(802)]), second = frame([rid(803)], 'second');
  await runRoute('POST', '/v1/store/frames', t, first.bytes);
  await runRoute('POST', '/v1/store/frames', t, second.bytes);
  await runRoute('POST', '/v1/store/frames', t, first.bytes);
  await runRoute('GET', '/v1/store/objects/' + rid(801), t);
  await runRoute('POST', '/v1/store/has', t, enc.encode(JSON.stringify({ rids: [rid(803)] })));

  // Reopening over the same SQLite must report stored totals independently of batched request counts.
  t.stats.flush();
  const cold = tenantOf('id_stats_totals', countedSql, options);
  const counters = t.stats.snapshot(), calls = { ...counted.calls };
  const used = { stored_bytes: first.bytes.length + second.bytes.length, stored_objects: 3, frames_today: 2 };
  const caps = { limit_bytes: 10_000, limit_objects: 100, limit_frames_per_day: 10 };
  queries.length = 0;
  const stats = async (tenant = cold) => (await runRoute('GET', '/v1/store/stats', tenant)).json();
  assert.deepEqual(await stats(), { ...counters, ...used, ...caps });
  assert.deepEqual(counters, { puts: 3, gets: 1, has: 1, bytes_in: first.bytes.length * 2 + second.bytes.length, bytes_out: first.objs[0].length });
  assert.deepEqual(await stats(tenantOf('id_stats_other')), { puts: 0, gets: 0, has: 0, bytes_in: 0, bytes_out: 0,
    stored_bytes: 0, stored_objects: 0, frames_today: 0,
    limit_bytes: DEFAULTS.storeBytes, limit_objects: DEFAULTS.storeObjects, limit_frames_per_day: DEFAULTS.framesPerDay });

  now = 86_400_000;
  assert.deepEqual(await stats(), { ...counters, ...used, frames_today: 0, ...caps });
  assert.deepEqual(sql.exec('SELECT day, frames, bytes, objects FROM usage').one(), {
    day: 0, frames: 2, bytes: used.stored_bytes, objects: 3,
  });
  assert.ok(queries.every((query) => /^SELECT\b/i.test(query)), 'stats reads only SQLite and never writes');
  assert.deepEqual(counted.calls, calls, 'stats never calls R2, including list');
});

test('stats omits every non-finite limit from JSON while preserving known zero limits', async () => {
  for (const [name, field] of [['storeBytes', 'limit_bytes'], ['storeObjects', 'limit_objects'], ['framesPerDay', 'limit_frames_per_day']]) {
    const t = tenantOf('id_stats_' + name, memorySql(), { limits: { ...DEFAULTS, [name]: Infinity } });
    const answer = await (await runRoute('GET', '/v1/store/stats', t)).json();
    assert.equal(Object.hasOwn(answer, field), false);
    for (const other of ['limit_bytes', 'limit_objects', 'limit_frames_per_day'].filter((key) => key !== field)) {
      assert.equal(typeof answer[other], 'number');
    }
    assert.equal(Object.values(answer).includes(null), false);
  }
  const t = tenantOf('id_stats_zero_limits', memorySql(), { limits: { ...DEFAULTS, storeBytes: 0, storeObjects: 0, framesPerDay: 0 } });
  const answer = await (await runRoute('GET', '/v1/store/stats', t)).json();
  assert.deepEqual([answer.limit_bytes, answer.limit_objects, answer.limit_frames_per_day], [0, 0, 0]);
});

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

test('getMany answers a prefix in request order, reading each frame once', async () => {
  const { store } = open('id_many');
  const a = frame([rid(1), rid(2), rid(3)]);
  const b = frame([rid(4), rid(5)]);
  await put(store, a);
  await put(store, b);
  const got = await store.getMany([rid(5), rid(1), rid(3), rid(4)]);
  assert.deepEqual(got.map((o) => o.rid), [rid(5), rid(1), rid(3), rid(4)]);
  assert.deepEqual(got.map((o) => o.bytes), [b.objs[1], a.objs[0], a.objs[2], b.objs[0]]);
});

test('getMany stops at the first rid the index does not hold', async () => {
  const { store } = open('id_many_gap');
  await put(store, frame([rid(1), rid(2)]));
  assert.deepEqual((await store.getMany([rid(1), rid(9), rid(2)])).map((o) => o.rid), [rid(1)]);
  assert.deepEqual(await store.getMany([rid(9), rid(1)]), []);
});

test('getMany costs one R2 read per frame touched, not one per object', async () => {
  const { sql, store } = open('id_many_cost');
  await put(store, frame([rid(1), rid(2), rid(3), rid(4)]));
  const counted = countingBucket(env.bucket);
  const cold = new R2Store(counted.bucket, sql, 'id_many_cost');
  await cold.getMany([rid(4), rid(1), rid(3), rid(2)]);
  assert.equal(counted.calls.get, 1);
});


test('getFrame answers the frame bytes as a stream, whole or as the slice one Range asks for', async () => {
  const { sql, store } = open('id_frame');
  const f = frame([rid(1), rid(2), rid(3)]);
  await put(store, f);
  const t = tenantOf('id_frame', sql);
  const id = (await sealFrame(f.bytes)).id;
  const whole = await t.getFrame(id);
  assert.equal(whole.body instanceof ReadableStream, true, 'the body streams, it is not a buffer');
  assert.equal(whole.size, f.bytes.length);
  assert.deepEqual(new Uint8Array(await new Response(whole.body).arrayBuffer()), f.bytes);
  const slice = await t.getFrame(id, { offset: 4, length: 5 });
  assert.equal(slice.served, 5);
  assert.deepEqual(new Uint8Array(await new Response(slice.body).arrayBuffer()), f.bytes.subarray(4, 9));
  assert.deepEqual(t.stats.snapshot(), { puts: 0, gets: 2, has: 0, bytes_in: 0, bytes_out: f.bytes.length + 5 });
});

test('GET /v1/store/frames/<id> answers the exact bytes, and a Range header answers 206 with content-range', async () => {
  const { sql, store } = open('id_frame_route');
  const f = frame([rid(1)]);
  await put(store, f);
  const t = tenantOf('id_frame_route', sql);
  const id = (await sealFrame(f.bytes)).id;
  const whole = await runRoute('GET', '/v1/store/frames/' + id, t);
  assert.equal(whole.status, 200);
  assert.equal(whole.headers.get('content-length'), String(f.bytes.length));
  assert.deepEqual(new Uint8Array(await whole.arrayBuffer()), f.bytes);
  const ranged = await runRoute('GET', '/v1/store/frames/' + id, t, { range: 'bytes=2-5' });
  assert.equal(ranged.status, 206);
  assert.equal(ranged.headers.get('content-range'), `bytes 2-5/${f.bytes.length}`);
  assert.equal(ranged.headers.get('content-length'), '4');
  assert.deepEqual(new Uint8Array(await ranged.arrayBuffer()), f.bytes.subarray(2, 6));
});

test('a frame get is refused as the contract says: malformed id 400, unknown 404, unsatisfiable range 416', async () => {
  const { sql, store } = open('id_frame_refuse');
  const f = frame([rid(1)]);
  await put(store, f);
  const t = tenantOf('id_frame_refuse', sql);
  const id = (await sealFrame(f.bytes)).id;
  await assert.rejects(runRoute('GET', '/v1/store/frames/zz', t), (e) => e instanceof Wire && e.code === 'bad_rid' && e.status === 400);
  await assert.rejects(runRoute('GET', '/v1/store/frames/' + 'a'.repeat(64), t), (e) => e instanceof Wire && e.code === 'not_found' && e.status === 404);
  for (const range of ['bytes=999999-', 'bytes=5-3', 'bytes=abc', 'bytes=0-1,3-4']) {
    await assert.rejects(runRoute('GET', '/v1/store/frames/' + id, t, { range }), (e) => e instanceof Wire && e.code === 'range_not_satisfiable' && e.status === 416, range);
  }
  const openEnded = await runRoute('GET', '/v1/store/frames/' + id, t, { range: 'bytes=1-' });
  assert.equal(openEnded.status, 206);
  assert.equal(openEnded.headers.get('content-length'), String(f.bytes.length - 1));
});

test('locate answers where held rids live, matching the frame header; an unheld rid is absent; it counts one has', async () => {
  const { sql, store } = open('id_locate');
  const f = frame([rid(1), rid(2), rid(3)]);
  await put(store, f);
  const t = tenantOf('id_locate', sql);
  const id = (await sealFrame(f.bytes)).id;
  const answer = await runRoute('POST', '/v1/store/locate', t, enc.encode(JSON.stringify({ rids: [rid(1), rid(9), rid(3)] })));
  const at = (await answer.json()).at;
  const decoded = decode(f.bytes);
  for (const { rid: r, off, len } of decoded.objects) {
    if (r === rid(2)) continue;
    assert.deepEqual(at[r], { frame: id, off: decoded.base + off, len }, r);
  }
  assert.equal(at[rid(9)], undefined);
  assert.equal(at[rid(2)], undefined, 'a rid not asked about is not answered');
  assert.deepEqual(t.stats.snapshot(), { puts: 0, gets: 0, has: 1, bytes_in: 0, bytes_out: 0 });
});

test('locate over a thousand rids is too_many', async () => {
  const t = tenantOf('id_locate_many');
  await assert.rejects(
    runRoute('POST', '/v1/store/locate', t, enc.encode(JSON.stringify({ rids: Array.from({ length: 1001 }, (_, i) => rid(i)) }))),
    (e) => e instanceof Wire && e.code === 'too_many' && e.status === 400,
  );
});

// R21 exercises the actual local workerd R2 backend: storage failure cannot
// produce upload evidence, and retry repairs the immutable bytes and index.
test('R21 upload references become confirmable only after local R2 and index completion', async () => {
  let fail = true;
  const bucket = {
    async put(...args) { if (fail) throw new Error('injected R2 failure'); return env.bucket.put(...args); },
    get(...args) { return env.bucket.get(...args); },
  };
  const sql = memorySql(), store = new R2Store(bucket, sql, 'id_r21');
  const f = frame([rid(21), rid(22)]), sealed = await sealFrame(f.bytes);
  await assert.rejects(store.put(sealed), /injected R2 failure/);
  await assert.rejects(store.checkReferences(rid(21), [sealed.id], async () => {}));
  fail = false;
  assert.equal(await store.put(sealed), true);
  let completed = false;
  await store.checkReferences(rid(21), [sealed.id], async () => { completed = true; });
  assert(completed);
  const cold = new R2Store(bucket, sql, 'id_r21');
  await cold.checkReferences(rid(21), [], async () => {});
  assert.deepEqual(await cold.get(rid(22)), f.objs[1]);
});
