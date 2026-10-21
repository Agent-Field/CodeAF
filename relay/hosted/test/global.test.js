import { test } from 'node:test';
import assert from 'node:assert/strict';
import { GlobalBudget } from '../src/global.js';
import { DEFAULTS } from '../src/limits.js';
import { Tenant } from '../src/tenant.js';
import { Flight } from '../src/flight.js';
import { policyOf } from '../src/rules.js';
import { NOBODY } from '../src/watch.js';
import { memorySql } from './sql.js';
import { frame, rid, openBucket } from './helpers.js';

const empty = { list: async () => ({ objects: [], truncated: false }) };
const refusal = (code, status) => (e) => e.code === code && e.status === status;
const limits = (overrides) => ({ ...DEFAULTS, ...overrides });

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

// C20 reserves storage atomically across owners; retries and process restarts cannot spend twice.
test('racing reservations share a durable storage ceiling and erasure returns capacity', async () => {
  const sql = memorySql(), cap = limits({ globalStoreBytes: 10 });
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
  const budget = new GlobalBudget(sql, bucket, limits({ globalStoreBytes: 10 }));
  await assert.rejects(budget.reserve('new', 'c', 1), /interrupted/);
  await assert.rejects(budget.reserve('new', 'c', 1), refusal('relay_storage_full', 507));
  await budget.reserve('old', 'a', 6);
  await budget.forget('old');
  await budget.reserve('new', 'c', 6);
});

// C20 refuses new frames while preserving reads, retries and owner erasure through the real store.
test('an existing owner can read and erase when the global storage brake is closed', async () => {
  const env = await openBucket();
  try {
    const first = frame([rid(1)]), second = frame([rid(2)]);
    const cap = limits({ globalStoreBytes: first.bytes.length });
    const budget = new GlobalBudget(memorySql(), env.bucket, cap);
    const tenant = new Tenant({ identity: 'owner', sql: memorySql(), bucket: env.bucket, clock: () => 0,
      policy: policyOf(), limits: cap, flight: new Flight(), arm: () => {}, watchers: NOBODY,
      reserve: (...args) => budget.reserve(...args) });
    await tenant.putFrame(first.bytes);
    await tenant.putFrame(first.bytes);
    await assert.rejects(tenant.putFrame(second.bytes), refusal('relay_storage_full', 507));
    assert.deepEqual(await tenant.getObject(rid(1)), first.objs[0]);
    const page = await env.bucket.list({ prefix: 'owner/' });
    await env.bucket.delete(page.objects.map((o) => o.key));
    await budget.forget('owner');
    await budget.reserve('another', 'f', second.bytes.length);
  } finally { await env.close(); }
});
