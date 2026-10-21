// The pairing mailbox (contract 18.4) end to end, one test per row of its edge-case table that a
// relay decides. Runs against a relay started with short limits: a 3 s TTL, 4 creates an hour,
// 10 writes a minute, 2 open polls and 4 live boxes per the numbers GET /v1/pair/limits returns.
// Each test comes from its own IP (the header a Cloudflare edge would set), so limits never bleed.
import assert from 'node:assert/strict';
import { randomBytes } from 'node:crypto';

import { TIGHT as BASE } from './client.js';
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const key = () => randomBytes(16).toString('base64url');
let nextIp = 10;
const freshIp = () => `10.9.0.${nextIp++}`;

async function send(method, path, { ip = freshIp(), pairKey, body, gen } = {}) {
  const headers = { 'cf-connecting-ip': ip, ...(pairKey ? { 'codeaf-pair-key': pairKey } : {}), ...(gen ? { 'codeaf-pair-gen': gen } : {}) };
  const res = await fetch(BASE + path, { method, headers, body });
  const text = await res.text();
  return { status: res.status, retry: res.headers.get('retry-after'), gen: res.headers.get('codeaf-pair-gen'), json: text.startsWith('{') ? JSON.parse(text) : null };
}

const limits = (await (await fetch(BASE + '/v1/pair/limits')).json());

async function box(ip = freshIp()) {
  const a = key();
  const made = await send('POST', '/v1/pair', { ip, pairKey: a });
  assert.equal(made.status, 201, `create answered ${made.status}`);
  const np = made.json.nameplate;
  return { np, a, ip, gen: made.gen, close: () => send('DELETE', `/v1/pair/${np}`, { pairKey: a }) };
}

const tests = {
  async Limits() {
    assert.deepEqual(Object.keys(limits).sort(), ['create_per_hour', 'max_msg', 'max_msgs_per_side', 'ttl_ms', 'write_per_minute']);
    assert.deepEqual(limits, { ttl_ms: 3000, max_msg: 4096, max_msgs_per_side: 4, create_per_hour: 4, write_per_minute: 10 });
  },

  async NoListing() {
    for (const path of ['/v1/pair', '/v1/pair/', '/v1/pair/x', '/v1/pair/12345/a']) {
      assert.equal((await send('GET', path)).status, 404, path);
    }
  },

  async Opaque() {
    const b = await box();
    const all = Uint8Array.from({ length: 256 }, (_, i) => i);
    assert.deepEqual((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: all })).json, { n: 0 });
    const got = (await send('GET', `/v1/pair/${b.np}/a?after=0`)).json;
    assert.deepEqual(new Uint8Array(Buffer.from(got.msgs[0], 'base64')), all);
    assert.equal(got.next, 1);
    assert.equal(JSON.stringify(got).includes(b.a), false, 'no key in an answer');
    await b.close();
  },

  async ReadNeedsNoKeyAndAfterSkips() {
    const b = await box();
    for (const m of ['one', 'two']) await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: m });
    const after = (await send('GET', `/v1/pair/${b.np}/a?after=1`)).json;
    assert.deepEqual([after.msgs.map((m) => Buffer.from(m, 'base64').toString()), after.next], [['two'], 2]);
    await b.close();
  },

  async WrongKey() {
    const b = await box();
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: key(), body: 'x' })).status, 403);
    assert.equal((await send('DELETE', `/v1/pair/${b.np}`, { pairKey: key() })).status, 403);
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { body: 'x' })).status, 403, 'no key at all owns nothing');
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: 'short', body: 'x' })).status, 403, 'nor does a malformed one');
    await b.close();
  },

  async ClaimRace() {
    const b = await box();
    const tries = await Promise.all(Array.from({ length: 20 }, () => send('POST', `/v1/pair/${b.np}/b`, { pairKey: key(), body: 'hi' })));
    assert.deepEqual(tries.map((t) => t.status).sort(), [201, ...Array(19).fill(403)]);
    await b.close();
  },

  async SideFull() {
    const b = await box();
    const ip = freshIp();
    const statuses = [];
    for (let i = 0; i < 5; i++) statuses.push((await send('POST', `/v1/pair/${b.np}/a`, { ip, pairKey: b.a, body: 'm' })).status);
    assert.deepEqual(statuses, [201, 201, 201, 201, 409]);
    await b.close();
  },

  async MessageTooBig() {
    const b = await box();
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: new Uint8Array(limits.max_msg) })).status, 201);
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: new Uint8Array(limits.max_msg + 1) })).status, 413);
    await b.close();
  },

  async LongPoll() {
    const b = await box();
    const t0 = Date.now();
    assert.equal((await send('GET', `/v1/pair/${b.np}/a?wait=1200`)).status, 204);
    assert.ok(Date.now() - t0 >= 1000, 'an empty poll waits');
    const waiting = send('GET', `/v1/pair/${b.np}/a?wait=2500`);
    await sleep(300);
    await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: 'now' });
    const t1 = Date.now();
    assert.equal((await waiting).status, 200);
    assert.ok(Date.now() - t1 < 1000, 'a post ends the wait at once');
    await b.close();
  },

  async DeleteWakesPoll() {
    const b = await box();
    const waiting = send('GET', `/v1/pair/${b.np}/b?wait=2500`);
    await sleep(300);
    assert.equal((await b.close()).status, 204);
    assert.equal((await waiting).status, 404);
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: 'x' })).status, 404);
  },

  async UnknownIs404() {
    assert.equal((await send('GET', '/v1/pair/9999/a')).status, 404);
    assert.equal((await send('POST', '/v1/pair/9999/b', { pairKey: key(), body: 'x' })).status, 404);
    assert.equal((await send('DELETE', '/v1/pair/9999', { pairKey: key() })).status, 404);
  },

  async ExpiryDeletes() {
    const b = await box();
    await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: 'x' });
    await sleep(limits.ttl_ms + 500);
    assert.equal((await send('GET', `/v1/pair/${b.np}/a`)).status, 404);
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: 'x' })).status, 404);
  },

  async NameplatesDistinct() {
    const boxes = await Promise.all([box(), box(), box()]);
    assert.equal(new Set(boxes.map((b) => b.np)).size, 3);
    await Promise.all(boxes.map((b) => b.close()));
  },

  async GenerationIsTold() {
    const b = await box();
    assert.match(b.gen, /^[0-9a-f]{32}$/, 'create tells the generation');
    const posted = await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: 'x' });
    const polled = await send('GET', `/v1/pair/${b.np}/a?after=1`);
    assert.deepEqual([posted.gen, polled.status, polled.gen], [b.gen, 204, b.gen], 'a joining device learns it from any answer');
    assert.equal((await send('GET', `/v1/pair/${b.np}/a`)).json.msgs.length, 1);
    await b.close();
  },

  async GenerationFences() {
    const b = await box();
    const other = '0'.repeat(32);
    for (const [method, path, extra] of [
      ['GET', `/v1/pair/${b.np}/a?after=0`, {}],
      ['POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: 'x' }],
      ['DELETE', `/v1/pair/${b.np}`, { pairKey: b.a }],
    ]) {
      for (const gen of [other, 'short', '!']) {
        const r = await send(method, path, { ...extra, gen });
        assert.deepEqual([r.status, r.json?.err], [404, 'gone'], `${method} with generation ${gen}`);
      }
    }
    assert.equal((await send('GET', `/v1/pair/${b.np}/a?after=0`, { gen: b.gen })).status, 204, 'the right generation is let in');
    assert.equal((await send('POST', `/v1/pair/${b.np}/a`, { pairKey: b.a, body: 'x', gen: b.gen })).status, 201);
    assert.equal((await send('GET', `/v1/pair/${b.np}/a?after=0`)).status, 200, 'a device with none is let in');
    assert.equal((await b.close()).status, 204);
  },

  async CreateRateLimited() {
    const ip = freshIp();
    const made = [];
    for (let i = 0; i < limits.create_per_hour; i++) made.push(await box(ip));
    const over = await send('POST', '/v1/pair', { ip, pairKey: key() });
    assert.deepEqual([over.status, over.json.err], [429, 'slow down']);
    assert.ok(Number(over.retry) > 0, 'Retry-After names the wait');
    await made[0].close(); // the relay holds four boxes at most; make room, so only the IP is in question
    assert.equal((await send('POST', '/v1/pair', { pairKey: key() })).status, 201, 'another IP is unaffected');
    await Promise.all(made.slice(1).map((b) => b.close()));
  },

  async WriteRateLimited() {
    const b = await box();
    const ip = freshIp();
    const statuses = [];
    for (let i = 0; i < limits.write_per_minute + 1; i++) statuses.push((await send('POST', `/v1/pair/${b.np}/b`, { ip, pairKey: b.a, body: 'w' })).status);
    assert.equal(statuses.at(-1), 429);
    assert.ok(!statuses.slice(0, -1).includes(429));
    await b.close();
  },

  async PollsAreLimited() {
    const b = await box();
    const ip = freshIp();
    const polls = [1, 2, 3].map(() => send('GET', `/v1/pair/${b.np}/b?wait=1500`, { ip }));
    assert.deepEqual((await Promise.all(polls)).map((p) => p.status).sort(), [204, 204, 429]);
    await b.close();
  },

  async RelayFull() {
    await sleep(limits.ttl_ms + 200); // let earlier boxes go, so the count starts from empty
    const boxes = [];
    for (let i = 0; i < 4; i++) boxes.push(await box());
    const full = await send('POST', '/v1/pair', { pairKey: key() });
    assert.deepEqual([full.status, full.json.err], [503, 'full']);
    await boxes[0].close();
    assert.equal((await send('POST', '/v1/pair', { pairKey: key() })).status, 201, 'a closed box frees its place');
  },
};

for (const [name, run] of Object.entries(tests)) {
  await run();
  console.log('pair ok', name);
}
console.log('pair: all pass');
