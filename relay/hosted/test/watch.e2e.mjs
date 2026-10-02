// The directory watch socket (GET /v1/dir/watch) end to end against `wrangler dev --local`.
// Usage: node watch.e2e.mjs            the wire cases (first frame, bumps, closes, refusals, cap)
//        node watch.e2e.mjs idle       pings for 40 s and shows the object's code never ran for them
//        node watch.e2e.mjs put|check <state file>   the version survives a restart of the relay
// The idle case reads the relay's log, named by WATCH_LOG; the cap case needs a relay started with
// maxWatchers 5, named by RELAY_WATCH_CAP; the restart case talks to RELAY_PERSIST.
import assert from 'node:assert/strict';
import http from 'node:http';
import { readFileSync, writeFileSync } from 'node:fs';
import { newIdentity, newDevice, signed, deviceId } from './party.js';
import { init } from './helpers.js';
import { BASE, answerOf, call } from './client.js';

const CAP = process.env.RELAY_WATCH_CAP ?? 'http://127.0.0.1:18796';
const PERSIST = process.env.RELAY_PERSIST ?? 'http://127.0.0.1:18795';
const WIRE = /^\{"v":\d+\}$/;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
const record = { V: 1, name: '', added_by: '', revoked: false, caps: { os: 'linux', arch: 'arm64', sandbox: null, container: null, gpu: null, cow: 'none' } };

/** openWatch dials the watch route signed by dev and answers a socket that remembers every text frame. */
async function openWatch(dev, base = BASE, query = '') {
  const headers = await signed(dev, 'GET', `/v1/dir/watch${query}`);
  const ws = new WebSocket(base.replace(/^http/, 'ws') + `/v1/dir/watch${query}`, { headers });
  const w = { frames: [], cursor: 0, ws, send: (text) => ws.send(text), close: () => ws.close(), wake: () => {} };
  w.closed = new Promise((done) => ws.addEventListener('close', (e) => done({ code: e.code, reason: e.reason })));
  ws.addEventListener('message', (e) => (w.frames.push(e.data), w.wake()));
  await new Promise((open, fail) => (ws.addEventListener('open', open), ws.addEventListener('error', () => fail(new Error('upgrade refused')))));
  return w;
}

/** waitFor answers once pred(w) holds, waking on each frame; it fails after ms, the time being the only clock. */
async function waitFor(w, pred, ms = 5000) {
  const deadline = Date.now() + ms;
  while (!pred(w)) {
    const left = deadline - Date.now();
    assert.ok(left > 0, `gave up after ${ms} ms`);
    await new Promise((wake) => ((w.wake = wake), setTimeout(wake, left)));
  }
}

/** until polls pred every 50 ms, for a condition that has no event to wake on (the relay's log file). */
async function until(pred, ms, what) {
  for (const deadline = Date.now() + ms; !pred(); await sleep(50)) assert.ok(Date.now() < deadline, what);
}

/** next answers the first frame this socket has not been read for, as text. */
async function next(w, ms) {
  await waitFor(w, () => w.cursor < w.frames.length, ms);
  return w.frames[w.cursor++];
}

/** nextVersion answers the number in the next frame, having checked the frame is nothing but that. */
async function nextVersion(w, ms) {
  const text = await next(w, ms);
  assert.match(text, WIRE);
  return JSON.parse(text).v;
}

/** quiet asserts no frame arrives for ms: the absence of a push is the thing measured. */
async function quiet(w, ms) {
  await sleep(ms);
  assert.equal(w.cursor, w.frames.length, 'no frame while nothing changed');
}

/** refusal dials the route by hand, asking to upgrade, and answers {status, json} of the HTTP refusal. */
function refusal(headers, base = BASE, upgrade = true, path = '/v1/dir/watch') {
  const url = new URL(base);
  const asks = upgrade ? { connection: 'Upgrade', upgrade: 'websocket', 'sec-websocket-version': '13', 'sec-websocket-key': Buffer.from('0123456789abcdef').toString('base64') } : {};
  return new Promise((done, fail) => {
    const req = http.request({ host: url.hostname, port: url.port, path, headers: { ...headers, ...asks } });
    req.on('response', (res) => {
      const parts = [];
      res.on('data', (d) => parts.push(d));
      res.on('end', () => done({ status: res.statusCode, headers: res.headers, json: JSON.parse(Buffer.concat(parts).toString() || 'null') }));
    });
    req.on('upgrade', (res, socket) => (socket.destroy(), done({ status: res.statusCode, headers: res.headers, json: null })));
    req.on('error', fail);
    req.end();
  });
}
const refused = async (dev, base, upgrade) => refusal(await signed(dev, 'GET', '/v1/dir/watch'), base, upgrade);
/** dialled answers the raw upgrade of a watch that names `query`, so the answer's own headers can be read. */
const dialled = async (dev, query) => refusal(await signed(dev, 'GET', `/v1/dir/watch${query}`), BASE, true, `/v1/dir/watch${query}`);

/** party makes a fresh identity with n devices, each having written its own device record. */
async function party(n = 1) {
  const identity = await newIdentity();
  const devs = [];
  for (let i = 0; i < n; i++) {
    const dev = await newDevice(identity);
    assert.equal((await call(dev, 'PUT', `/v1/dir/devices/${await deviceId(dev)}`, record)).status, 204);
    devs.push(dev);
  }
  return devs;
}

const pub = (fence, head, old_head, pending = 0) => ({ fence, old_head, head, size: 0, class: 'work', pending });
const version = async (dev) => Number((await call(dev, 'GET', '/v1/dir/list')).headers.get('codeaf-dir-version'));

/** bumped asserts the socket hears exactly one frame, at the version after last. */
async function bumped(w, last) {
  assert.equal(await nextVersion(w), last + 1);
  return last + 1;
}

// Cases, each on a fresh identity and against the one relay at BASE (the cap one excepted).
const cases = {
  /** The event frames (contract 5): opt-in, presence with a 15 s offline debounce, revoked; a plain socket hears none of it. */
  async events() {
    const [a, b, c] = await party(3);
    const [ida, idb, idc] = [await deviceId(a), await deviceId(b), await deviceId(c)];
    const event = async (x) => {
      const { at, ...rest } = JSON.parse(await next(x));
      assert.equal(typeof at, 'number');
      return rest;
    };
    const plain = await openWatch(a);
    const wa = await openWatch(a, BASE, '?events=1');
    assert.match(await next(plain), WIRE);
    assert.match(await next(wa), WIRE);
    let wb = await openWatch(b, BASE, '?events=1');
    assert.match(await next(wb), WIRE);
    assert.deepEqual(await event(wb), { t: 'presence', device: ida, online: true }, 'a new event socket hears who is online');
    assert.deepEqual(await event(wa), { t: 'presence', device: idb, online: true });
    wb.close();
    await sleep(1000);
    wb = await openWatch(b, BASE, '?events=1');
    assert.match(await next(wb), WIRE);
    assert.deepEqual(await event(wb), { t: 'presence', device: ida, online: true });
    const gone = Date.now();
    wb.close();
    await sleep(12_000);
    assert.equal(wa.cursor, wa.frames.length, 'a reconnect inside the gap, and a gap not yet over, say nothing');
    assert.deepEqual(await event(wa), { t: 'presence', device: idb, online: false });
    assert.ok(Date.now() - gone >= 14_000, 'offline waits out the 15 s debounce');
    const wc = await openWatch(c, BASE, '?events=1');
    assert.match(await next(wc), WIRE);
    await next(wc);
    assert.deepEqual(await event(wa), { t: 'presence', device: idc, online: true });
    assert.equal((await call(a, 'POST', `/v1/dir/devices/${idc}/revoke`, {})).status, 204);
    assert.match(await next(wa), WIRE);
    assert.deepEqual(await event(wa), { t: 'revoked', device: idc });
    assert.deepEqual((await wc.closed).code, 4401);
    assert.deepEqual(plain.frames.filter((f) => !WIRE.test(f)), [], 'a socket that did not ask for events never hears one');
  },
  /** Presence from the sign of life (contract 21.12): a socket that declared beat=2 and never pings is told offline and closed 4408. */
  async frozen() {
    const [a, b, c] = await party(3);
    const idb = await deviceId(b);
    for (const bad of ['?beat=0', '?beat=61', '?beat=x', '?beat=2&beat=3']) assert.equal((await dialled(b, bad)).status, 400, bad);
    assert.equal((await dialled(c, '?beat=2')).headers['codeaf-presence'], '1', 'the 101 says the relay judges presence by the sign of life');
    const viewer = await openWatch(a, BASE, '?events=1');
    const quietOne = await openWatch(b, BASE, '?beat=2');
    const opened = Date.now();
    await waitFor(viewer, () => viewer.frames.some((f) => f.includes('"online":false')), 12_000);
    assert.ok(Date.now() - opened >= 4_000, 'not before 2.5 beats of silence');
    const told = viewer.frames.filter((f) => f.includes(idb)).map((f) => JSON.parse(f));
    assert.deepEqual(told.map((f) => f.online), [true, false]);
    assert.deepEqual((await quietOne.closed).code, 4408);
    const listed = (await call(a, 'GET', '/v1/dir/presence')).json?.devices?.[idb];
    assert.equal(listed?.online, false, 'the presence answer lists it offline');
    const pinger = await openWatch(b, BASE, '?beat=2');
    await waitFor(viewer, () => viewer.frames.filter((f) => f.includes(idb)).length >= 3, 3_000);
    for (let i = 0; i < 5; i++) (pinger.send('ping'), await sleep(2_000));
    assert.equal((await call(a, 'GET', '/v1/dir/presence')).json?.devices?.[idb]?.online, true, 'a pinging socket stays online past the window');
    viewer.close();
    pinger.close();
  },
  async firstFrame() {
    const [a] = await party();
    const w = await openWatch(a);
    const first = await next(w);
    assert.equal(first, JSON.stringify({ v: await version(a) }), 'the current version, on accept');
    assert.match(first, WIRE);
    const v = JSON.parse(first).v;
    assert.equal((await call(a, 'POST', '/v1/dir/cells/c1', init())).status, 200);
    await bumped(w, v);
    w.close();
    console.log('version frame bytes:', Buffer.byteLength(first), first);
  },

  async fresh() {
    const id = await newIdentity();
    const a = await newDevice(id);
    const w = await openWatch(a);
    assert.equal(await next(w), '{"v":0}', 'a fresh identity starts at zero');
    w.close();
  },

  async twoDevices() {
    const [a, b] = await party(2);
    const [wa, wb] = [await openWatch(a), await openWatch(b)];
    const [va, vb] = [await nextVersion(wa), await nextVersion(wb)];
    assert.equal(va, vb);
    await call(a, 'POST', '/v1/dir/cells/c1', init());
    assert.equal(await nextVersion(wa), va + 1);
    assert.equal(await nextVersion(wb), va + 1, 'both sockets hear the same number');
    wa.close(); wb.close();
  },

  async everyChangeBumpsOnce() {
    const [a] = await party();
    const w = await openWatch(a);
    let v = await nextVersion(w);
    const steps = [
      ['create', () => call(a, 'POST', '/v1/dir/cells/c1', init())],
      ['acquire', () => call(a, 'POST', '/v1/dir/cells/c1/acquire', {})],
      ['publish', () => call(a, 'POST', '/v1/dir/cells/c1/publish', pub(2, 'h1', 'h0'))],
      ['release', () => call(a, 'POST', '/v1/dir/cells/c1/release', { fence: 2 })],
      ['archive', () => call(a, 'POST', '/v1/dir/cells/c1/archive', {})],
      ['device put', async () => call(a, 'PUT', `/v1/dir/devices/${await deviceId(a)}`, { ...record, name: 'renamed' })],
      ['vault', () => call(a, 'POST', '/v1/dir/vault', { old: '', new: 'x' })],
    ];
    for (const [name, step] of steps) {
      assert.ok((await step()).status < 300, name);
      v = await bumped(w, v);
    }
    await quiet(w, 300);
    w.close();
  },

  async silentWrites() {
    const [a] = await party();
    await call(a, 'POST', '/v1/dir/cells/c1', init());
    const w = await openWatch(a);
    let v = await nextVersion(w);
    assert.equal((await call(a, 'POST', '/v1/dir/cells/c1/heartbeat', { fence: 1, pending: 0 })).status, 200);
    await sleep(1500);
    await call(a, 'POST', '/v1/dir/cells/c2', init());
    v = await bumped(w, v); // the next frame is the create, so the heartbeat bumped nothing
    assert.equal((await call(a, 'POST', '/v1/dir/cells/c1/heartbeat', { fence: 1, pending: 3 })).status, 200);
    v = await bumped(w, v); // a changed pending is visible
    assert.equal((await call(a, 'PUT', `/v1/dir/devices/${await deviceId(a)}`, record)).status, 204);
    await call(a, 'POST', '/v1/dir/cells/c3', init());
    await bumped(w, v); // and an identical device record was no frame in between
    w.close();
  },

  async listCarriesVersion() {
    const [a] = await party();
    const w = await openWatch(a);
    await nextVersion(w);
    await call(a, 'POST', '/v1/dir/cells/c1', init());
    const v = JSON.parse(await waitLast(w)).v;
    assert.equal(await version(a), v, 'the header is the last frame when nothing changed since');
    w.close();
  },

  async revokeClosesOnlyThatDevice() {
    const [a, b] = await party(2);
    const [wa, wb] = [await openWatch(a), await openWatch(b)];
    const v = await nextVersion(wa);
    await nextVersion(wb);
    assert.equal((await call(a, 'POST', `/v1/dir/devices/${await deviceId(b)}/revoke`)).status, 204);
    assert.equal(await nextVersion(wa), v + 1, 'the revoke is a bump');
    assert.deepEqual(await wb.closed, { code: 4401, reason: 'revoked' });
    await call(a, 'POST', '/v1/dir/cells/c1', init());
    assert.equal(await nextVersion(wa), v + 2, 'the other socket stays and hears the next change');
    const r = await refused(b);
    assert.deepEqual([r.status, r.json.err], [401, 'revoked']);
    wa.close();
  },

  async upgradeRefusals() {
    const [a] = await party();
    const bare = await refusal({});
    assert.deepEqual([bare.status, bare.json.err], [401, 'unauthorized']);
    const plain = await refused(a, BASE, false);
    assert.deepEqual([plain.status, plain.json], [426, { err: 'upgrade_required' }]);
  },

  async freezeClosesAll() {
    const [a, b] = await party(2);
    const ws = [await openWatch(a), await openWatch(b)];
    const v = await nextVersion(ws[0]);
    await nextVersion(ws[1]);
    assert.equal((await call(a, 'POST', '/v1/identity/rotation', { V: 1, op: 'freeze' })).status, 200);
    for (const w of ws) {
      assert.equal(await nextVersion(w), v + 1, 'each heard the bump first');
      assert.deepEqual(await w.closed, { code: 4410, reason: 'rotated' });
    }
    const r = await refused(a);
    assert.deepEqual([r.status, r.json], [410, { err: 'rotated' }]);
  },

  async pingAndIgnoredFrames() {
    const [a] = await party();
    const w = await openWatch(a);
    const v = await nextVersion(w);
    w.send('ping');
    assert.equal(await next(w), 'pong');
    w.send('hello');
    await quiet(w, 500);
    await call(a, 'POST', '/v1/dir/cells/c1', init());
    assert.equal(await nextVersion(w), v + 1, 'a socket that sent a stray frame still hears changes');
    w.close();
  },

  async holdQuery() {
    const [a] = await party();
    const plain = await dialled(a, '');
    assert.deepEqual([plain.status, plain.headers['codeaf-vouch']], [101, '1'], 'a relay that vouches says so on every upgrade');
    const held = await dialled(a, '?hold=c1:1&hold=nowhere:9&hold=c1:1');
    assert.deepEqual([held.status, held.headers['codeaf-vouch']], [101, '1'], 'holds of cells that do not exist are accepted');
    const sixteen = Array.from({ length: 16 }, (_, i) => `hold=c${i}:1`).join('&');
    assert.equal((await dialled(a, `?${sixteen}`)).status, 101);
    const refusals = ['?hold=c1', '?hold=:1', '?hold=c1:', '?hold=c1:x', '?hold=c1:-1', `?hold=${'c'.repeat(65)}:1`, `?${sixteen}&hold=c16:1`];
    for (const q of refusals) {
      const r = await dialled(a, q);
      assert.deepEqual([r.status, r.json], [400, { err: 'bad_request' }], q);
    }
    const w = await openWatch(a, BASE, '?hold=c1:1');
    assert.match(await next(w), WIRE, 'a socket that names holds still speaks only versions');
    w.close();
  },

  async cap() {
    const [a] = await party();
    const open = [];
    for (let i = 0; i < 5; i++) open.push(await openWatch(a, CAP));
    const sixth = await refused(a, CAP);
    assert.deepEqual([sixth.status, sixth.json], [429, { err: 'too_many_watchers' }]);
    open.pop().close();
    let again;
    for (let i = 0; i < 20 && !again; i++) again = await openWatch(a, CAP).catch(() => sleep(100)); // a close frees its place at once, but arrives asynchronously
    assert.ok(again, 'a closed socket frees a place');
    const [other] = await party();
    const w = await openWatch(other, CAP);
    await next(w);
    for (const s of [...open, again, w]) s.close();
  },
};

/** waitLast answers the newest frame once there is one past the cursor, marking it read. */
async function waitLast(w) {
  await waitFor(w, () => w.frames.length > w.cursor);
  w.cursor = w.frames.length;
  return w.frames.at(-1);
}

// idle: a socket that only pings must never run the object's code; the log says what ran.
async function idle() {
  const log = () => readFileSync(process.env.WATCH_LOG, 'utf8');
  const ran = () => (log().match(/watch: message/g) ?? []).length;
  const [a] = await party();
  const w = await openWatch(a);
  const first = await next(w);
  const before = ran();
  const pings = 40;
  for (let i = 0; i < pings; i++) {
    w.send('ping');
    assert.equal(await next(w, 3000), 'pong', `ping ${i}`);
    await sleep(1000);
  }
  assert.equal(ran() - before, 0, 'no ping reached JavaScript');
  w.send('hello');
  await until(() => ran() > before, 3000, 'the control frame reaches JavaScript and is logged');
  console.log(`idle: ${pings} pings answered, JS ran for 0 of them; a version frame is ${Buffer.byteLength(first)} bytes (${first})`);
  w.close();
}

// restart: the version of a directory outlives the relay process.
const jwk = (key) => crypto.subtle.exportKey('jwk', key);
const load = (j, usage) => crypto.subtle.importKey('jwk', j, { name: 'Ed25519' }, true, [usage]);

async function putState(file) {
  const [a] = await party();
  for (const cell of ['c1', 'c2']) await call(a, 'POST', `/v1/dir/cells/${cell}`, init(), { base: PERSIST });
  const w = await openWatch(a, PERSIST);
  const v = await nextVersion(w);
  assert.ok(v >= 2);
  w.close();
  writeFileSync(file, JSON.stringify({ identity: await jwk(a.identity.pair.privateKey), device: await jwk(a.pair.privateKey), cert: a.cert, v }));
}

async function checkState(file) {
  const s = JSON.parse(readFileSync(file));
  const identity = { pair: { privateKey: await load(s.identity, 'sign') }, pub: Uint8Array.from(Buffer.from(s.identity.x, 'base64url')) };
  const a = { identity, cert: s.cert, pair: { privateKey: await load(s.device, 'sign') } };
  const w = await openWatch(a, PERSIST);
  assert.equal(await nextVersion(w), s.v, 'the first frame after a restart is the version before it');
  w.close();
}

// lease: a socket that names a lease and pings, and nothing else, keeps it live past its stored expiry while the
// object hibernates between pings; once the pings stop the lease lapses TTL later (contract 21.11). Real time, about 200 s.
async function lease() {
  const TTL = 90_000;
  const [a, b] = await party(2);
  await call(a, 'POST', '/v1/dir/cells/c1', init());
  const w = await openWatch(a, BASE, '?hold=c1:1');
  await next(w);
  const began = Date.now();
  while (Date.now() - began < TTL + 15_000) {
    w.send('ping');
    assert.equal(await next(w, 3000), 'pong');
    await sleep(25_000);
  }
  const taken = await call(b, 'POST', '/v1/dir/cells/c1/acquire', {});
  assert.equal(answerOf(taken), 'lease_held', 'past the stored expiry, the pinging socket still holds the lease');
  const cell = (await call(b, 'GET', '/v1/dir/list')).json.cells.c1;
  assert.ok(cell.lease.expires > Date.now() - 1000, 'the list shows it held');
  await sleep(TTL + 2_000); // the pings stop; the socket stays open
  assert.equal(answerOf(await call(b, 'POST', '/v1/dir/cells/c1/acquire', {})), 200, 'TTL after the last ping the lease is free');
  w.close();
}

const [mode, file] = process.argv.slice(2);
if (mode === 'idle') await idle();
else if (mode === 'lease') await lease();
else if (mode === 'put') await putState(file);
else if (mode === 'check') await checkState(file);
else if (mode in cases) (await cases[mode](), console.log('watch', mode, 'ok'));
else for (const [name, run] of Object.entries(cases)) (await run(), console.log('watch', name, 'ok'));
console.log('watch', mode ?? 'cases', 'ok');
process.exit(0); // open sockets of a failed case must not hold the process
