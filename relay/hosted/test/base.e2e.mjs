// A relay served under a base path (`wrangler dev --var BASE_PATH:/fabric`, default
// http://127.0.0.1:18798/fabric): every wire answers beneath the prefix and nowhere else, and the
// signature covers the whole path the client sent.
import assert from 'node:assert/strict';
import { newIdentity, newDevice, signed } from './party.js';
import { frame, rid, init } from './helpers.js';
import { call, answerOf } from './client.js';

const PREFIXED = process.env.RELAY_BASE ?? 'http://127.0.0.1:18798/fabric';
const ROOT = new URL(PREFIXED).origin;
const dev = await newDevice(await newIdentity());
const get = (path, init) => fetch(path, init).then(async (r) => ({ status: r.status, text: await r.text() }));

// Store and directory under the prefix.
const f = frame([rid(1)]);
assert.equal((await call(dev, 'POST', '/v1/store/frames', f.bytes, { base: PREFIXED })).status, 200);
assert.deepEqual((await call(dev, 'GET', '/v1/store/objects/' + rid(1), undefined, { base: PREFIXED })).buf, f.objs[0]);
assert.equal((await call(dev, 'POST', '/v1/dir/cells/c1', init(), { base: PREFIXED })).status, 200);
assert.equal((await call(dev, 'GET', '/v1/dir/list', undefined, { base: PREFIXED })).status, 200);

// The signature covers the prefix: a request signed for the bare path is refused, not served.
const bare = await signed(dev, 'GET', '/v1/dir/list');
assert.equal(answerOf({ status: 0, json: JSON.parse((await get(PREFIXED + '/v1/dir/list', { headers: bare })).text) }), 'unauthorized');

// Outside the prefix the relay does not exist.
for (const path of ['/v1/pair/limits', '/v1/store/stats', '/fabricx/v1/pair/limits', '/fabric']) {
  assert.equal((await get(ROOT + path)).status, 404, path);
}

// Pairing, unauthenticated, works beneath the prefix.
const limits = await get(PREFIXED + '/v1/pair/limits');
assert.equal(limits.status, 200);
assert.ok(JSON.parse(limits.text).ttl_ms > 0);
const made = await fetch(PREFIXED + '/v1/pair', { method: 'POST', headers: { 'codeaf-pair-key': Buffer.from('0123456789abcdef').toString('base64url'), 'cf-connecting-ip': '10.8.0.1' } });
assert.equal(made.status, 201);

// A signed WebSocket upgrade works beneath the prefix.
const headers = await signed(dev, 'GET', new URL(PREFIXED).pathname + '/v1/dir/watch');
const ws = new WebSocket(PREFIXED.replace(/^http/, 'ws') + '/v1/dir/watch', { headers });
const first = await new Promise((done, fail) => {
  ws.addEventListener('message', (e) => done(e.data));
  ws.addEventListener('error', () => fail(new Error('upgrade refused')));
});
assert.match(first, /^\{"v":\d+\}$/);
ws.close();
console.log('base path e2e: ok');
