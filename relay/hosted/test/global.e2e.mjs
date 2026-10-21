// C20 proves the operator brakes on a real Worker: admission, storage, reads and owner erasure.
// Run on a fresh local relay with globalNewIdentitiesPerDay=2, globalStoreBytes=512 and minGraceMs=2000.
import assert from 'node:assert/strict';
import { newIdentity, newDevice } from './party.js';
import { frame, rid } from './helpers.js';
import { call } from './client.js';

const device = async () => newDevice(await newIdentity());
const [a, b, c] = await Promise.all([device(), device(), device()]);
for (const d of [a, b]) assert.equal((await call(d, 'GET', '/v1/dir/list')).status, 200);
const denied = await call(c, 'GET', '/v1/dir/list');
assert.equal(denied.status, 429);
assert.equal(denied.json.err, 'relay_admission_full');
assert.ok(Number(denied.headers.get('retry-after')) > 0);

const f = frame([rid(1)], 'x'.repeat(250));
assert.ok(f.bytes.length <= 512 && f.bytes.length * 2 > 512);
const put = (d) => call(d, 'POST', '/v1/store/frames', f.bytes);
assert.equal((await put(a)).status, 200);
const full = await put(b);
assert.equal(full.status, 507);
assert.equal(full.json.err, 'relay_storage_full');
assert.equal((await put(a)).status, 200, 'an existing frame is free to retry');
const read = await call(a, 'GET', '/v1/store/objects/' + rid(1));
assert.equal(read.status, 200);
assert.deepEqual(read.buf, f.objs[0]);
assert.equal((await call(a, 'POST', '/v1/identity/rotation', { op: 'freeze' })).status, 200);
assert.equal((await call(a, 'POST', '/v1/identity/rotation', { op: 'retire', grace_ms: 2000 })).status, 200);
const deadline = Date.now() + 40_000;
while ((await call(a, 'GET', '/v1/dir/list')).status !== 410) {
  assert.ok(Date.now() < deadline, 'owner erasure did not finish');
  await new Promise((resolve) => setTimeout(resolve, 100));
}
// The tombstone precedes the paged sweep, so wait for capacity rather than treating 410 as the sweep's completion.
while ((await put(b)).status !== 200) {
  assert.ok(Date.now() < deadline, 'erasure did not return global capacity');
  await new Promise((resolve) => setTimeout(resolve, 100));
}
console.log('C20: global admission, storage, reads, retries and owner erasure pass');
