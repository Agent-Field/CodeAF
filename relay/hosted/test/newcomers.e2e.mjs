// The cap on new identities per IP, against a relay started with short limits (3 a day). Only a
// first sight of an identity is counted: an existing identity is never refused, from any address.
import assert from 'node:assert/strict';
import { readdirSync } from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { newIdentity, newDevice, signed } from './party.js';
import { TIGHT } from './client.js';

const ip = '203.0.113.7';
const from = async (dev, address) => {
  const headers = { ...(await signed(dev, 'GET', '/v1/dir/list')), 'cf-connecting-ip': address };
  const res = await fetch(TIGHT + '/v1/dir/list', { headers });
  return { status: res.status, retry: res.headers.get('retry-after'), err: res.status === 200 ? null : (await res.json()).err };
};

// Where the relay under test keeps its identity objects on disk (the runner sets it). A refused
// identity must leave nothing stored, or refused identities would pile up for good. The local runtime
// makes an empty file for any object it wakes, so what is counted is the files that hold a table
// (the runtime's own name table aside): an admitted identity has several, a refused one none.
const dir = process.env.IDENTITY_DO_DIR;
const tables = (file) => {
  const db = new DatabaseSync(`${dir}/${file}`, { readOnly: true });
  const names = db.prepare('SELECT name FROM sqlite_master').all().map((r) => r.name);
  db.close();
  return names.filter((n) => n !== '__miniflare_do_name');
};
const stored = () => readdirSync(dir).filter((f) => f.endsWith('.sqlite')).filter((f) => tables(f).length > 0).length;

const people = await Promise.all([1, 2, 3, 4].map(async () => newDevice(await newIdentity())));
const before = stored();
for (const p of people.slice(0, 3)) assert.equal((await from(p, ip)).status, 200);
assert.equal(stored(), before + 3, 'each admitted identity is stored');

const kept = stored();

const fourth = await from(people[3], ip);
assert.deepEqual([fourth.status, fourth.err], [429, 'too_many_identities']);
assert.ok(Number(fourth.retry) > 0, 'Retry-After names the wait');
assert.equal(stored(), kept, 'a refused identity stored nothing');

for (let i = 0; i < 5; i++) assert.equal((await from(people[0], ip)).status, 200, 'an admitted identity is never counted again');
assert.equal((await from(people[1], '198.51.100.9')).status, 200, 'nor from another address');
assert.equal((await from(people[3], '198.51.100.9')).status, 200, 'a refused identity is welcome from an address with room');
assert.equal((await from(people[3], ip)).status, 200, 'and once admitted it is known here too');
console.log('newcomers: all pass');
