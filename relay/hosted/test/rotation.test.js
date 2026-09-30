import { test, before, after } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULTS } from '../src/limits.js';
import { rotateBy, rotationView } from '../src/rotation.js';
import { dropTables, erasePage } from '../src/erasure.js';
import { Directory } from '../src/directory.js';
import { STAGE1 } from '../src/rules.js';
import { memorySql } from './sql.js';
import { openBucket } from './helpers.js';

const NOW = 5_000;
const move = (cur, device, op, grace_ms) => rotateBy(cur, device, { op, grace_ms }, NOW, DEFAULTS);
const code = (fn) => {
  try {
    fn();
    return 'ok';
  } catch (e) {
    return e.code;
  }
};

test('the first device to freeze owns the rotation; a second freeze and another device retire are rotated', () => {
  const frozen = move(undefined, 'a', 'freeze');
  assert.deepEqual(frozen, { state: 'frozen', by: 'a', at: NOW });
  assert.equal(move(frozen, 'a', 'freeze'), frozen, 'the owner freezing again changes nothing');
  assert.equal(code(() => move(frozen, 'b', 'freeze')), 'rotated');
  assert.equal(code(() => move(frozen, 'b', 'retire')), 'rotated');
});

test('any device may thaw a frozen identity, thawing a live one is fine, and nothing thaws a retired one', () => {
  const frozen = move(undefined, 'a', 'freeze');
  assert.equal(move(frozen, 'b', 'thaw'), undefined);
  assert.equal(move(undefined, 'b', 'thaw'), undefined);
  const retired = move(frozen, 'a', 'retire');
  assert.equal(code(() => move(retired, 'a', 'thaw')), 'rotated');
  assert.equal(code(() => move(retired, 'b', 'freeze')), 'rotated');
});

test('retire needs a freeze, takes its grace from the bounds, and again changes nothing', () => {
  assert.equal(code(() => move(undefined, 'a', 'retire')), 'rotation_step');
  const frozen = move(undefined, 'a', 'freeze');
  for (const bad of [1, DEFAULTS.minGraceMs - 1, DEFAULTS.maxGraceMs + 1, -5]) assert.equal(code(() => move(frozen, 'a', 'retire', bad)), 'bad_grace');
  assert.equal(code(() => move(frozen, 'a', 'retire', 1.5)), 'bad_request');
  const retired = move(frozen, 'a', 'retire');
  assert.equal(retired.retire_at, NOW + DEFAULTS.defaultGraceMs);
  assert.deepEqual(Object.keys(retired), ['state', 'by', 'at', 'retire_at'], 'the field order the Go relay writes');
  assert.equal(move(retired, 'a', 'retire', 1), retired);
});

test('an unknown op, including one that names an inherited property, is bad_request', () => {
  for (const op of ['explode', 'constructor', undefined]) assert.equal(code(() => move(undefined, 'a', op)), 'bad_request');
});

test('the view names the bounds, and leaves the rotation out while the identity is live', () => {
  const view = rotationView(undefined, NOW, DEFAULTS);
  assert.equal(JSON.stringify(view), `{"now":5000,"min_grace_ms":3600000,"max_grace_ms":2592000000,"default_grace_ms":604800000}`);
});

test('the rotation lives in the identity record beside the vault, and a thaw removes it', () => {
  const dir = new Directory(memorySql(), 'id_t', () => NOW, STAGE1);
  dir.setVault('', 'sealed');
  dir.setRotation(move(undefined, 'a', 'freeze'));
  assert.equal(dir.list().identity.rotation.state, 'frozen');
  assert.equal(dir.list().identity.vault, 'sealed');
  dir.setRotation(undefined);
  assert.equal('rotation' in dir.list().identity, false);
});

let env;
before(async () => { env = await openBucket(); });
after(() => env.close());

test('erasure deletes the prefix a page at a time, touches no other identity, and a second run changes nothing', async () => {
  for (const key of ['id_e/f/1', 'id_e/f/2', 'id_e/f/3', 'id_keep/f/1']) await env.bucket.put(key, 'x');
  const pages = [];
  while (await erasePage(env.bucket, 'id_e/', 2)) pages.push(1);
  assert.equal(pages.length, 2, 'three objects at two a page is two turns');
  assert.equal(await erasePage(env.bucket, 'id_e/', 2), false);
  assert.deepEqual((await env.bucket.list({ prefix: 'id_keep/' })).objects.length, 1);
});

test('dropTables removes every relay table and is safe to run twice', () => {
  const sql = memorySql();
  new Directory(sql, 'id_t', () => NOW, STAGE1).setVault('', 'v');
  dropTables(sql);
  assert.deepEqual(sql.exec("SELECT name FROM sqlite_master WHERE type = 'table'").toArray(), []);
  dropTables(sql);
});
