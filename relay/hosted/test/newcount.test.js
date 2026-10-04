import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Counters } from '../src/counters.js';
import { DEFAULTS, limitsOf } from '../src/limits.js';
import { ipOf, respond } from '../src/wire.js';
import { countNewcomer } from '../src/newcount.js';
import { memorySql } from './sql.js';

const HOUR = 3_600_000;
const network = (address) => ipOf(new Request('https://x/', { headers: { 'cf-connecting-ip': address } }), {});
const refusal = (fn) => {
  try {
    fn();
    return null;
  } catch (e) {
    return e;
  }
};

test('the default allows 200 new identities from one network a day', () => {
  assert.equal(DEFAULTS.newIdentitiesPerIpPerDay, 200);
  assert.equal(limitsOf({}).newIdentitiesPerIpPerDay, 200);
  const counters = new Counters(memorySql());
  for (let i = 0; i < 200; i++) countNewcomer(counters, 200, '203.0.113.7', 0);
  assert.equal(refusal(() => countNewcomer(counters, 200, '203.0.113.7', 0)).code, 'too_many_identities');
});

test('a refusal names the seconds left in that network\'s own day, in the header and in the body', async () => {
  const counters = new Counters(memorySql());
  countNewcomer(counters, 1, '203.0.113.7', 0); // the day starts here
  const e = refusal(() => countNewcomer(counters, 1, '203.0.113.7', 3 * HOUR));
  assert.equal(e.retryAfter, 21 * 3600);
  const res = await respond(() => { throw e; });
  assert.equal(res.status, 429);
  assert.equal(res.headers.get('retry-after'), String(21 * 3600));
  assert.deepEqual(await res.json(), { err: 'too_many_identities', retry_after: 21 * 3600 });
});

test('the wait is rounded up to a whole second, and the day starts over once it ends', () => {
  const counters = new Counters(memorySql());
  countNewcomer(counters, 1, 'n', 0);
  assert.equal(refusal(() => countNewcomer(counters, 1, 'n', 86_399_500)).retryAfter, 0.5);
  countNewcomer(counters, 1, 'n', 86_400_000);
});

test('two addresses in one /64 share a count, and another /64 has its own', () => {
  const counters = new Counters(memorySql());
  countNewcomer(counters, 2, network('2001:db8:1:2::1'), 0);
  countNewcomer(counters, 2, network('2001:db8:1:2:ffff:eeee:dddd:cccc'), 0);
  assert.equal(refusal(() => countNewcomer(counters, 2, network('2001:db8:1:2::77'), 0)).code, 'too_many_identities');
  countNewcomer(counters, 2, network('2001:db8:1:3::1'), 0);
});

test('an IPv4-mapped address shares the count of its IPv4 address', () => {
  const counters = new Counters(memorySql());
  countNewcomer(counters, 1, network('198.51.100.9'), 0);
  assert.equal(refusal(() => countNewcomer(counters, 1, network('::ffff:198.51.100.9'), 0)).code, 'too_many_identities');
});
