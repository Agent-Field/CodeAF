// The pure parts of link pairing, against the contract's own fixtures (docs/testdata/ux-pairing-vectors.json).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { Counters } from '../src/counters.js';
import { Directory } from '../src/directory.js';
import { asGrant, checkDigits, codeOf, CODE_ALPHABET, deviceIdOf, newCode, newRequest, platformOf } from '../src/link/code.js';
import { settled } from '../src/link/state.js';
import { onlineDevices, presenceOf } from '../src/presence.js';
import { STAGE1 } from '../src/rules.js';
import { memorySql } from './sql.js';

const vectors = JSON.parse(readFileSync(new URL('../../../docs/testdata/ux-pairing-vectors.json', import.meta.url)));
const b64u = (bytes) => Buffer.from(bytes).toString('base64url');
const fail = async (fn) => {
  try {
    await fn();
    return 'ok';
  } catch (e) {
    return [e.code, e.status].join(' ');
  }
};

test('a code is 8 characters of the Crockford alphabet, and the vector code is one', () => {
  for (let i = 0; i < 200; i++) assert.match(newCode(), /^[0-9a-hjkmnp-tv-z]{8}$/);
  assert.equal(CODE_ALPHABET.length, 32);
  assert.equal(codeOf(vectors.request_pending.code), 'k7m2q9xd');
  assert.equal(codeOf('K7M2Q9XD'), 'k7m2q9xd', 'upper case is accepted');
  for (const bad of ['k7m2q9x', 'k7m2q9xdd', 'k7m2q9xi', 'k7m2q9xl', 'k7m2q9xo', 'k7m2q9xu', '', null, 'k7m2q9x!']) assert.equal(codeOf(bad), null, String(bad));
});

test('codes do not repeat in a thousand draws', () => {
  assert.equal(new Set(Array.from({ length: 1000 }, newCode)).size, 1000);
});

test('check digits are four digits of the key hash', async () => {
  const check = await checkDigits(new Uint8Array(32));
  assert.match(check, /^\d{4}$/);
  assert.equal(check, await checkDigits(new Uint8Array(32)));
  const other = new Uint8Array(32).fill(7);
  assert.match(await checkDigits(other), /^\d{4}$/);
});

test('the device id is dev_ and 16 bytes of the key hash, as verify.js makes it', async () => {
  assert.match(await deviceIdOf(new Uint8Array(32)), /^dev_[0-9a-f]{32}$/);
});

test('an unknown platform is other', () => {
  for (const p of ['darwin', 'linux', 'windows', 'ios', 'android', 'other']) assert.equal(platformOf(p), p);
  for (const p of ['plan9', '', undefined, 7]) assert.equal(platformOf(p), 'other');
});

const goodBody = (over = {}) => ({ pubkey: b64u(new Uint8Array(32).fill(1)), x25519: b64u(new Uint8Array(32).fill(2)), name_sealed: b64u(new Uint8Array(24 + 16 + 5)), platform: 'linux', ...over });

test('a create body is checked field by field, and the relay derives the device and the check', async () => {
  const made = await newRequest(goodBody());
  assert.equal(made.device, await deviceIdOf(new Uint8Array(32).fill(1)));
  assert.equal(made.check, await checkDigits(new Uint8Array(32).fill(1)));
  assert.equal(made.platform, 'linux');
  assert.equal((await newRequest(goodBody({ platform: 'beos' }))).platform, 'other');
  const bad = {
    'short pubkey': { pubkey: b64u(new Uint8Array(31)) },
    'long x25519': { x25519: b64u(new Uint8Array(33)) },
    'missing pubkey': { pubkey: undefined },
    'not base64url': { x25519: '!!' },
    'name shorter than nonce and mac': { name_sealed: b64u(new Uint8Array(39)) },
    'name over 96 bytes': { name_sealed: b64u(new Uint8Array(24 + 16 + 97)) },
    'device that is not the key\'s': { device: 'dev_other' },
  };
  for (const [what, over] of Object.entries(bad)) assert.equal(await fail(() => newRequest(goodBody(over))), 'bad_request 400', what);
  assert.equal((await newRequest(goodBody({ name_sealed: b64u(new Uint8Array(24 + 16 + 96)) }))).device, made.device, '96 bytes of name is allowed');
});

test('the device a body names is accepted when it is the key\'s', async () => {
  const device = await deviceIdOf(new Uint8Array(32).fill(1));
  assert.equal((await newRequest(goodBody({ device }))).device, device);
});

test('grant refusals: too_big over the cap, bad_request for nothing or for text that is not base64url', async () => {
  assert.equal(await fail(() => asGrant(b64u(new Uint8Array(4097)), 4096)), 'too_big 413');
  assert.equal(await fail(() => asGrant('', 4096)), 'bad_request 400');
  assert.equal(await fail(() => asGrant(undefined, 4096)), 'bad_request 400');
  assert.equal(await fail(() => asGrant('a b', 4096)), 'bad_request 400');
});

test('a request decides once; a repeat is the same record; a contradiction is already_decided', async () => {
  const pending = vectors.request_pending;
  const approved = settled(pending, 'approved', 'GRANT', 5);
  assert.deepEqual([approved.state, approved.grant, approved.decided_at], ['approved', 'GRANT', 5]);
  assert.equal(pending.state, 'pending', 'the first record is not changed');
  assert.equal(settled(approved, 'approved', 'OTHER', 9), approved, 'a repeat keeps the first grant');
  const denied = settled(pending, 'denied', null, 5);
  assert.deepEqual([denied.state, denied.grant], ['denied', null]);
  assert.equal(settled(denied, 'denied', null, 9), denied);
  assert.equal(await fail(() => settled(approved, 'denied', null, 9)), 'already_decided 409');
  assert.equal(await fail(() => settled(denied, 'approved', 'G', 9)), 'already_decided 409');
});

test('presence lists every live device, online ones as seen now', () => {
  const devices = { dev_A: { revoked: false }, dev_B: { revoked: false, last_seen: 1789999000000 }, dev_C: { revoked: true } };
  const answer = presenceOf(devices, new Set(['dev_A', 'dev_C']), 1790000001000);
  assert.deepEqual(answer, vectors.presence_list);
});

test('a device is online when any socket carries its tag', () => {
  const sockets = [['dev_A'], ['dev_A'], ['dev_B']];
  const ctx = { getWebSockets: () => sockets, getTags: (s) => s };
  assert.deepEqual([...onlineDevices(ctx)].sort(), ['dev_A', 'dev_B']);
});

test('counters peek counts nothing and forgets an ended window', () => {
  const c = new Counters(memorySql());
  assert.equal(c.peek('k', 0).n, 0);
  c.hit('k', 1000, 0);
  c.hit('k', 1000, 0);
  assert.equal(c.peek('k', 500).n, 2);
  assert.equal(c.peek('k', 500).n, 2, 'peeking twice does not count');
  assert.equal(c.peek('k', 1000).n, 0);
});

function open() {
  const clock = { now: 1_790_000_123_456 };
  return { clock, dir: new Directory(memorySql(), 'id_t', () => clock.now, STAGE1) };
}

test('a device written by the relay gets created once, and the client cannot set created or last_seen', () => {
  const { dir, clock } = open();
  dir.putDevice('dev_a', { ...vectors.device_new, created: 5, last_seen: 6 });
  assert.deepEqual([dir.read('devices', 'dev_a').created, dir.read('devices', 'dev_a').last_seen], [1_790_000_123_456, undefined]);
  clock.now += 1000;
  dir.putDevice('dev_a', { ...vectors.device_new, created: 9, last_seen: 9, name: 'renamed' });
  const after = dir.read('devices', 'dev_a');
  assert.deepEqual([after.created, after.name], [1_790_000_123_456, 'renamed']);
});

test('a legacy record stays valid: rewriting it adds no created', () => {
  const { dir } = open();
  dir.write('devices', 'dev_old', vectors.device_legacy);
  dir.putDevice('dev_old', vectors.device_legacy);
  assert.equal('created' in dir.read('devices', 'dev_old'), false);
});

test('approving a device stores it live, with a created time and a bumped version', () => {
  const { dir } = open();
  const v = dir.version;
  const stored = dir.approveDevice('dev_b', { ...vectors.device_new, revoked: true, created: 1 });
  assert.deepEqual([stored.revoked, stored.created, stored.platform], [false, 1_790_000_123_456, 'linux']);
  assert.equal(dir.version, v + 1);
  dir.approveDevice('dev_b', vectors.device_new);
  assert.equal(dir.version, v + 1, 'the same approve again changes nothing a person sees');
});
