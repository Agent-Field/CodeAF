// The cap on new identities per IP, against a relay started with short limits (3 a day). Only a
// first sight of an identity is counted: an existing identity is never refused, from any address.
import assert from 'node:assert/strict';
import { newIdentity, newDevice, signed } from './party.js';
import { TIGHT } from './client.js';

const ip = '203.0.113.7';
const from = async (dev, address) => {
  const headers = { ...(await signed(dev, 'GET', '/v1/dir/list')), 'cf-connecting-ip': address };
  const res = await fetch(TIGHT + '/v1/dir/list', { headers });
  return { status: res.status, retry: res.headers.get('retry-after'), err: res.status === 200 ? null : (await res.json()).err };
};

const people = await Promise.all([1, 2, 3, 4].map(async () => newDevice(await newIdentity())));
for (const p of people.slice(0, 3)) assert.equal((await from(p, ip)).status, 200);

const fourth = await from(people[3], ip);
assert.deepEqual([fourth.status, fourth.err], [429, 'too_many_identities']);
assert.ok(Number(fourth.retry) > 0, 'Retry-After names the wait');

for (let i = 0; i < 5; i++) assert.equal((await from(people[0], ip)).status, 200, 'an admitted identity is never counted again');
assert.equal((await from(people[1], '198.51.100.9')).status, 200, 'nor from another address');
assert.equal((await from(people[3], '198.51.100.9')).status, 200, 'a refused identity is welcome from an address with room');
assert.equal((await from(people[3], ip)).status, 200, 'and once admitted it is known here too');
console.log('newcomers: all pass');
