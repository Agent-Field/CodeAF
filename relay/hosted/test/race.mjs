// HTTP-level proof against a running relay (either option): 50 devices race for one free cell,
// then 50 publishes race on one old_head, then a stale fence and a forced takeover.
import assert from 'node:assert/strict';
import { newIdentity, newDevice, signed } from './party.js';
import { init } from './helpers.js';

const BASE = process.env.RELAY ?? 'http://127.0.0.1:18791';
const enc = new TextEncoder();
async function call(dev, method, path, body) {
  const bytes = body === undefined ? new Uint8Array(0) : enc.encode(JSON.stringify(body));
  const res = await fetch(BASE + path, { method, headers: await signed(dev, method, path, bytes), body: method === 'GET' ? undefined : bytes });
  const text = await res.text();
  return { status: res.status, json: text ? JSON.parse(text) : null };
}
const tally = (rs) => rs.reduce((m, r) => ((m[r.json?.err ?? r.status] = (m[r.json?.err ?? r.status] ?? 0) + 1), m), {});

const id = await newIdentity();
const devs = await Promise.all(Array.from({ length: 51 }, () => newDevice(id)));
const [owner, ...rivals] = devs;
const made = await call(owner, 'POST', '/v1/dir/cells/race', init());
await call(owner, 'POST', '/v1/dir/cells/race/release', { fence: made.json.cell.lease.fence });
const won = tally(await Promise.all(rivals.map((d) => call(d, 'POST', '/v1/dir/cells/race/acquire', {}))));
assert.deepEqual(won, { 200: 1, lease_held: 49 });
console.log('acquire race', JSON.stringify(won));

const c2 = await call(owner, 'POST', '/v1/dir/cells/heads', init());
const fence = c2.json.cell.lease.fence;
const pubs = tally(await Promise.all(Array.from({ length: 50 }, (_, i) => call(owner, 'POST', '/v1/dir/cells/heads/publish', { fence, old_head: 'h0', head: `h${i + 1}`, size: 1, class: 'work', pending: 0 }))));
assert.deepEqual(pubs, { 200: 1, head_moved: 49 });
console.log('head race', JSON.stringify(pubs));

const t0 = performance.now();
const took = await call(rivals[0], 'POST', '/v1/dir/cells/heads/acquire', { force: true });
const ms = Math.round(performance.now() - t0);
assert.equal(took.json.cell.lease.fence, fence + 1);
const stale = await call(owner, 'POST', '/v1/dir/cells/heads/publish', { fence, old_head: 'h0', head: 'hx', size: 1, class: 'work', pending: 0 });
assert.equal(stale.json.err, 'fence_stale');
console.log(`forced takeover ${ms} ms, stale fence refused`);
