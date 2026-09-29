// Runs against `wrangler dev --local` (default http://127.0.0.1:18791). Not part of `npm test`.
import assert from 'node:assert/strict';
import { newIdentity, newDevice, signed } from './party.js';
import { frame, rid, init } from './helpers.js';

const BASE = process.env.RELAY ?? 'http://127.0.0.1:18791';
const enc = new TextEncoder();

async function call(dev, method, path, body) {
  const bytes = body === undefined ? new Uint8Array(0) : body instanceof Uint8Array ? body : enc.encode(JSON.stringify(body));
  const res = await fetch(BASE + path, { method, headers: await signed(dev, method, path, bytes), body: method === 'GET' ? undefined : bytes });
  return { status: res.status, now: res.headers.get('codeaf-now'), buf: new Uint8Array(await res.arrayBuffer()) };
}
const asJson = (r) => (r.buf.length ? JSON.parse(new TextDecoder().decode(r.buf)) : null);

const id = await newIdentity();
const a = await newDevice(id);
const b = await newDevice(id);
const stranger = await newDevice(await newIdentity());

const f = frame([rid(1), rid(2)]);
let r = await call(a, 'POST', '/v1/store/frames', f.bytes);
assert.equal(r.status, 200); assert.equal(asJson(r).objects, 2); assert.ok(r.now, 'Codeaf-Now on every answer');
r = await call(a, 'POST', '/v1/store/has', { rids: [rid(1), rid(9)] });
assert.deepEqual(asJson(r), { have: [true, false] });
r = await call(a, 'GET', '/v1/store/objects/' + rid(2));
assert.deepEqual(r.buf, f.objs[1]);
assert.equal((await call(stranger, 'GET', '/v1/store/objects/' + rid(2))).status, 404, 'identity isolation');
assert.equal((await call(a, 'POST', '/v1/store/frames', enc.encode('junk'))).status, 400);
assert.equal(asJson(await call(a, 'POST', '/v1/store/frames', enc.encode('junk'))).err, 'bad_frame');
assert.equal((await call(a, 'GET', '/v1/store/objects/zz')).status, 400);

const created = asJson(await call(a, 'POST', '/v1/dir/cells/c1', init()));
assert.equal(created.cell.lease.fence, 1);
assert.equal((await call(a, 'POST', '/v1/dir/cells/c1', init())).status, 409);
assert.equal(asJson(await call(b, 'POST', '/v1/dir/cells/c1/acquire', {})).err, 'lease_held');
const took = asJson(await call(b, 'POST', '/v1/dir/cells/c1/acquire', { force: true }));
assert.equal(took.cell.lease.fence, 2);
const stale = await call(a, 'POST', '/v1/dir/cells/c1/publish', { fence: 1, old_head: 'h0', head: 'h1', size: 0, class: 'work', pending: 0 });
assert.equal(asJson(stale).err, 'fence_stale'); assert.equal(stale.status, 409);
assert.equal((await call(b, 'POST', '/v1/dir/cells/c1/publish', { fence: 2, old_head: 'h0', head: 'h1', size: 0, class: 'work', pending: 0 })).status, 200);
assert.equal((await call(b, 'POST', '/v1/dir/cells/c1/release', { fence: 2 })).status, 204);
const list = asJson(await call(a, 'GET', '/v1/dir/list'));
assert.equal(list.cells.c1.head, 'h1');
assert.equal((await call(a, 'PUT', '/v1/dir/devices/someone-else', {})).status, 401);

const big = await fetch(BASE + '/v1/store/frames', { method: 'POST', headers: { ...(await signed(a, 'POST', '/v1/store/frames')), 'content-length': String((16 << 20) + 1) }, body: new Uint8Array((16 << 20) + 1) }).catch(() => null);
assert.equal(big?.status, 413, 'frame over MaxFrame');
console.log('e2e: all pass');
