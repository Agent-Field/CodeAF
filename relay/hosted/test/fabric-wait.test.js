// The slot wait and the fabric routes through the real HostedIdentity methods (dispatch, serveFabric, slotWait,
// parkSlotWait, waitIO, stopIO, serve), with the parts that need the Raft core replaced by a stand-in directory: the
// object is built without its constructor (that needs the WASI core, which these tests do not load) and given exactly the
// collaborators the methods use. What is proved is the STRUCTURE the design asks for (design 3.7, 7.3; A0 (d)): a held
// wait never sits inside the coordination queue, a revoke ends it, the caps hold, and one door answers every fabric route.
// Contract ids in each name (design 13). The Raft-backed worker fixtures (test/raft/hosted-*.test.js) need the built core
// and are not run here; the same routes are covered end to end by the shared conformance suite, which the owner runs.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { createHash, generateKeyPairSync, sign } from 'node:crypto';
import { Fabric } from '../src/fabric.js';
import { HostedIdentity } from '../src/raft/hosted.js';
import { matchRoute } from '../src/routes.js';
import { Wire, wireOf } from '../src/wire.js';
import { memorySql } from './sql.js';
import { watchRig } from './clientrig.js';

const enc = new TextEncoder();
const bytes = (value) => enc.encode(JSON.stringify(value));
const FULL = 'dev_' + 'a'.repeat(32);
const PHONE = 'dev_' + 'b'.repeat(32);
const OTHER = 'dev_' + 'c'.repeat(32);
const RUNNER = 'dev_' + 'd'.repeat(32);
const unsegment = (hex) => new TextDecoder().decode(Uint8Array.from(hex.match(/../g) ?? [], (h) => parseInt(h, 16)));
const tick = () => new Promise((r) => setImmediate(r));
const identity = generateKeyPairSync('ed25519');
const identityKey = identity.publicKey.export({ format: 'der', type: 'spki' }).subarray(-32).toString('base64url');

function rig({ slotWaitMax = 32 } = {}) {
  const w = watchRig();
  const devices = {
    [FULL]: { V: 1, last_seen: 100 },
    [RUNNER]: { V: 1, last_seen: 200 },
    [PHONE]: { V: 1, kind: 'client', methods: ['agents.send'] },
    [OTHER]: { V: 1, kind: 'client', methods: ['agents.send'] },
  };
  const fabric = new Fabric(memorySql(), () => w.clock.now, { read: () => undefined });
  const host = Object.create(HostedIdentity.prototype);
  Object.assign(host, {
    operations: 0,
    operationTail: Promise.resolve(),
    pendingIO: new Map(),
    policy: { tick_every: 5_000 },
    watchers: w.watchers,
    tenant: { fabric, limits: { slotWaitMax, maxHolds: 16 }, watchers: w.watchers, identity: 'id_x' },
    // What the Raft core answers, in the shape the methods read it: a device row per id, refusals for a revoked or unknown caller.
    ensure: async () => {},
    wakeMaintenance: async () => {},
    authorize: async (device) => {
      const row = devices[device];
      if (!row) throw new Wire('unauthorized', 401);
      if (row.revoked) throw new Wire('revoked', 401);
      return { barrier: {}, app: { now: String(w.clock.now), version: '7' }, identity: {}, actor: row };
    },
    readNow: () => w.clock.now,
    readView: async (_b, _ns, key) => {
      const row = devices[unsegment(key.slice('device/'.length))];
      return { raw: row ? JSON.stringify(row) : null };
    },
    project: (_kind, raw) => JSON.parse(raw),
    listingJSON: async (_b, app) => JSON.stringify({ now: app.now, identity: null, devices }),
    finishRead: async (_b, reply) => reply,
    recheckAuthority: async () => {},
    holds: { opened() {}, next: null, renew: async () => {} },
  });
  host.endWaitsOnRevoke();
  const request = (method, path, { signal, body } = {}) =>
    new Request('https://relay.test' + path, {
      method,
      signal,
      headers: { 'codeaf-identity': identityKey, 'codeaf-cert': 'cert', 'codeaf-time': String(w.clock.now) },
      ...(body ? { body } : {}),
    });
  // call runs one request through the host's own door and answers {status, json, headers}, or the refusal the wire would send.
  const call = async (method, path, device, { body = new Uint8Array(), signal } = {}) => {
    const route = matchRoute(method, path.split('?')[0]);
    try {
      const reply = await host.dispatch({ tenant: host.tenant, device, body, request: request(method, path, { signal }) }, route);
      const text = reply.status === 204 ? '' : await reply.text();
      return { status: reply.status, json: text ? JSON.parse(text) : null, headers: reply.headers };
    } catch (e) {
      const wire = wireOf(e);
      return { status: wire.status, err: wire.code };
    }
  };
  const put = (target, name, as = FULL) => call('PUT', `/v1/clients/${target}/slots/${name}`, as, { body: bytes({ sealed: 'YQ==' }) });
  return { w, host, devices, fabric, call, put, request };
}

// settled answers what a promise did, or 'pending' if it did nothing within a few turns of the event loop.
async function settled(promise) {
  let state = 'pending';
  promise.then((v) => (state = v), (e) => (state = e));
  for (let i = 0; i < 20 && state === 'pending'; i++) await tick();
  return state;
}

test('R1, L7: a held wait is not in the coordination queue: other operations run while it is parked, and a PUT wakes it with what is newer', async () => {
  const r = rig();
  try {
    const wait = r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=25`, PHONE);
    await tick();
    assert.equal(r.host.operations, 0, 'the first operation ended once the wait was registered');
    assert.equal(r.host.pendingIO.get(PHONE).size, 1, 'the wait is filed where a revoke can find it');
    assert.equal(await settled(r.host.operation(async () => 'a raft tick ran')), 'a raft tick ran', 'the queue is not held for 25 s');
    assert.equal(r.w.clients.headerOn(PHONE), true, 'a parked wait is watching');
    assert.equal(await settled(wait), 'pending', 'and it is still waiting');

    const written = await r.put(PHONE, 'card');
    assert.equal(written.status, 204);
    assert.equal(written.headers.get('codeaf-client-watching'), '1', 'the writing computer is told the phone is waiting');
    const answered = await wait;
    assert.equal(answered.status, 200);
    assert.deepEqual(answered.json, { version: 1, slots: ['card'] });
    assert.equal(r.host.pendingIO.size, 0, 'the wait left nothing filed');
  } finally {
    r.w.restore();
  }
});

test('R1: a wait that nothing wakes ends 204 when its time is over, and holds nothing afterwards', async () => {
  const r = rig();
  try {
    const started = performance.now();
    const answered = await r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=0.05`, PHONE);
    assert.equal(answered.status, 204);
    assert.ok(performance.now() - started >= 40, 'it did wait');
    assert.equal(r.host.pendingIO.size, 0);
    const again = await Promise.all([1, 2].map(() => r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=0.05`, PHONE)));
    assert.deepEqual(again.map((a) => a.status), [204, 204], 'the place it held was released');
  } finally {
    r.w.restore();
  }
});

test('R1: a wait with something newer answers 200 at once and parks nothing; wait 0 with nothing newer is 204 at once', async () => {
  const r = rig();
  try {
    await r.put(PHONE, 'card');
    const now = await r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=25`, PHONE);
    assert.deepEqual([now.status, now.json], [200, { version: 1, slots: ['card'] }]);
    assert.equal(r.host.pendingIO.size, 0);
    const none = await r.call('GET', `/v1/clients/${PHONE}/slots?since=1&wait=0`, PHONE);
    assert.equal(none.status, 204);
    assert.equal((await r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=26`, PHONE)).err, 'bad_request');
    assert.equal((await r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=1`, FULL)).err, 'unauthorized', 'a computer may not wait on a phone\'s slots');
  } finally {
    r.w.restore();
  }
});

test('V4, R1: a revoke ends a parked wait with 401 revoked, through stopIO as a committed revoke does, and the phone stops watching', async () => {
  const r = rig();
  try {
    const wait = r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=25`, PHONE);
    const other = r.call('GET', `/v1/clients/${OTHER}/slots?since=0&wait=0.2`, OTHER);
    await tick();
    r.devices[PHONE].revoked = true;
    // What notifyApplied does for every revoked device it finds: wipe the client, then end what is parked for it.
    r.fabric.revokeClient(PHONE);
    r.host.stopIO(PHONE, 'revoked');
    const ended = await wait;
    assert.deepEqual([ended.status, ended.err], [401, 'revoked']);
    assert.equal(r.w.clients.headerOn(PHONE), false);
    assert.equal((await other).status, 204, 'another phone\'s wait is untouched');
    assert.equal(r.host.pendingIO.size, 0);
  } finally {
    r.w.restore();
  }
});

test('V4, R1: whichever door a revoke comes through (a fabric revoke, a request that finds the phone revoked) ends the wait and wipes the phone\'s state', async () => {
  const r = rig();
  try {
    await r.put(PHONE, 'card');
    const wait = r.call('GET', `/v1/clients/${PHONE}/slots?since=1&wait=25`, PHONE);
    await tick();
    r.devices[PHONE].revoked = true;
    // A computer writes a slot for the revoked phone: serveFabric finds the row revoked and wipes the client, which must end the wait.
    const refused = await r.put(PHONE, 'card');
    assert.equal(refused.err, 'not_found');
    const ended = await wait;
    assert.deepEqual([ended.status, ended.err], [401, 'revoked']);
    assert.equal(r.fabric.sql.exec('SELECT COUNT(*) AS n FROM fabric_slots').one().n, 0, 'V4: the slots are deleted');
  } finally {
    r.w.restore();
  }
});

test('R1: at most 2 waits per phone and slotWaitMax per identity are held (429 full past either); slotWaitMax 0 holds none and answers 204 at once', async () => {
  const r = rig({ slotWaitMax: 3 });
  try {
    const wait = (device, as = device) => r.call('GET', `/v1/clients/${device}/slots?since=0&wait=25`, as);
    const a1 = wait(PHONE);
    const a2 = wait(PHONE);
    await tick();
    assert.equal((await wait(PHONE)).err, 'full', 'a third wait for one phone');
    const b1 = wait(OTHER);
    await tick();
    const refused = await wait(OTHER);
    assert.deepEqual([refused.status, refused.err], [429, 'full'], 'the identity holds 3 and the fourth is refused');
    // One PUT each ends them all.
    await r.put(PHONE, 'card');
    await r.put(OTHER, 'card');
    assert.deepEqual((await Promise.all([a1, a2, b1])).map((a) => a.status), [200, 200, 200]);
    assert.equal(r.host.pendingIO.size, 0);
    assert.equal((await r.call('GET', `/v1/clients/${PHONE}/slots?since=1&wait=0.05`, PHONE)).status, 204, 'the places were given back: a new wait is admitted again');
  } finally {
    r.w.restore();
  }
  const off = rig({ slotWaitMax: 0 });
  try {
    const started = performance.now();
    const answered = await off.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=25`, PHONE);
    assert.equal(answered.status, 204, 'the kill switch answers at once');
    assert.ok(performance.now() - started < 1_000);
    assert.equal(off.host.pendingIO.size, 0);
    await off.put(PHONE, 'card');
    assert.equal((await off.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=25`, PHONE)).status, 200, 'an answer that is already there is still given');
  } finally {
    off.w.restore();
  }
});

test('R1: a client that gives up on its wait frees its place at once', async () => {
  const r = rig();
  try {
    const abort = new AbortController();
    const given = r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=25`, PHONE, { signal: abort.signal });
    const second = r.call('GET', `/v1/clients/${PHONE}/slots?since=0&wait=25`, PHONE);
    await tick();
    assert.equal(r.host.pendingIO.get(PHONE).size, 2);
    abort.abort();
    assert.equal((await given).status, 204, 'nobody is left to read the answer, but the place is released');
    assert.equal(r.host.pendingIO.get(PHONE).size, 1);
    await r.put(PHONE, 'card');
    assert.equal((await second).status, 200);
  } finally {
    r.w.restore();
  }
});

test('R1, L8: computers is served through the same door with the identity\'s whole listing, and says which runner is alive', async () => {
  const r = rig();
  try {
    r.devices['dev_' + 'e'.repeat(32)] = { V: 1, revoked: true };
    r.w.dial(FULL, 10, { serving: true });
    r.w.dial(RUNNER, 10);
    const answered = await r.call('GET', `/v1/clients/${PHONE}/computers`, PHONE);
    assert.equal(answered.status, 200);
    assert.deepEqual(answered.json, {
      now: r.w.clock.now,
      computers: [
        { device: FULL, serving: true, at: r.w.clock.now },
        { device: RUNNER, serving: false, at: r.w.clock.now },
      ],
    });
    r.w.clock.now += 26_000;
    const later = await r.call('GET', `/v1/clients/${PHONE}/computers`, PHONE);
    assert.deepEqual(later.json.computers, [
      { device: FULL, serving: false, at: 100 },
      { device: RUNNER, serving: false, at: 200 },
    ], 'the runner missed its beats: not serving, and at is when it was last seen');
    assert.equal((await r.call('GET', `/v1/clients/${PHONE}/computers`, FULL)).err, 'unauthorized');
    assert.equal((await r.call('GET', `/v1/clients/${OTHER}/computers`, PHONE)).err, 'unauthorized', 'only its own device');
  } finally {
    r.w.restore();
  }
});

test('R1, R2: enroll works while the device has no directory row, GET /v1/clients needs no device, and a phone may do neither', async () => {
  const r = rig();
  try {
    const key = generateKeyPairSync('ed25519');
    const signPub = key.publicKey.export({ format: 'der', type: 'spki' }).subarray(-32);
    const device = 'dev_' + createHash('sha256').update(signPub).digest('hex').slice(0, 32);
    assert.equal(r.devices[device], undefined, 'the phone is not registered');
    const e = { v: 1, device, sign_pub: signPub.toString('base64'), box_pub: Buffer.alloc(32, 3).toString('base64'), methods: ['agents.send'], reads: ['agents.list'], ver: 1, sig: '' };
    const text = `codeaf-enroll-v1\n${device}\n${signPub.toString('hex')}\n${Buffer.alloc(32, 3).toString('hex')}\nagents.send\nagents.list\n1`;
    e.sig = sign(null, Buffer.from(text), identity.privateKey).toString('base64');
    const stored = await r.call('PUT', `/v1/clients/${device}/enroll`, FULL, { body: bytes(e) });
    assert.equal(stored.status, 204);
    const listed = await r.call('GET', '/v1/clients', RUNNER);
    assert.deepEqual([listed.status, listed.json], [200, { clients: [{ device, enroll: e }] }]);
    assert.equal((await r.call('GET', '/v1/clients', PHONE)).status, 401);
    assert.equal((await r.call('PUT', `/v1/clients/${device}/enroll`, PHONE, { body: bytes(e) })).status, 401);
    assert.equal((await r.call('PUT', `/v1/clients/${device}/enroll`, FULL, { body: bytes({ ...e, ver: 0 }) })).status, 400);
    assert.equal((await r.call('PUT', `/v1/clients/${device}/enroll`, FULL, { body: bytes(e) })).status, 204, 'an identical re-send');
  } finally {
    r.w.restore();
  }
});

test('L4, L7: a phone that reads a slot starts to watch: the 204 of the next slot PUT carries the header and a listening socket hears on', async () => {
  const r = rig();
  try {
    const socket = r.w.dial(FULL, 10, { events: true });
    assert.equal((await r.put(PHONE, 'card')).headers.get('codeaf-client-watching'), null);
    assert.equal((await r.call('GET', `/v1/clients/${PHONE}/slots/card`, PHONE)).status, 200);
    assert.equal((await r.put(PHONE, 'card')).headers.get('codeaf-client-watching'), '1');
    assert.deepEqual(r.w.watching(socket), [{ t: 'watching', device: PHONE, on: true, at: r.w.clock.now }]);
  } finally {
    r.w.restore();
  }
});

test('L8: the host\'s watch branch marks serving only for serving=1', async () => {
  const r = rig();
  try {
    for (const [search, want] of [['serving=1', true], ['serving=true', false], ['serving=0', false], ['', false]]) {
      const request = new Request(`https://relay.test/v1/dir/watch?events=1&beat=10${search && '&' + search}`, {
        headers: { upgrade: 'websocket', 'codeaf-identity': identityKey, 'codeaf-cert': 'cert' },
      });
      const route = matchRoute('GET', '/v1/dir/watch');
      const reply = await r.w.upgrading(() => r.host.serve({ tenant: r.host.tenant, device: FULL, body: new Uint8Array(), request }, route));
      assert.equal(reply.status, 101);
      assert.equal(r.w.p.open.at(-1).attachment.serving, want, search);
    }
  } finally {
    r.w.restore();
  }
});
