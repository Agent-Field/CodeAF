// A phone's link request (design 3.8, routes.json link.create / link.read / link.approve.client).
// The first half is pure: shapes, codes, refusals, deliveries. The second half drives the whole hosted
// Worker (Raft-hosted, the way production runs) and needs the wasm proof build, so it skips without
// CODEAF_RAFT_PROOF_DIR. Each test names the validation-contract id it maps to (design section 13).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash, createPrivateKey, generateKeyPairSync, randomBytes, sign } from 'node:crypto';
import { Directory } from '../src/directory.js';
import { Fabric } from '../src/fabric.js';
import { guarded } from '../src/wire.js';
import { STAGE1 } from '../src/rules.js';
import { memorySql } from './sql.js';
import {
  checkDigits, classOf, CLIENT_CODE_LENGTH, codeLengthOf, codeOf, COMPUTER_CODE_LENGTH, newCode, newRequest,
} from '../src/link/code.js';
import { approve, checkApprovalDevice, clientGrant, requireEnrolled } from '../src/link/decide.js';
import { boundLink, deliveredLink } from '../src/link/effects.js';
import { settled } from '../src/link/state.js';
import { hostedFixture } from './raft/hosted-fixture.js';

const b64u = (bytes) => Buffer.from(bytes).toString('base64url');
const fail = async (fn) => {
  try {
    await fn();
    return 'ok';
  } catch (e) {
    return [e.code, e.status].join(' ');
  }
};
const failSync = (fn) => {
  try {
    fn();
    return 'ok';
  } catch (e) {
    return [e.code, e.status].join(' ');
  }
};

const BIND = b64u(new Uint8Array(16).fill(9)); // 22 characters
const goodBody = (over = {}) => ({
  pubkey: b64u(new Uint8Array(32).fill(1)), x25519: b64u(new Uint8Array(32).fill(2)),
  name_sealed: b64u(new Uint8Array(24 + 16 + 5)), platform: 'ios', ...over,
});

// ---------------------------------------------------------------- pure

// R3, P6: the code's length is the first structural sign of a request's class.
test('R3 P6 a phone code is 10 characters and a computer code 8, both of the Crockford alphabet', () => {
  for (let i = 0; i < 200; i++) {
    assert.match(newCode(CLIENT_CODE_LENGTH), /^[0-9a-hjkmnp-tv-z]{10}$/);
    assert.match(newCode(), /^[0-9a-hjkmnp-tv-z]{8}$/);
    assert.match(newCode(COMPUTER_CODE_LENGTH), /^[0-9a-hjkmnp-tv-z]{8}$/);
  }
  assert.equal(codeLengthOf('client'), 10);
  assert.equal(codeLengthOf(undefined), 8);
  assert.equal(new Set(Array.from({ length: 1000 }, () => newCode(10))).size, 1000, 'no repeats in a thousand 10-character draws');
  // All ten characters carry entropy: the first and the last character each take more than a handful of values.
  const draws = Array.from({ length: 400 }, () => newCode(10));
  assert.ok(new Set(draws.map((c) => c[0])).size > 16);
  assert.ok(new Set(draws.map((c) => c[9])).size > 16);
});

// R3: every lookup (read, approve, deny) names a request by either length.
test('R3 every lookup accepts a code of 8 or 10 characters and nothing between or beyond', () => {
  assert.equal(codeOf('k7m2q9xd'), 'k7m2q9xd');
  assert.equal(codeOf('k7m2q9xdhz'), 'k7m2q9xdhz');
  assert.equal(codeOf('K7M2Q9XDHZ'), 'k7m2q9xdhz', 'upper case is accepted at both lengths');
  for (const bad of ['k7m2q9x', 'k7m2q9xdh', 'k7m2q9xdhzz', 'k7m2q9xdhi', 'k7m2q9xdhl', 'k7m2q9xdho', 'k7m2q9xdhu', 'k7m2q9xd h', '', null, undefined]) {
    assert.equal(codeOf(bad), null, String(bad));
  }
});

// R3, P6, P9: the create body's `class` and `bind` matrix.
test('R3 P6 class and bind: a phone names both, a computer names neither, everything else is 400', async () => {
  const phone = await newRequest(goodBody({ class: 'client', bind: BIND }));
  assert.equal(phone.class, 'client');
  assert.equal(phone.bind, BIND, 'bind is carried as sent, never computed');
  assert.equal(phone.check, await checkDigits(new Uint8Array(32).fill(1)), "the relay's own check number is still derived from the key");

  for (const over of [{}, { class: undefined }, { class: null }, { class: '' }, { class: '', bind: '' }, { class: null, bind: null }]) {
    const computer = await newRequest(goodBody(over));
    assert.equal('class' in computer, false, JSON.stringify(over));
    assert.equal('bind' in computer, false, JSON.stringify(over));
  }

  const bad = {
    'bind without a class': { bind: BIND },
    'class client without a bind': { class: 'client' },
    'class client with an empty bind': { class: 'client', bind: '' },
    'class client with a null bind': { class: 'client', bind: null },
    'an unknown class': { class: 'computer', bind: BIND },
    'an unknown class alone': { class: 'phone' },
    'a class of the wrong case': { class: 'Client', bind: BIND },
    'a numeric class': { class: 1, bind: BIND },
    'an object class': { class: {}, bind: BIND },
    'a bind of 15 bytes': { class: 'client', bind: b64u(new Uint8Array(15)) },
    'a bind of 17 bytes': { class: 'client', bind: b64u(new Uint8Array(17)) },
    'a bind of 32 bytes': { class: 'client', bind: b64u(new Uint8Array(32)) },
    'a padded bind': { class: 'client', bind: BIND + '==' },
    'a standard-alphabet bind': { class: 'client', bind: 'a+/'.padEnd(22, 'a') },
    'a bind that is not text': { class: 'client', bind: 5 },
    'a bind that is a list': { class: 'client', bind: [BIND] },
    'a bind with a space': { class: 'client', bind: BIND.slice(0, 21) + ' ' },
  };
  for (const [what, over] of Object.entries(bad)) assert.equal(await fail(() => newRequest(goodBody(over))), 'bad_request 400', what);
});

test('R3 classOf is the whole class check, and answers nothing for a computer', () => {
  assert.deepEqual(classOf({}), {});
  assert.deepEqual(classOf({ class: 'client', bind: BIND }), { class: 'client', bind: BIND });
  assert.equal(failSync(() => classOf({ class: 'client' })), 'bad_request 400');
});

// P6: the phone's approve body is exactly {class, grant}.
test('P6 a phone approval body is exactly {"class":"client","grant":<base64url>}; every other body is 400', () => {
  const grant = b64u(randomBytes(64));
  assert.equal(clientGrant({ class: 'client', grant }, 4096), grant);
  assert.equal(clientGrant({ grant, class: 'client' }, 4096), grant, 'key order does not matter');
  const bad = {
    'a full-device body': { device: { V: 1 }, cert: 'x', grant },
    'a full-device body with the class': { class: 'client', device: { V: 1 }, cert: 'x', grant },
    'an extra key': { class: 'client', grant, extra: 1 },
    'a device key': { class: 'client', grant, device: {} },
    'no class': { grant },
    'no grant': { class: 'client' },
    'the wrong class': { class: 'computer', grant },
    'an upper-case class': { class: 'Client', grant },
    'a null class': { class: null, grant },
    'an empty grant': { class: 'client', grant: '' },
    'a grant that is not text': { class: 'client', grant: 7 },
    'a null grant': { class: 'client', grant: null },
    'a grant that is not base64url': { class: 'client', grant: 'a b' },
    'a standard-alphabet grant': { class: 'client', grant: 'a+/=' },
    'a grant of 4097 bytes': { class: 'client', grant: b64u(new Uint8Array(4097)) },
    'an empty object': {},
    'a list': [],
    'null': null,
    'text': 'grant',
  };
  for (const [what, body] of Object.entries(bad)) assert.equal(failSync(() => clientGrant(body, 4096)), 'bad_request 400', what);
  assert.equal(clientGrant({ class: 'client', grant: b64u(new Uint8Array(4096)) }, 4096).length > 0, true, '4096 bytes is the largest grant');
});

// R3: not registered as a client, revoked, or not enrolled is one refusal and nothing is decided.
test('R3 P10 the precondition: a non-revoked kind:client device AND a stored enrollment, else 409 not_enrolled', () => {
  const client = { kind: 'client', methods: ['agents.create'], revoked: false };
  assert.equal(failSync(() => requireEnrolled(client, true)), 'ok');
  assert.equal(failSync(() => requireEnrolled(client, false)), 'not_enrolled 409', 'registered but no enrollment');
  assert.equal(failSync(() => requireEnrolled(undefined, true)), 'not_enrolled 409', 'enrolled but not registered (absent row)');
  assert.equal(failSync(() => requireEnrolled(null, true)), 'not_enrolled 409');
  assert.equal(failSync(() => requireEnrolled({ ...client, revoked: true }, true)), 'not_enrolled 409', 'a revoked phone');
  assert.equal(failSync(() => requireEnrolled({ kind: '', revoked: false }, true)), 'not_enrolled 409', 'a full device is not a phone');
  assert.equal(failSync(() => requireEnrolled({ revoked: false }, true)), 'not_enrolled 409');
});

// P6, P7: the full-device body check refuses both crossings.
test('P6 P7 the full-device approval check refuses a phone request and a body that names a class', async () => {
  const body = { device: { V: 1, name: 'c2VhbGVk' }, cert: 'x', grant: 'AAAA' };
  const c = { request: { headers: new Headers() }, tenant: {} };
  assert.equal(await fail(() => checkApprovalDevice(c, body, { device: 'dev_a', class: 'client' })), 'bad_request 400', 'a phone request takes no full-device body');
  assert.equal(await fail(() => checkApprovalDevice(c, { ...body, class: 'client' }, { device: 'dev_a' })), 'bad_request 400', 'a computer request takes no class');
  assert.equal(await fail(() => checkApprovalDevice(c, { class: 'client', grant: 'AAAA' }, { device: 'dev_a' })), 'bad_request 400', 'a client body for a computer request');
});

// R3, P10: the delivery of a phone's grant is deterministic, so a repeat is the same and a different one is refused.
test('R3 P10 a phone grant delivery: a repeat is the same record, a different grant or a denial is already_decided', () => {
  const hash = (grant) => 'client:' + createHash('sha256').update(grant).digest('hex');
  const open = { code: 'k7m2q9xdhz', device: 'dev_a', class: 'client', bind: BIND, state: 'pending', grant: null, decided_at: 0, expires_at: 10_000 };
  const bound = boundLink(open, 'cluster-1');
  const id = hash('R1JBTlQ');
  const first = deliveredLink(bound, 'cluster-1', id, 'approved', 'R1JBTlQ', 5, 'dev_a');
  assert.deepEqual([first.state, first.grant, first.delivery, first.class, first.bind], ['approved', 'R1JBTlQ', id, 'client', BIND]);
  assert.equal(deliveredLink(first, 'cluster-1', id, 'approved', 'R1JBTlQ', 9, 'dev_a'), first, 'the identical decision changes nothing');
  assert.equal(boundLink(first, 'cluster-1'), first, 'binding again is a no-op');
  assert.equal(failSync(() => deliveredLink(first, 'cluster-1', hash('T1RIRVI'), 'approved', 'T1RIRVI', 9, 'dev_a')), 'already_decided 409', 'a different grant');
  assert.equal(failSync(() => deliveredLink(first, 'cluster-1', 'denied-id', 'denied', null, 9, 'dev_a')), 'already_decided 409', 'a denial after the approval');
  assert.equal(failSync(() => boundLink(first, 'cluster-2')), 'already_decided 409', 'another identity cannot bind the code');
  assert.equal(failSync(() => deliveredLink(bound, 'cluster-2', id, 'approved', 'R1JBTlQ', 5, 'dev_a')), 'already_decided 409', 'nor deliver to it');
  assert.equal(failSync(() => deliveredLink(bound, 'cluster-1', id, 'approved', 'R1JBTlQ', 5, 'dev_other')), 'already_decided 409', 'the device must be the request\'s');
  assert.equal(failSync(() => deliveredLink(bound, 'cluster-1', id, 'approved', null, 5, 'dev_a')), 'already_decided 409', 'an approval carries a grant');
  assert.deepEqual(settled(open, 'denied', null, 3).state, 'denied', 'a plain deny of a pending phone request still works');
});

// R3: the legacy LinkRequest.decide keeps a computer's repeat as it was, and holds a phone's to its exact grant.
test('R3 settled: a phone approval repeated with another grant is already_decided, a computer one keeps the first grant', () => {
  const computer = settled({ state: 'pending', device: 'dev_a' }, 'approved', 'ONE', 1);
  assert.equal(settled(computer, 'approved', 'TWO', 2), computer);
  const phone = settled({ state: 'pending', device: 'dev_a', class: 'client' }, 'approved', 'ONE', 1);
  assert.equal(settled(phone, 'approved', 'ONE', 2), phone);
  assert.equal(failSync(() => settled(phone, 'approved', 'TWO', 2)), 'already_decided 409');
  assert.equal(failSync(() => settled(phone, 'denied', null, 2)), 'already_decided 409');
});

// ---------------------------------------------------------------- the legacy (non-Raft) approve path

// `decide.js` `approve` is the handler routes.js names. The Raft-hosted Worker (the live one) answers an
// approval inside hosted.js `serve` and never calls it; the legacy handler must still refuse and accept the
// same way, so a fixture or an older host cannot turn a phone request into a device.
function legacy() {
  const clock = { now: 1_790_000_123_456 };
  const sql = memorySql();
  const dir = new Directory(sql, 'id_t', () => clock.now, STAGE1);
  const fabric = new Fabric(sql, () => clock.now, dir);
  const identity = generateKeyPairSync('ed25519');
  const identityKey = identity.publicKey.export({ format: 'der', type: 'spki' }).subarray(-32);
  const phoneKey = generateKeyPairSync('ed25519').publicKey.export({ format: 'der', type: 'spki' }).subarray(-32);
  const phone = 'dev_' + createHash('sha256').update(phoneKey).digest('hex').slice(0, 32);
  dir.putDevice('dev_full', { V: 1, name: 'bg==', added_by: 'id_t', revoked: false });
  const records = new Map(), joined = [], decided = [];
  const box = (code) => ({
    read: () => guarded(() => records.get(code)),
    decide: (state, grant) => guarded(() => {
      const record = records.get(code);
      records.set(code, settled(record, state, grant, clock.now));
    }),
  });
  const c = (code, body) => ({
    body: new TextEncoder().encode(JSON.stringify(body)),
    request: { headers: new Headers({ 'codeaf-identity': b64u(identityKey), 'codeaf-cert': 'x' }) },
    env: {
      LINK_REQUEST: { idFromName: (name) => name, get: box },
      LINK_GATE: { idFromName: () => 'gate', get: () => ({ decided: (done) => guarded(() => decided.push(done)) }) },
    },
    tenant: { dir, fabric, identity: 'id_t', limits: { linkMaxGrant: 4096 }, join: (id) => joined.push(id) },
    code,
  });
  const open = (code, over = {}) => records.set(code, { code, device: phone, state: 'pending', grant: null, decided_at: 0, expires_at: clock.now + 600_000, ...over });
  const run = (code, body) => fail(() => approve(c(code, body), [code]));
  const enroll = () => {
    const e = { v: 1, device: phone, sign_pub: Buffer.from(phoneKey).toString('base64'), box_pub: Buffer.alloc(32, 4).toString('base64'), methods: ['agents.create'], reads: [], ver: 1 };
    const text = 'codeaf-enroll-v1\n' + phone + '\n' + Buffer.from(phoneKey).toString('hex') + '\n' + Buffer.alloc(32, 4).toString('hex') + '\nagents.create\n\n1';
    e.sig = sign(null, Buffer.from(text), identity.privateKey).toString('base64');
    return fabric.putEnrollment({ caller: 'dev_full', targetDevice: phone, body: new TextEncoder().encode(JSON.stringify(e)), identityHeader: b64u(identityKey) });
  };
  return { dir, fabric, records, joined, decided, open, run, enroll, phone };
}

test('R3 P6 P10 the legacy approve path answers a phone request the way the hosted one does', async () => {
  const w = legacy();
  const grant = b64u(randomBytes(80));
  w.open('k7m2q9xdhz', { class: 'client', bind: BIND });
  assert.equal(await w.run('k7m2q9xdhz', { class: 'client', grant }), 'not_enrolled 409', 'nothing registered or enrolled');
  w.dir.putClientDevice(w.phone, { V: 1, methods: ['agents.create'], name: '', added_by: 'id_t' });
  assert.equal(await w.run('k7m2q9xdhz', { class: 'client', grant }), 'not_enrolled 409', 'registered, not enrolled');
  assert.equal(w.records.get('k7m2q9xdhz').state, 'pending');
  await w.enroll();

  // The crossings are 400 and leave the request pending.
  for (const body of [{ device: { V: 1 }, cert: 'x', grant }, { class: 'client', grant, extra: 1 }, { grant }, { class: 'client' }, { class: 'client', grant: b64u(new Uint8Array(4097)) }]) {
    assert.equal(await w.run('k7m2q9xdhz', body), 'bad_request 400', JSON.stringify(Object.keys(body)));
  }
  assert.equal(w.records.get('k7m2q9xdhz').state, 'pending');

  const before = JSON.stringify([w.dir.version, w.dir.read('devices', w.phone)]);
  assert.equal(await w.run('k7m2q9xdhz', { class: 'client', grant }), 'ok');
  assert.deepEqual([w.records.get('k7m2q9xdhz').state, w.records.get('k7m2q9xdhz').grant], ['approved', grant]);
  assert.deepEqual(w.joined, [], 'no device was joined');
  assert.equal(JSON.stringify([w.dir.version, w.dir.read('devices', w.phone)]), before, 'the directory is untouched');
  assert.deepEqual(w.decided, ['k7m2q9xdhz']);
  assert.equal(await w.run('k7m2q9xdhz', { class: 'client', grant }), 'ok', 'a repeat');
  assert.equal(await w.run('k7m2q9xdhz', { class: 'client', grant: b64u(randomBytes(80)) }), 'already_decided 409', 'a different grant');

  // A computer request refuses the client body and still takes its own.
  w.open('k7m2q9xd');
  assert.equal(await w.run('k7m2q9xd', { class: 'client', grant }), 'bad_request 400');
  assert.equal(w.records.get('k7m2q9xd').state, 'pending');
});

// ---------------------------------------------------------------- the hosted Worker

const PROOF = process.env.CODEAF_RAFT_PROOF_DIR;
const skip = PROOF ? false : 'CODEAF_RAFT_PROOF_DIR is unset: the hosted Worker needs the wasm proof build (core.wasm)';
const LABEL = 'link-client-root';
const rootKey = () => createPrivateKey({
  key: Buffer.concat([Buffer.from('302e020100300506032b657004220420', 'hex'), createHash('sha256').update(LABEL).digest()]),
  format: 'der', type: 'pkcs8',
});

// A world: one identity, a full computer `a` registered in it, and a phone `p` that has not been registered.
async function world() {
  const h = await hostedFixture({ rootLabel: LABEL });
  const a = h.client(), p = h.client();
  await h.start();
  const fullRecord = { V: 1, name: 'c2VhbGVk', added_by: h.identity, revoked: false, caps: { os: 'linux', arch: 'amd64', sandbox: null, container: null, gpu: null, cow: 'none' } };
  const reg = await h.call(a, 'PUT', '/v1/dir/devices/' + a.device, fullRecord);
  assert.equal(reg.status, 204, JSON.stringify(reg));
  const phoneKeys = { sign: Buffer.from(p.cert.device, 'hex'), box: randomBytes(32) };
  const open = async (over = {}, who = p) => {
    const r = await h.mf.dispatchFetch('http://local/v1/link/requests', {
      method: 'POST',
      body: JSON.stringify({ pubkey: Buffer.from(who.cert.device, 'hex').toString('base64url'), x25519: phoneKeys.box.toString('base64url'), name_sealed: randomBytes(45).toString('base64url'), platform: 'ios', class: 'client', bind: BIND, ...over }),
    });
    return { status: r.status, body: await r.json() };
  };
  const read = async (code) => {
    const r = await h.mf.dispatchFetch('http://local/v1/link/requests/' + code);
    return { status: r.status, body: await r.json() };
  };
  const register = (methods = ['agents.create']) => h.call(a, 'PUT', '/v1/clients/' + p.device, { cert: p.cert, methods, name: 'bmFtZQ' });
  const enroll = (over = {}) => {
    const e = { v: 1, device: p.device, sign_pub: phoneKeys.sign.toString('base64'), box_pub: phoneKeys.box.toString('base64'), methods: ['agents.create'], reads: ['agents.list'], ver: 1, ...over };
    const text = 'codeaf-enroll-v1\n' + e.device + '\n' + Buffer.from(e.sign_pub, 'base64').toString('hex') + '\n' + Buffer.from(e.box_pub, 'base64').toString('hex') + '\n' + [...e.methods].sort().join(',') + '\n' + [...e.reads].sort().join(',') + '\n' + e.ver;
    e.sig = sign(null, Buffer.from(text), rootKey()).toString('base64');
    return h.call(a, 'PUT', '/v1/clients/' + p.device + '/enroll', e);
  };
  const approve = (code, body, who = a) => h.call(who, 'POST', '/v1/dir/requests/' + code + '/approve', body);
  const listing = async () => {
    const r = await h.request(a, 'GET', '/v1/dir/list');
    return { status: r.status, version: r.headers.get('codeaf-dir-version'), body: await r.json() };
  };
  return { h, a, p, open, read, register, enroll, approve, listing };
}

const grantOf = () => b64u(randomBytes(120));
const without = (listing) => ({ devices: listing.body.devices, cells: listing.body.cells, version: listing.version });

// R3, P6: create and read.
test('R3 P6 P9 a phone request: 10-character code, class echoed, class and bind served back verbatim', { skip, timeout: 60000 }, async () => {
  const w = await world();
  try {
    const made = await w.open();
    assert.equal(made.status, 201, JSON.stringify(made));
    assert.match(made.body.code, /^[0-9a-hjkmnp-tv-z]{10}$/);
    assert.equal(made.body.class, 'client');
    assert.equal(made.body.device, w.p.device);
    assert.match(made.body.check, /^\d{4}$/);
    assert.equal('bind' in made.body, false, 'create answers the class, the phone already has its bind');
    assert.ok(made.body.expires_at > made.body.requested_at);

    const read = await w.read(made.body.code);
    assert.equal(read.status, 200);
    assert.equal(read.body.state, 'pending');
    assert.equal(read.body.class, 'client');
    assert.equal(read.body.bind, BIND);
    assert.equal(read.body.check, made.body.check);
    assert.equal((await w.read(made.body.code.toUpperCase())).body.code, made.body.code, 'lookup ignores case at the long length too');

    // A computer's request is untouched: 8 characters, no class, no bind.
    const other = await w.open({ class: undefined, bind: undefined });
    assert.equal(other.status, 201);
    assert.match(other.body.code, /^[0-9a-hjkmnp-tv-z]{8}$/);
    assert.equal('class' in other.body, false);
    const otherRead = await w.read(other.body.code);
    assert.equal('class' in otherRead.body, false);
    assert.equal('bind' in otherRead.body, false);
  } finally {
    await w.h.close();
  }
});

// R3: the create matrix over the wire, so the Worker front answers 400 and not a thrown error.
test('R3 P6 the create matrix over the wire: every class/bind mistake is 400 bad_request', { skip, timeout: 60000 }, async () => {
  const w = await world();
  try {
    for (const over of [
      { bind: undefined }, { class: undefined }, { class: 'computer' }, { bind: b64u(new Uint8Array(15)) }, { bind: b64u(new Uint8Array(17)) },
      { bind: BIND + '=' }, { class: 5 }, { bind: 5 },
    ]) {
      const r = await w.open(over);
      assert.deepEqual([r.status, r.body], [400, { err: 'bad_request' }], JSON.stringify(over));
    }
    assert.equal((await w.open({ class: undefined, bind: undefined })).status, 201, 'a computer request with neither is fine');
  } finally {
    await w.h.close();
  }
});

// P9: the relay stores whatever bind it is given and never judges it.
test('P9 a substituted-key bind is stored as sent and served back unchanged, before and after the decision', { skip, timeout: 60000 }, async () => {
  const w = await world();
  try {
    const attacker = b64u(randomBytes(16));
    const made = await w.open({ bind: attacker });
    assert.equal(made.status, 201);
    assert.equal((await w.read(made.body.code)).body.bind, attacker);
    assert.equal((await w.register()).status, 204);
    assert.equal((await w.enroll()).status, 204);
    const grant = grantOf();
    assert.equal((await w.approve(made.body.code, { class: 'client', grant })).status, 204);
    const after = await w.read(made.body.code);
    assert.deepEqual([after.body.state, after.body.bind, after.body.grant], ['approved', attacker, grant]);
  } finally {
    await w.h.close();
  }
});

// R3, P10 (relay half): enroll, register, then decide, and the refusals before that.
test('R3 P10 decide before register and enroll is 409 not_enrolled and the request stays pending', { skip, timeout: 60000 }, async () => {
  const w = await world();
  try {
    const { body: { code } } = await w.open();
    const grant = grantOf();
    // Nothing registered, nothing enrolled.
    assert.deepEqual(await w.approve(code, { class: 'client', grant }), { status: 409, body: { err: 'not_enrolled' } });
    assert.equal((await w.read(code)).body.state, 'pending');
    // Registered, not enrolled.
    assert.equal((await w.register()).status, 204);
    assert.deepEqual(await w.approve(code, { class: 'client', grant }), { status: 409, body: { err: 'not_enrolled' } });
    assert.equal((await w.read(code)).body.state, 'pending');
    assert.equal((await w.read(code)).body.grant, null);
  } finally {
    await w.h.close();
  }
});

test('R3 P10 enrolled but not registered is 409 not_enrolled too (an enrollment alone admits nothing)', { skip, timeout: 60000 }, async () => {
  const w = await world();
  try {
    const { body: { code } } = await w.open();
    assert.equal((await w.enroll()).status, 204);
    assert.deepEqual(await w.approve(code, { class: 'client', grant: grantOf() }), { status: 409, body: { err: 'not_enrolled' } });
    assert.equal((await w.read(code)).body.state, 'pending');
    assert.equal((await w.register()).status, 204);
    assert.equal((await w.approve(code, { class: 'client', grant: grantOf() })).status, 204, 'the order of enroll and register does not matter');
  } finally {
    await w.h.close();
  }
});

// R3, P10, P6: the full happy path, the phone's read, idempotence.
test('R3 P10 enroll, register, decide: 204, the phone reads approved with its grant, a repeat is 204, a different one is 409', { skip, timeout: 90000 }, async () => {
  const w = await world();
  try {
    const { body: { code } } = await w.open();
    assert.equal((await w.enroll()).status, 204);
    assert.equal((await w.register()).status, 204);
    const grant = grantOf();
    const done = await w.approve(code, { class: 'client', grant });
    assert.deepEqual(done, { status: 204, body: null });
    const read = await w.read(code);
    assert.deepEqual([read.status, read.body.state, read.body.grant, read.body.class, read.body.bind], [200, 'approved', grant, 'client', BIND]);
    assert.equal('cluster' in read.body, false, "the relay's bookkeeping is not served");
    assert.equal('delivery' in read.body, false);
    assert.equal((await w.approve(code, { class: 'client', grant })).status, 204, 'the identical decision again');
    assert.deepEqual(await w.approve(code, { class: 'client', grant: grantOf() }), { status: 409, body: { err: 'already_decided' } });
    assert.equal((await w.read(code)).body.grant, grant, 'the first grant stands');
    assert.deepEqual(await w.h.call(w.a, 'POST', '/v1/dir/requests/' + code + '/deny'), { status: 409, body: { err: 'already_decided' } }, 'a denial after an approval');
    assert.equal((await w.read(code)).body.state, 'approved');
    // The directory still answers (a refused denial left nothing for the maintenance replay to choke on).
    assert.equal((await w.listing()).status, 200);
    // A restart keeps the decision.
    await w.h.start();
    assert.equal((await w.approve(code, { class: 'client', grant })).status, 204);
    assert.equal((await w.read(code)).body.state, 'approved');
  } finally {
    await w.h.close();
  }
});

// R3: no device row, nothing committed to the directory.
test('R3 P6 a phone decision writes no device row: the directory is identical before and after, and the phone is still the client it was registered as', { skip, timeout: 90000 }, async () => {
  const w = await world();
  try {
    const { body: { code } } = await w.open();
    assert.equal((await w.enroll()).status, 204);
    assert.equal((await w.register(['agents.create', 'agents.send'])).status, 204);
    const before = await w.listing();
    assert.equal(before.body.devices[w.p.device].kind, 'client');
    assert.deepEqual(before.body.devices[w.p.device].methods, ['agents.create', 'agents.send']);
    assert.equal((await w.approve(code, { class: 'client', grant: grantOf() })).status, 204);
    const after = await w.listing();
    assert.deepEqual(without(after), without(before), 'same devices, same cells, same directory version');
    assert.equal(after.body.devices[w.p.device].kind, 'client');
    assert.deepEqual(after.body.devices[w.p.device].methods, ['agents.create', 'agents.send']);
    assert.equal(after.body.devices[w.p.device].revoked, false);
    // Neither a repeat nor a restart writes one.
    assert.equal((await w.approve(code, { class: 'client', grant: 'x' })).status >= 400, true);
    await w.h.start();
    assert.deepEqual(without(await w.listing()), without(before));
  } finally {
    await w.h.close();
  }
});

// P6, P7: the crossings are refused and leave the request pending.
test('P6 P7 a client request never takes a full-device body, a computer request never a client body, and both stay pending', { skip, timeout: 90000 }, async () => {
  const w = await world();
  try {
    const phone = (await w.open()).body.code;
    const computer = (await w.open({ class: undefined, bind: undefined })).body.code;
    assert.equal((await w.enroll()).status, 204);
    assert.equal((await w.register()).status, 204);
    const grant = grantOf();
    const cert = Buffer.from(JSON.stringify(w.p.cert)).toString('base64url');
    const full = { device: { V: 1, name: 'c2VhbGVk', platform: 'ios' }, cert, grant };
    const bad = (r, what) => assert.deepEqual([r.status, r.body], [400, { err: 'bad_request' }], what);

    bad(await w.approve(phone, full), 'a full-device body for a phone request');
    bad(await w.approve(phone, { ...full, class: 'client' }), 'a full-device body that also says client');
    bad(await w.approve(phone, { class: 'client', grant, extra: true }), 'an extra key');
    bad(await w.approve(phone, { class: 'client', grant, device: full.device }), 'a device key beside the class');
    bad(await w.approve(phone, { grant }), 'no class');
    bad(await w.approve(phone, { class: 'computer', grant }), 'the wrong class');
    bad(await w.approve(phone, { class: 'client' }), 'no grant');
    bad(await w.approve(phone, { class: 'client', grant: '' }), 'an empty grant');
    bad(await w.approve(phone, { class: 'client', grant: 'not base64url!' }), 'a grant that is not base64url');
    bad(await w.approve(phone, { class: 'client', grant: b64u(new Uint8Array(4097)) }), 'a grant of 4097 bytes decoded');
    bad(await w.approve(phone, {}), 'an empty body');
    assert.equal((await w.read(phone)).body.state, 'pending', 'not one of them decided it');

    bad(await w.approve(computer, { class: 'client', grant }), 'a client body for a computer request');
    bad(await w.approve(computer, { ...full, class: 'client' }), 'a computer body that names a class');
    assert.equal((await w.read(computer)).body.state, 'pending');
    // 4096 bytes is the largest accepted.
    assert.equal((await w.approve(phone, { class: 'client', grant: b64u(new Uint8Array(4096).fill(3)) })).status, 204);
  } finally {
    await w.h.close();
  }
});

// R3, P6: a phone cannot approve, and a revoked phone cannot be approved.
test('R3 only a full device decides: the phone itself is refused, and a revoked phone is 409 not_enrolled', { skip, timeout: 90000 }, async () => {
  const w = await world();
  try {
    const { body: { code } } = await w.open();
    assert.equal((await w.enroll()).status, 204);
    assert.equal((await w.register()).status, 204);
    const byPhone = await w.approve(code, { class: 'client', grant: grantOf() }, w.p);
    assert.equal(byPhone.status, 401, 'a client is not a full device');
    assert.equal((await w.read(code)).body.state, 'pending');
    assert.equal((await w.h.call(w.a, 'POST', '/v1/dir/devices/' + w.p.device + '/revoke')).status, 204);
    assert.deepEqual(await w.approve(code, { class: 'client', grant: grantOf() }), { status: 409, body: { err: 'not_enrolled' } });
    assert.equal((await w.read(code)).body.state, 'pending');
  } finally {
    await w.h.close();
  }
});

// R3: unknown and denied.
test('R3 an unknown 10-character code is 404, and a deny works for a phone request and writes nothing', { skip, timeout: 90000 }, async () => {
  const w = await world();
  try {
    assert.deepEqual(await w.approve('zzzzzzzzzz', { class: 'client', grant: grantOf() }), { status: 404, body: { err: 'gone' } });
    assert.equal((await w.read('zzzzzzzzzz')).status, 404);
    const { body: { code } } = await w.open();
    const before = await w.listing();
    assert.equal((await w.h.call(w.a, 'POST', '/v1/dir/requests/' + code + '/deny')).status, 204);
    const read = await w.read(code);
    assert.deepEqual([read.body.state, read.body.grant, read.body.class, read.body.bind], ['denied', null, 'client', BIND]);
    assert.deepEqual(without(await w.listing()), without(before), 'a denial adds no device');
    assert.equal((await w.h.call(w.a, 'POST', '/v1/dir/requests/' + code + '/deny')).status, 204, 'a repeated denial');
    // An approval after a denial is the already-decided answer, even with everything in place.
    assert.equal((await w.enroll()).status, 204);
    assert.equal((await w.register()).status, 204);
    assert.deepEqual(await w.approve(code, { class: 'client', grant: grantOf() }), { status: 409, body: { err: 'already_decided' } });
  } finally {
    await w.h.close();
  }
});
