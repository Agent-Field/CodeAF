// HTTP-level proof against a running relay: the identity Durable Object is the one writer, so 50 devices race for one free cell,
// then 50 publishes race on one old_head, then a stale fence and a forced takeover.
import assert from 'node:assert/strict';
import { newIdentity, newDevice } from './party.js';
import { init } from './helpers.js';
import { call } from './client.js';

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
