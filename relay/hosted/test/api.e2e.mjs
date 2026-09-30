// The wires end to end against `wrangler dev --local` (default http://127.0.0.1:18791):
// every route, every refusal, isolation. Not part of `npm test`; `npm run e2e` starts the relay.
import assert from 'node:assert/strict';
import { newIdentity, newDevice, deviceId } from './party.js';
import { frame, rid, init } from './helpers.js';
import { BASE, call, answerOf } from './client.js';

const enc = new TextEncoder();
const id = await newIdentity();
const a = await newDevice(id);
const b = await newDevice(id);
const stranger = await newDevice(await newIdentity());

// Store: put, has, get, isolation, and every refusal the contract names.
const f = frame([rid(1), rid(2)]);
let r = await call(a, 'POST', '/v1/store/frames', f.bytes);
assert.equal(r.status, 200);
assert.equal(r.json.objects, 2);
assert.ok(r.headers.get('codeaf-now'), 'Codeaf-Now on every answer');
assert.deepEqual((await call(a, 'POST', '/v1/store/has', { rids: [rid(1), rid(9)] })).json, { have: [true, false] });
assert.deepEqual((await call(a, 'GET', '/v1/store/objects/' + rid(2))).buf, f.objs[1]);
assert.equal((await call(stranger, 'GET', '/v1/store/objects/' + rid(2))).status, 404, 'identity isolation');
assert.equal(answerOf(await call(a, 'POST', '/v1/store/frames', enc.encode('junk'))), 'bad_frame');
assert.equal(answerOf(await call(a, 'GET', '/v1/store/objects/zz')), 'bad_rid');
assert.equal(answerOf(await call(a, 'POST', '/v1/store/has', { rids: ['zz'] })), 'bad_rid');
assert.equal(answerOf(await call(a, 'POST', '/v1/store/has', { rids: Array(1001).fill(rid(1)) })), 'too_many');
assert.equal(answerOf(await call(a, 'POST', '/v1/store/has', enc.encode('[]'))), 'bad_request');
assert.equal(answerOf(await call(a, 'POST', '/v1/store/frames', frame([rid(1)], 'other').bytes)), 'conflict');
assert.equal((await call(a, 'GET', '/v1/store/stats')).json.puts, 3, 'the relay counts the puts it was asked for');

// An oversized frame is 413 whether or not the client declared its length, never 400.
const big = new Uint8Array((16 << 20) + 1);
assert.equal((await call(a, 'POST', '/v1/store/frames', big)).status, 413);
const streamed = await fetch(BASE + '/v1/store/frames', { method: 'POST', body: new ReadableStream({ start(c) { c.enqueue(big); c.close(); } }), duplex: 'half' });
assert.equal(streamed.status, 401, 'an unsigned stream is refused as unsigned, having cost the relay at most one chunk of reading');

// Directory: create, lease, takeover, fence, release, list, devices.
assert.equal((await call(a, 'POST', '/v1/dir/cells/c1', init())).json.cell.lease.fence, 1);
assert.equal((await call(a, 'POST', '/v1/dir/cells/c1', init())).status, 409);
assert.equal(answerOf(await call(b, 'POST', '/v1/dir/cells/c1/acquire', {})), 'lease_held');
assert.equal((await call(b, 'POST', '/v1/dir/cells/c1/acquire', { force: true })).json.cell.lease.fence, 2);
const pub = (fence) => ({ fence, old_head: 'h0', head: 'h1', size: 0, class: 'work', pending: 0 });
const stale = await call(a, 'POST', '/v1/dir/cells/c1/publish', pub(1));
assert.deepEqual([stale.status, stale.json.err], [409, 'fence_stale']);
assert.equal((await call(b, 'POST', '/v1/dir/cells/c1/publish', pub(2))).status, 200);
assert.equal((await call(b, 'POST', '/v1/dir/cells/c1/release', { fence: 2 })).status, 204);
assert.equal((await call(a, 'GET', '/v1/dir/list')).json.cells.c1.head, 'h1');
assert.equal(answerOf(await call(a, 'GET', '/v1/dir/cells/nope')), 'not_found');
assert.equal(answerOf(await call(a, 'PUT', '/v1/dir/devices/someone-else', {})), 'unauthorized');
assert.equal(answerOf(await call(a, 'POST', '/v1/dir/cells/c1/heartbeat', enc.encode('[1]'))), 'bad_request');
assert.equal(answerOf(await call(a, 'POST', '/v1/dir/cells/c2', new Uint8Array(1 << 20 | 1))), 'too_large');

// Authentication: unsigned, skewed both ways, and a revoked device. Every refusal carries the clock.
const unsigned = await fetch(BASE + '/v1/dir/list');
assert.deepEqual([unsigned.status, (await unsigned.json()).err], [401, 'unauthorized']);
assert.ok(Number(unsigned.headers.get('codeaf-now')) > 0, 'an unsigned refusal still tells the time');
for (const shiftMs of [6 * 60_000, -6 * 60_000]) {
  const skew = await call(a, 'GET', '/v1/dir/list', undefined, { shiftMs });
  assert.deepEqual([skew.status, skew.json.err], [401, 'skew']);
}
// Revocation: one device stops another, which is then 401 revoked on both wires. A device cannot stop
// itself or an unknown id, stopping twice is fine, and a record cannot clear or set its own flag.
const gone = await newDevice(id);
const goneId = await deviceId(gone);
const aId = await deviceId(a);
const record = { V: 1, name: '', added_by: '', revoked: false, caps: {} };
assert.equal((await call(gone, 'PUT', `/v1/dir/devices/${goneId}`, { ...record, revoked: true })).status, 204);
assert.equal((await call(a, 'PUT', `/v1/dir/devices/${aId}`, record)).status, 204);
assert.equal((await call(gone, 'GET', '/v1/dir/list')).status, 200, 'a record cannot set its own revoked flag');
assert.equal(answerOf(await call(a, 'POST', `/v1/dir/devices/${aId}/revoke`)), 'self_revoke');
assert.equal((await call(a, 'POST', `/v1/dir/devices/${aId}/revoke`)).status, 400);
assert.equal(answerOf(await call(a, 'POST', '/v1/dir/devices/dev_cccccccccccccccccccccccccccccccc/revoke')), 'not_found');
assert.equal((await call(a, 'POST', `/v1/dir/devices/${goneId}/revoke`)).status, 204);
assert.equal((await call(a, 'POST', `/v1/dir/devices/${goneId}/revoke`)).status, 204, 'revoking twice changes nothing');
for (const [method, path] of [['GET', '/v1/dir/list'], ['GET', '/v1/store/stats'], ['POST', '/v1/dir/cells/c1/acquire']]) {
  const revoked = await call(gone, method, path, method === 'POST' ? {} : undefined);
  assert.deepEqual([revoked.status, revoked.json.err], [401, 'revoked'], path);
}
assert.equal((await call(gone, 'PUT', `/v1/dir/devices/${goneId}`, record)).status, 401, 'and it cannot put itself back');
assert.equal((await call(a, 'GET', '/v1/dir/list')).json.devices[goneId].revoked, true);
assert.equal((await call(a, 'GET', '/v1/dir/list')).status, 200, 'the other devices of the identity carry on');
console.log('e2e: all pass');
