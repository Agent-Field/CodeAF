// The enrollment store of the Worker's fabric (design 3.2, 3.7; routes.json enroll.put and enroll.list). Every test names
// the contract id it proves. The identity's signature is checked through the implementation only: the vector verdicts
// come from internal/clientproto/testdata/vectors.json, which Go generated, and are never written into a test as an answer.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createHash, generateKeyPairSync, sign } from 'node:crypto';
import { Fabric, checkEnrollment } from '../src/fabric.js';
import { memorySql } from './sql.js';

const vectors = JSON.parse(readFileSync(new URL('../../../internal/clientproto/testdata/vectors.json', import.meta.url), 'utf8'));

const enc = new TextEncoder();
const bytes = (value) => enc.encode(JSON.stringify(value));
const sha = (b) => createHash('sha256').update(b).digest();
const raw32 = (key) => key.export({ format: 'der', type: 'spki' }).subarray(-32);

// codeOf names how a call ended: the refusal's code (a Wire) or kind (an authentication refusal), or 'ok'.
async function codeOf(fn) {
  try {
    await fn();
    return 'ok';
  } catch (e) {
    return e.code ?? e.kind ?? `unexpected: ${e.message}`;
  }
}

// The identity and its phones, built here from the spec's text (design 3.2), so a drift between this and fabric.js is a failure.
const identity = generateKeyPairSync('ed25519');
const identityHeader = raw32(identity.publicKey).toString('base64url');
const FULL = 'dev_' + 'a'.repeat(32);

function phone(seed = 1) {
  const key = generateKeyPairSync('ed25519');
  const signPub = raw32(key.publicKey);
  return { signPub, boxPub: Buffer.alloc(32, seed), device: 'dev_' + sha(signPub).toString('hex').slice(0, 32) };
}

// signText is the exact text of design 3.2.
const signText = (e) =>
  `codeaf-enroll-v1\n${e.device}\n${Buffer.from(e.sign_pub, 'base64').toString('hex')}\n${Buffer.from(e.box_pub, 'base64').toString('hex')}\n${[...e.methods].sort().join(',')}\n${[...e.reads].sort().join(',')}\n${e.ver}`;

function enrollment(p, { methods = ['agents.send', 'agents.create'], reads = ['agents.list'], ver = 1 } = {}, key = identity.privateKey) {
  const e = { v: 1, device: p.device, sign_pub: p.signPub.toString('base64'), box_pub: p.boxPub.toString('base64'), methods, reads, ver, sig: '' };
  e.sig = sign(null, Buffer.from(signText(e)), key).toString('base64');
  return e;
}

// A directory stand-in: the rows the host would have loaded from the committed view.
function open(rows = {}) {
  const sql = memorySql();
  const dir = { rows, read: (kind, id) => (kind === 'devices' ? dir.rows[id] : undefined), devices: () => dir.rows };
  const fabric = new Fabric(sql, () => 1_000_000, dir);
  const put = (e, { caller = FULL, header = identityHeader, device = e.device } = {}) =>
    fabric.putEnrollment({ caller, targetDevice: device, body: bytes(e), identityHeader: header });
  const list = (caller = FULL) => fabric.listEnrollments({ caller, body: new Uint8Array() }).clients;
  return { sql, dir, fabric, put, list };
}

test('R2, P6, P11: every vector in clientproto/testdata/vectors.json gets the verdict Go gave it, and a stored one is listed back unchanged', async () => {
  assert.ok(vectors.enrollment.cases.length >= 6, 'the vectors are the ones the Go side wrote');
  for (const c of vectors.enrollment.cases) {
    const header = Buffer.from(c.identity_pub_hex, 'hex').toString('base64url');
    const verdict = await codeOf(() => checkEnrollment(bytes(c.enrollment), header, c.enrollment.device));
    assert.equal(verdict === 'ok', c.valid, `${c.name}: ${verdict}`);
    if (!c.valid) assert.equal(verdict, 'bad_request', c.name);

    // The same verdict through the route's own method, and what a valid case leaves behind.
    const { put, list } = open();
    const stored = await codeOf(() => put(c.enrollment, { header }));
    assert.equal(stored === 'ok', c.valid, c.name);
    if (c.valid) {
      const [row] = list();
      assert.equal(row.device, c.enrollment.device);
      assert.deepEqual(row.enroll, { ...c.enrollment, methods: c.enrollment.methods, reads: c.enrollment.reads }, c.name);
    } else {
      assert.deepEqual(list(), [], `${c.name}: a refused enrollment stores nothing`);
    }
  }
});

test('R2: the text this test signs is the text the vectors say is signed (a drift in either is a failure)', () => {
  const valid = vectors.enrollment.cases.find((c) => c.name === 'valid');
  assert.equal(signText(valid.enrollment), valid.sign_text);
});

test('R1: an enrollment is stored before its device is registered, and listed with the other phones in device order', async () => {
  const { put, list, dir } = open();
  assert.deepEqual(dir.rows, {}, 'no directory row for any phone');
  const phones = [phone(1), phone(2), phone(3)];
  for (const p of phones) await put(enrollment(p));
  const listed = list();
  assert.deepEqual(listed.map((r) => r.device), phones.map((p) => p.device).sort());
  assert.equal(listed[0].enroll.v, 1);
  assert.deepEqual(open().list(), [], 'a fresh identity lists an empty array, not null');
});

test('R2: an unknown key, a missing key, or a body that is not an object is 400 bad_request', async () => {
  const p = phone();
  const good = enrollment(p);
  const { put } = open();
  assert.equal(await codeOf(() => put({ ...good, extra: 1 })), 'bad_request');
  for (const key of Object.keys(good)) {
    const missing = { ...good };
    delete missing[key];
    assert.equal(await codeOf(() => put(missing)), 'bad_request', `missing ${key}`);
  }
  for (const body of [[], 'x', null, 3]) assert.equal(await codeOf(() => put(body, { device: p.device })), 'bad_request');
  assert.equal(await codeOf(() => open().fabric.putEnrollment({ caller: FULL, targetDevice: p.device, body: enc.encode('{nope'), identityHeader })), 'bad_request');
});

test('R2: a wrong version, a zero ver, a key or signature of the wrong length, or a device not bound to sign_pub is 400', async () => {
  const p = phone();
  const good = enrollment(p);
  const { put } = open();
  const resigned = (change) => {
    const e = { ...good, ...change };
    e.sig = sign(null, Buffer.from(signText(e)), identity.privateKey).toString('base64');
    return e;
  };
  const refused = {
    'v is 2': resigned({ v: 2 }),
    'ver is 0': resigned({ ver: 0 }),
    'ver is a fraction': resigned({ ver: 1.5 }),
    'ver is a string': resigned({ ver: '1' }),
    'sign_pub is 31 bytes': resigned({ sign_pub: Buffer.alloc(31).toString('base64') }),
    'box_pub is 33 bytes': resigned({ box_pub: Buffer.alloc(33).toString('base64') }),
    'box_pub is not base64': resigned({ box_pub: '***' }),
    'sig is 63 bytes': { ...good, sig: Buffer.alloc(63).toString('base64') },
    'sig is empty': { ...good, sig: '' },
    'device is not the hash of sign_pub': resigned({ device: 'dev_' + 'b'.repeat(32) }),
  };
  for (const [name, e] of Object.entries(refused)) assert.equal(await codeOf(() => put(e, { device: e.device })), 'bad_request', name);
  // {device} in the path must be the device in the body.
  assert.equal(await codeOf(() => put(good, { device: 'dev_' + 'c'.repeat(32) })), 'bad_request', 'path and body name different devices');
  assert.equal(await codeOf(() => put(good, { device: 'not-a-device' })), 'bad_request');
});

test('R2: a name list over 64, a name outside [A-Za-z0-9_.-]{1,128}, a repeated name, or a list that is not a list is 400', async () => {
  const p = phone();
  const { put } = open();
  const names = (n) => Array.from({ length: n }, (_, i) => `m${i}`);
  assert.equal(await codeOf(() => put(enrollment(p, { methods: names(64) }))), 'ok', '64 names is allowed');
  const q = phone(9);
  for (const [what, change] of Object.entries({
    '65 methods': { methods: names(65) },
    '65 reads': { reads: names(65) },
    'a name with a space': { methods: ['agents send'] },
    'an empty name': { reads: [''] },
    'a 129 character name': { methods: ['x'.repeat(129)] },
    'a repeated method': { methods: ['a', 'b', 'a'] },
    'a repeated read': { reads: ['r', 'r'] },
    'a number for a name': { methods: [1] },
    'methods that is not a list': { methods: 'agents.send' },
  })) {
    assert.equal(await codeOf(() => put(enrollment(q, change))), 'bad_request', what);
  }
  assert.equal(await codeOf(() => put(enrollment(q, { methods: ['x'.repeat(128)], reads: [] }))), 'ok', 'a 128 character name and an empty list are fine');
});

test('R2, P6: a signature by any key but the identity in Codeaf-Identity, or a statement changed after signing, is refused', async () => {
  const p = phone();
  const other = generateKeyPairSync('ed25519');
  const { put, list } = open();
  assert.equal(await codeOf(() => put(enrollment(p, {}, other.privateKey))), 'bad_request', 'signed by another key');
  assert.equal(await codeOf(() => put({ ...enrollment(p), reads: ['agents.list', 'agents.entries'] })), 'bad_request', 'a read added after signing');
  assert.equal(await codeOf(() => put({ ...enrollment(p), box_pub: Buffer.alloc(32, 9).toString('base64') })), 'bad_request', 'the box key swapped after signing');
  for (const header of [null, '', 'AAAA', raw32(other.publicKey).toString('base64url')]) {
    assert.equal(await codeOf(() => put(enrollment(p), { header })), 'bad_request', `header ${header}`);
  }
  assert.deepEqual(list(), [], 'nothing was stored by any of them');
  assert.equal(await codeOf(() => put(enrollment(p))), 'ok', 'and the right one is accepted');
});

test('R2, P6: lists written in another order are the same statement; the signature does not cover their order', async () => {
  const p = phone();
  const { put } = open();
  const a = enrollment(p, { methods: ['b', 'a'], reads: ['y', 'x'] });
  assert.equal(await codeOf(() => put(a)), 'ok');
  assert.equal(await codeOf(() => put({ ...a, methods: ['a', 'b'], reads: ['x', 'y'] })), 'ok', 'reordered lists re-sent: 204');
});

test('R2: a directory row for the device that is a computer, or a revoked phone, is 409 exists; a live phone row is fine', async () => {
  const p = phone();
  const e = enrollment(p);
  for (const [what, row, want] of [
    ['a computer', { V: 1 }, 'exists'],
    ['a revoked phone', { V: 1, kind: 'client', revoked: true }, 'exists'],
    ['a live phone', { V: 1, kind: 'client', methods: ['agents.send'] }, 'ok'],
  ]) {
    const { put, list } = open({ [p.device]: row });
    assert.equal(await codeOf(() => put(e)), want, what);
    assert.equal(list().length, want === 'ok' ? 1 : 0, what);
  }
});

test('R2, P11: a lower ver is 409 version, the same ver with another statement is 409 version, an identical re-send is 204, a higher ver replaces', async () => {
  const p = phone();
  const { put, list } = open();
  const one = enrollment(p, { ver: 1 });
  assert.equal(await codeOf(() => put(one)), 'ok');
  assert.equal(await codeOf(() => put(one)), 'ok', 'an identical re-send after a lost answer');
  assert.equal(await codeOf(() => put(enrollment(p, { ver: 1, reads: ['agents.list', 'agents.entries'] }))), 'version', 'same ver, different statement');
  assert.equal(await codeOf(() => put(enrollment(p, { ver: 1, methods: ['agents.send'] }))), 'version', 'same ver, fewer methods');
  const two = enrollment(p, { ver: 2, reads: ['agents.list', 'agents.entries'] });
  assert.equal(await codeOf(() => put(two)), 'ok');
  assert.equal(list()[0].enroll.ver, 2);
  assert.equal(await codeOf(() => put(one)), 'version', 'a rollback to ver 1 is refused');
  assert.equal(list()[0].enroll.ver, 2, 'and changes nothing');
  assert.equal((await codeOf(() => put(enrollment(p, { ver: 1 })))), 'version');
  // The error is 409 on the wire.
  const err = await put(one).catch((e) => e);
  assert.equal(err.status, 409);
});

test('R2: the 17th enrollment of an identity is 429 full, and a change to one of the sixteen is still accepted', async () => {
  const { put, list } = open();
  const phones = Array.from({ length: 16 }, (_, i) => phone(i + 1));
  for (const p of phones) assert.equal(await codeOf(() => put(enrollment(p))), 'ok');
  const err = await put(enrollment(phone(99))).catch((e) => e);
  assert.equal(err.code, 'full');
  assert.equal(err.status, 429);
  assert.equal(list().length, 16);
  assert.equal(await codeOf(() => put(enrollment(phones[3], { ver: 2 }))), 'ok', 'a renewal is not a new phone');
  assert.equal(await codeOf(() => put(enrollment(phones[3], { ver: 2 }))), 'ok', 'and an identical re-send of it is 204 too');
});

test('R2, V4: a revoke deletes the slots, queue and sequence of a phone but keeps its enrollment, so its ver cannot roll back', async () => {
  const p = phone();
  const client = { V: 1, kind: 'client', methods: ['agents.send'] };
  const { put, list, fabric, dir, sql } = open({ [FULL]: { V: 1 }, [p.device]: client });
  await put(enrollment(p, { ver: 3 }));
  fabric.putSlot({ caller: FULL, targetDevice: p.device, name: 'card', body: bytes({ sealed: 'YQ==' }) });
  assert.equal(sql.exec('SELECT COUNT(*) AS n FROM fabric_slots').one().n, 1);
  fabric.revokeClient(p.device);
  assert.equal(sql.exec('SELECT COUNT(*) AS n FROM fabric_slots').one().n, 0, 'the slots are gone');
  assert.equal(sql.exec('SELECT COUNT(*) AS n FROM fabric_versions').one().n, 0, 'and the slot counter');
  assert.equal(list().length, 1, 'the enrollment stays');
  assert.equal(list()[0].enroll.ver, 3);
  assert.equal(fabric.hasEnrollment(p.device), true);
  // With the row revoked in the directory the refusal is exists; with the row absent (a relay that never saw the phone) it is the version.
  dir.rows[p.device] = { ...client, revoked: true };
  assert.equal(await codeOf(() => put(enrollment(p, { ver: 4 }))), 'exists');
  delete dir.rows[p.device];
  assert.equal(await codeOf(() => put(enrollment(p, { ver: 2 }))), 'version');
  assert.equal(list()[0].enroll.ver, 3);
});

test('R2: a client may not enroll or list, and the list takes no body', async () => {
  const p = phone();
  const client = 'dev_' + 'd'.repeat(32);
  const { put, list, fabric } = open({ [client]: { V: 1, kind: 'client', methods: [] } });
  const refused = await put(enrollment(p), { caller: client }).catch((e) => e);
  assert.equal(refused.kind, 'unauthorized');
  assert.throws(() => list(client), { kind: 'unauthorized' });
  assert.throws(() => fabric.listEnrollments({ caller: FULL, body: enc.encode('{}') }), { code: 'bad_request' });
});
