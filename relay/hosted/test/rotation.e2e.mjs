// Identity rotation end to end, against a relay whose shortest grace is 2 s and whose sweep deletes
// two R2 objects a turn (the runner sets both): freeze, thaw, retire, the refusals by state, the
// deadline, the tombstone, and that a gone identity costs the relay nothing more.
import assert from 'node:assert/strict';
import { readdirSync } from 'node:fs';
import { DatabaseSync } from 'node:sqlite';
import { newIdentity, newDevice } from './party.js';
import { frame, rid, init } from './helpers.js';
import { call, answerOf } from './client.js';

// The id the relay names an identity by: id_ and the first 16 bytes of the hash of its signing key, in hex.
const idOf = async (dev) => 'id_' + Buffer.from(await crypto.subtle.digest('SHA-256', dev.identity.pub)).subarray(0, 16).toString('hex');
const rotate = (dev, op, grace_ms) => call(dev, 'POST', '/v1/identity/rotation', { V: 1, op, grace_ms });
const put = (dev, tag) => call(dev, 'POST', '/v1/store/frames', frame([rid(tag)], String(tag)).bytes);

const id = await newIdentity();
const a = await newDevice(id);
const b = await newDevice(id);
assert.equal((await call(a, 'POST', '/v1/dir/cells/c1', init())).status, 200);
for (const tag of [1, 2, 3, 4, 5]) assert.equal((await put(a, tag)).status, 200); // five frames: three sweep turns at two a page

// Live: the view names the bounds and no rotation.
let view = (await call(a, 'GET', '/v1/identity/rotation')).json;
assert.equal(view.rotation, undefined);
assert.deepEqual([view.min_grace_ms, view.max_grace_ms, view.default_grace_ms], [2000, 30 * 86_400_000, 7 * 86_400_000]);

// Frozen: every write is 410 rotated, for every device; reads stay open; the first freezer owns it.
assert.equal((await rotate(a, 'freeze')).json.rotation.state, 'frozen');
for (const dev of [a, b]) {
  for (const [method, path, body] of [
    ['POST', '/v1/dir/cells/c2', init()],
    ['POST', '/v1/dir/cells/c1/acquire', {}],
    ['POST', '/v1/dir/vault', { old: '', new: 'x' }],
    ['POST', '/v1/dir/cells/c1/archive'],
  ]) assert.equal(answerOf(await call(dev, method, path, body)), 'rotated', `${method} ${path}`);
  assert.equal(answerOf(await put(dev, 6)), 'rotated');
}
assert.equal((await call(b, 'GET', '/v1/dir/list')).status, 200);
assert.equal((await call(b, 'GET', '/v1/dir/cells/c1')).status, 200, 'a single cell reads too');
assert.equal(answerOf(await rotate(b, 'freeze')), 'rotated');
assert.equal(answerOf(await rotate(b, 'retire')), 'rotated');

// Any device thaws, and the identity writes again.
assert.equal((await rotate(b, 'thaw')).status, 200);
assert.equal((await put(b, 7)).status, 200);
assert.equal(answerOf(await rotate(a, 'retire')), 'rotation_step');

// Retire: bounds, reads open inside the grace, a retire is forever.
await rotate(a, 'freeze');
for (const bad of [1, 1999, 30 * 86_400_000 + 1]) assert.equal(answerOf(await rotate(a, 'retire', bad)), 'bad_grace');
assert.equal(answerOf(await call(a, 'POST', '/v1/identity/rotation', { op: 'explode' })), 'bad_request');
const retired = await rotate(a, 'retire', 2000);
assert.equal(retired.json.rotation.retire_at, retired.json.now + 2000);
assert.equal((await call(b, 'GET', '/v1/store/objects/' + rid(1))).status, 200, 'a read inside the grace');
assert.equal(answerOf(await rotate(a, 'thaw')), 'rotated');
assert.equal(answerOf(await rotate(b, 'freeze')), 'rotated');
assert.deepEqual((await rotate(a, 'retire', 9999)).json.rotation, retired.json.rotation, 'retiring again changes nothing');

// The deadline: gone on both wires, and a revoked-style check still precedes it (a stranger is unauthorized).
const deadline = Date.now() + 40_000;
while (answerOf(await call(a, 'GET', '/v1/dir/list')) !== 'gone') {
  assert.ok(Date.now() < deadline, 'the identity was still there 38 s after its deadline');
  await new Promise((r) => setTimeout(r, 500));
}
for (const dev of [a, b]) {
  assert.equal(answerOf(await call(dev, 'POST', '/v1/store/has', { rids: [] })), 'gone');
  assert.equal(answerOf(await rotate(dev, 'thaw')), 'gone');
  assert.equal(answerOf(await call(dev, 'POST', '/v1/dir/cells/c9', init())), 'gone');
}
assert.equal((await call(a, 'GET', '/v1/dir/list')).status, 410);

// Gone costs nothing: no R2 object of the identity is left, its tables are dropped, and asking again makes none.
const objectsLeft = async (file) => {
  const db = new DatabaseSync(`${process.env.R2_DIR}/${file}`, { readOnly: true });
  const has = db.prepare("SELECT count(*) AS n FROM sqlite_master WHERE name = '_mf_objects'").get().n;
  const left = has && db.prepare('SELECT count(*) AS n FROM _mf_objects WHERE key LIKE ?').get(`${await idOf(a)}/%`).n;
  db.close();
  return has ? left : null; // the bucket's own metadata file holds no objects table and says nothing
};
const counts = (await Promise.all(readdirSync(process.env.R2_DIR).filter((f) => f.endsWith('.sqlite')).map(objectsLeft))).filter((n) => n !== null);
assert.ok(counts.length >= 1 && counts.every((n) => n === 0), 'the R2 prefix is empty');
const dir = process.env.IDENTITY_DO_DIR;
const mine = () => readdirSync(dir).filter((f) => f.endsWith('.sqlite')).flatMap((f) => {
  const db = new DatabaseSync(`${dir}/${f}`, { readOnly: true });
  const names = db.prepare("SELECT name FROM sqlite_master WHERE type = 'table'").all().map((t) => t.name);
  const tombstone = names.includes('_cf_KV') && db.prepare("SELECT count(*) AS n FROM _cf_KV WHERE key = 'gone'").get().n;
  db.close();
  return tombstone ? [names.filter((n) => !n.startsWith('_') && n !== 'sqlite_sequence')] : [];
});
assert.ok(mine().length >= 1, 'the tombstone is stored');
assert.ok(mine().every((tables) => tables.length === 0), 'the gone identity keeps no table');
console.log('rotation: all pass');
