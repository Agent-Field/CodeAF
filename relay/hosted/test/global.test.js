import { test } from 'node:test';
import assert from 'node:assert/strict';
import { GlobalBudget } from '../src/global.js';
import { DEFAULTS, limitsOf } from '../src/limits.js';
import { Tenant } from '../src/tenant.js';
import { Flight } from '../src/flight.js';
import { policyOf } from '../src/rules.js';
import { NOBODY } from '../src/watch.js';
import { memorySql } from './sql.js';
import { frame, rid, openBucket } from './helpers.js';

const empty = { list: async () => ({ objects: [], truncated: false }) };
const refusal = (code, status) => (e) => e.code === code && e.status === status;
const limits = (overrides) => ({ ...DEFAULTS, ...overrides });

test('deployment defaults allow admission and storage above the former global limits', async () => {
  const sql = memorySql();
  const budget = new GlobalBudget(sql, empty, limitsOf({}));
  await budget.reserve('a', 'large', 2 ** 41);
  for (let i = 0; i <= 10_000; i++) await budget.admit(`network-${i}`, 0);
  await new GlobalBudget(sql, empty, limitsOf({})).admit('another', 0);
});

test('a zero shared storage override closes admission only', async () => {
  const sql = memorySql();
  await new GlobalBudget(sql, empty, limitsOf({})).admit('a', 0);
  const budget = new GlobalBudget(sql, empty, limitsOf({ CAF_LIMITS: '{"globalStoreBytes":0}' }));
  await assert.rejects(budget.admit('a', 0), refusal('relay_storage_full', 507));
  await budget.reserve('a', 'f', 1);
  await budget.reserve('a', 'f', 1);
  assert.equal(sql.exec('SELECT bytes FROM global_usage').one().bytes, 1);
});

test('a zero hard storage override closes all new writes but preserves retries and erasure', async () => {
  const sql = memorySql();
  await new GlobalBudget(sql, empty, limitsOf({})).reserve('a', 'held', 1);
  const budget = new GlobalBudget(sql, empty, limitsOf({ CAF_LIMITS: '{"globalHardStoreBytes":0}' }));
  await assert.rejects(budget.reserve('a', 'f', 1), refusal('relay_storage_full', 507));
  await assert.rejects(budget.reserve('b', 'f', 1), refusal('relay_storage_full', 507));
  await assert.rejects(budget.reserve('a', 'empty', 0), refusal('relay_storage_full', 507));
  await budget.reserve('a', 'held', 1);
  await budget.forget('a');
  assert.equal(sql.exec('SELECT bytes FROM global_usage').one().bytes, 0);
  await assert.rejects(budget.reserve('a', 'f', 1), refusal('relay_storage_full', 507));
});

test('existing identities grow past shared storage and keep admission closed until erasure', async () => {
  const sql = memorySql(), cap = limits({ globalStoreBytes: 10 });
  const budget = new GlobalBudget(sql, empty, cap);
  await budget.admit('a', 0);
  await budget.admit('b', 0);
  await budget.reserve('a', 'first', 6);
  await budget.reserve('b', 'first', 4);
  await assert.rejects(budget.admit('new', 0), refusal('relay_storage_full', 507));
  const cold = new GlobalBudget(sql, empty, cap);
  await cold.reserve('a', 'second', 7);
  await cold.reserve('a', 'second', 7);
  assert.equal(sql.exec('SELECT bytes FROM global_usage').one().bytes, 17);
  assert.equal(sql.exec('SELECT COUNT(*) AS n FROM global_frames').one().n, 3);
  await assert.rejects(cold.admit('new', 0), refusal('relay_storage_full', 507));
  await cold.forget('a');
  await cold.forget('a');
  assert.equal(sql.exec('SELECT bytes FROM global_usage').one().bytes, 4);
  await cold.admit('new', 0);
});

// C20 bounds successful new identities across networks and keeps the count after restart.
test('global admission has one durable daily ceiling across networks', async () => {
  const sql = memorySql(), cap = limits({ globalNewIdentitiesPerDay: 2 });
  const budget = new GlobalBudget(sql, empty, cap);
  await budget.admit('a', 0);
  await budget.admit('b', 100);
  const cold = new GlobalBudget(sql, empty, cap);
  await assert.rejects(cold.admit('c', 1000), (e) => refusal('relay_admission_full', 429)(e) && e.retryAfter === 86399);
  await cold.admit('c', 86400000);
});

// C20 lets an operator close admission without changing code, including at a zero ceiling.
test('zero admission closes the door and a per-network refusal spends no global allowance', async () => {
  await assert.rejects(new GlobalBudget(memorySql(), empty, limits({ globalNewIdentitiesPerDay: 0 })).admit('a', 0), refusal('relay_admission_full', 429));
  const budget = new GlobalBudget(memorySql(), empty, limits({ globalNewIdentitiesPerDay: 2, newIdentitiesPerIpPerDay: 1 }));
  await budget.admit('a', 0);
  await assert.rejects(budget.admit('a', 0), refusal('too_many_identities', 429));
  await budget.admit('b', 0);
});

// The emergency stop must reserve atomically so racing owners cannot exceed it.
test('racing reservations share a durable hard storage ceiling and erasure returns capacity', async () => {
  const sql = memorySql(), cap = limits({ globalStoreBytes: 10, globalHardStoreBytes: 10 });
  const budget = new GlobalBudget(sql, empty, cap);
  const results = await Promise.allSettled([budget.reserve('a', 'f', 6), budget.reserve('b', 'f', 6)]);
  assert.deepEqual(results.map((r) => r.status), ['fulfilled', 'rejected']);
  assert.equal(results[1].reason.code, 'relay_storage_full');
  const cold = new GlobalBudget(sql, empty, cap);
  await cold.reserve('a', 'f', 6);
  await assert.rejects(cold.reserve('b', 'g', 5), refusal('relay_storage_full', 507));
  await cold.forget('a');
  await cold.forget('a');
  await cold.reserve('b', 'g', 10);
  await assert.rejects(cold.admit('new', 0), refusal('relay_storage_full', 507));
});

// C20 includes the deployed bucket's existing bytes, across pages and an interrupted inventory.
test('the first inventory counts existing R2 objects once before allowing writes', async () => {
  const sql = memorySql();
  let fail = true;
  const bucket = { list: async ({ cursor }) => {
    if (!cursor) return { objects: [{ key: 'old/f/a', size: 6 }], truncated: true, cursor: 'next' };
    if (fail) { fail = false; throw new Error('interrupted'); }
    return { objects: [{ key: 'other/f/b', size: 4 }], truncated: false };
  } };
  const budget = new GlobalBudget(sql, bucket, limits({ globalStoreBytes: 10, globalHardStoreBytes: 10 }));
  await assert.rejects(budget.reserve('new', 'c', 1), /interrupted/);
  await assert.rejects(budget.reserve('new', 'c', 1), refusal('relay_storage_full', 507));
  await budget.reserve('old', 'a', 6);
  await budget.forget('old');
  await budget.reserve('new', 'c', 6);
});

// The emergency stop preserves reads and retries so an operator can recover without losing data.
test('an existing owner can read and erase when the hard storage brake is closed', async () => {
  const env = await openBucket();
  try {
    const first = frame([rid(1)]), second = frame([rid(2)]);
    const cap = limits({ globalStoreBytes: first.bytes.length, globalHardStoreBytes: first.bytes.length });
    const budget = new GlobalBudget(memorySql(), env.bucket, cap);
    const tenant = new Tenant({ identity: 'owner', sql: memorySql(), bucket: env.bucket, clock: () => 0,
      policy: policyOf(), limits: cap, flight: new Flight(), arm: () => {}, watchers: NOBODY,
      reserve: (...args) => budget.reserve(...args) });
    await tenant.putFrame(first.bytes);
    await tenant.putFrame(first.bytes);
    await assert.rejects(tenant.putFrame(second.bytes), refusal('relay_storage_full', 507));
    cap.globalHardStoreBytes = 0;
    await tenant.putFrame(first.bytes);
    await assert.rejects(tenant.putFrame(second.bytes), refusal('relay_storage_full', 507));
    assert.deepEqual(await tenant.getObject(rid(1)), first.objs[0]);
    const page = await env.bucket.list({ prefix: 'owner/' });
    await env.bucket.delete(page.objects.map((o) => o.key));
    await budget.forget('owner');
    cap.globalHardStoreBytes = first.bytes.length;
    await budget.reserve('another', 'f', second.bytes.length);
  } finally { await env.close(); }
});

test('existing identities keep their own byte, object and daily ceilings after shared storage closes', async () => {
  const env = await openBucket();
  try {
    const first = frame([rid(1)]), second = frame([rid(2)]), third = frame([rid(3)]);
    for (const own of [{ storeBytes: first.bytes.length * 2 }, { storeObjects: 2 }, { framesPerDay: 2 }]) {
      const cap = limits({ globalStoreBytes: first.bytes.length, ...own });
      const sql = memorySql();
      const budget = new GlobalBudget(sql, env.bucket, cap);
      const identity = Object.keys(own)[0];
      await budget.admit(identity, 0);
      const tenant = new Tenant({ identity, sql: memorySql(), bucket: env.bucket, clock: () => 0,
        policy: policyOf(), limits: cap, flight: new Flight(), arm: () => {}, watchers: NOBODY,
        reserve: (...args) => budget.reserve(...args) });
      await tenant.putFrame(first.bytes);
      await assert.rejects(budget.admit('new', 0), refusal('relay_storage_full', 507));
      await tenant.putFrame(second.bytes);
      await tenant.putFrame(first.bytes);
      await assert.rejects(tenant.putFrame(third.bytes), (e) => refusal('full', 507)(e) && e.limitBytes === own.storeBytes);
      assert.deepEqual(await tenant.getObject(rid(1)), first.objs[0]);
      assert.equal(sql.exec('SELECT bytes FROM global_frames WHERE owner = ?', identity).toArray().length, 2);
      const page = await env.bucket.list({ prefix: `${identity}/` });
      await env.bucket.delete(page.objects.map((o) => o.key));
      await budget.forget(identity);
      assert.equal(sql.exec('SELECT bytes FROM global_usage').one().bytes, 0);
    }
  } finally { await env.close(); }
});
