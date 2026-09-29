import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { R2Directory } from '../src/r2dir.js';
import { openBucket, init } from './helpers.js';

let env;
before(async () => { env = await openBucket(); });
after(() => env.close());

const dirAt = (identity, clock = Date.now) => new R2Directory(env.bucket, identity, clock);
const code = (p) => p.then(() => 'ok', (e) => e.code);
const tally = (codes) => codes.reduce((m, c) => ((m[c] = (m[c] ?? 0) + 1), m), {});

test('50 devices race to acquire one free cell: exactly one winner', async () => {
  const d = dirAt('id_race');
  const made = await d.create('c1', init(), 'dev_0');
  await d.release('c1', 'dev_0', made.cell.lease.fence);
  const results = await Promise.all(Array.from({ length: 50 }, (_, i) => code(dirAt('id_race').acquire('c1', `dev_${i + 1}`))));
  assert.deepEqual(tally(results), { ok: 1, lease_held: 49 });
  const { cell } = await d.cell('c1');
  assert.equal(cell.lease.fence, 2);
});

test('50 concurrent creates of one id: one wins, 49 exists', async () => {
  const results = await Promise.all(Array.from({ length: 50 }, (_, i) => code(dirAt('id_create').create('c', init(`h${i}`), `dev_${i}`))));
  assert.deepEqual(tally(results), { ok: 1, exists: 49 });
});

test('50-way head race from one holder: exactly one publish moves the head', async () => {
  const d = dirAt('id_head');
  const { cell } = await d.create('c1', init(), 'dev_a');
  const beat = (i) => ({ fence: cell.lease.fence, old_head: 'h0', head: `h${i + 1}`, size: 1, class: 'work', pending: 0 });
  const results = await Promise.all(Array.from({ length: 50 }, (_, i) => code(dirAt('id_head').publish('c1', 'dev_a', beat(i)))));
  assert.deepEqual(tally(results), { ok: 1, head_moved: 49 });
});

test('a stale fence is rejected after takeover, and the new holder publishes', async () => {
  const d = dirAt('id_fence');
  const a = (await d.create('c1', init(), 'dev_a')).cell.lease;
  const b = (await d.acquire('c1', 'dev_b', true)).cell.lease;
  assert.equal(b.fence, a.fence + 1);
  assert.equal(await code(d.publish('c1', 'dev_a', { fence: a.fence, old_head: 'h0', head: 'h1', size: 0, class: 'work', pending: 0 })), 'fence_stale');
  assert.equal(await code(d.heartbeat('c1', 'dev_a', { fence: a.fence, pending: 0 })), 'fence_stale');
  assert.equal(await code(d.publish('c1', 'dev_b', { fence: b.fence, old_head: 'h0', head: 'h1', size: 0, class: 'work', pending: 0 })), 'ok');
  assert.equal((await d.cell('c1')).cell.head, 'h1');
});

test('takeover is immediate: a live lease refuses a plain acquire and yields to a forced one with no wait', async () => {
  const d = dirAt('id_take');
  await d.create('c1', init(), 'dev_a');
  assert.equal(await code(d.acquire('c1', 'dev_b')), 'lease_held');
  const t0 = performance.now();
  const view = await d.acquire('c1', 'dev_b', true);
  assert.ok(performance.now() - t0 < 1000, 'no TTL wait');
  assert.equal(view.cell.lease.device, 'dev_b');
});

test('an expired lease is taken without force (unchanged Stage 1 rule)', async () => {
  let t = 1_000_000;
  const d = dirAt('id_exp', () => t);
  await d.create('c1', init(), 'dev_a');
  t += 31_000;
  assert.equal(await code(d.acquire('c1', 'dev_b')), 'ok');
});

test('list answers every cell in one list call, no per-cell read', async () => {
  const d = dirAt('id_list');
  for (let i = 0; i < 5; i++) await d.create(`c${i}`, init(), 'dev_a');
  await d.putDevice('dev_a', { V: 1, name: 'n' });
  await d.setVault('', 'rid1');
  const seen = [];
  const spy = new Proxy(env.bucket, { get: (t, k) => (typeof t[k] === 'function' ? (...a) => (seen.push(k), t[k](...a)) : t[k]) });
  const l = await new R2Directory(spy, 'id_list').list();
  assert.deepEqual(seen, ['list']);
  assert.equal(Object.keys(l.cells).length, 5);
  assert.equal(l.identity.vault, 'rid1');
  assert.ok(l.devices.dev_a);
});

test('vault swap is a compare-and-swap', async () => {
  const d = dirAt('id_vault');
  await d.setVault('', 'a');
  assert.equal(await code(d.setVault('', 'b')), 'cas');
  await d.setVault('a', 'b');
});

test('identities never see each other', async () => {
  await dirAt('id_one').create('shared', init(), 'dev_a');
  assert.equal(await code(dirAt('id_two').cell('shared')), 'not_found');
});
