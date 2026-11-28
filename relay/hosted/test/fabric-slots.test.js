// The slots of the Worker's fabric and what the relay knows of a phone that is looking at them: slot versions and the wait
// query (routes.json slots.watch), the 128-slot cap, the claim token, the watching state with its two rules and its
// damping, serving, and the header a slot PUT answers with. Each test names its contract id (design 13).
// The clock is injected everywhere, so no test sleeps; the pieces are the real ones over in-memory SQLite and a socket stand-in.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { ClientWatch, DAMPEN_MS, EVENT_FOR_MS, HEADER_FOR_MS, holdWait } from '../src/clientwatch.js';
import { Fabric } from '../src/fabric.js';
import { matchRoute } from '../src/routes.js';
import { memorySql } from './sql.js';
import { watchRig } from './clientrig.js';

const enc = new TextEncoder();
const bytes = (value) => enc.encode(JSON.stringify(value));
const NONE = new Uint8Array();
const FULL = 'dev_' + 'a'.repeat(32);
const PHONE = 'dev_' + 'b'.repeat(32);
const OTHER = 'dev_' + 'c'.repeat(32);
const query = (text) => new URLSearchParams(text);

function code(fn) {
  try {
    fn();
    return 'ok';
  } catch (e) {
    return e.code ?? e.kind ?? `unexpected: ${e.message}`;
  }
}

function open({ rows } = {}) {
  const sql = memorySql();
  const clock = { now: 1_000_000 };
  const dir = {
    rows: rows ?? {
      [FULL]: { V: 1 },
      [PHONE]: { V: 1, kind: 'client', methods: ['agents.send'] },
      [OTHER]: { V: 1, kind: 'client', methods: ['agents.send'] },
    },
    read: (kind, id) => (kind === 'devices' ? dir.rows[id] : undefined),
    devices: () => dir.rows,
  };
  const fabric = new Fabric(sql, () => clock.now, dir);
  const slot = (name, sealed = 'YQ==', target = PHONE) => fabric.putSlot({ caller: FULL, targetDevice: target, name, body: bytes({ sealed }) });
  const watch = (q, caller = PHONE, target = PHONE) => fabric.watchSlots({ caller, targetDevice: target, query: query(q), body: NONE });
  return { sql, clock, dir, fabric, slot, watch };
}

test('R1, L6: the slot counter rises by one on every PUT, a rewrite included; each slot keeps the version it was written at', () => {
  const { fabric, slot, watch } = open();
  slot('a');
  slot('b');
  slot('a', 'Yg==');
  const read = (name) => fabric.getSlot({ caller: PHONE, targetDevice: PHONE, name, body: NONE });
  assert.equal(read('a').version, 3, 'a was rewritten after b');
  assert.equal(read('b').version, 2);
  assert.equal(read('a').sealed, 'Yg==');
  assert.deepEqual(watch('since=0&wait=0').changes, { version: 3, slots: ['a', 'b'] });
  assert.deepEqual(watch('since=1&wait=0').changes, { version: 3, slots: ['a', 'b'] }, 'a (v3) and b (v2) are both past 1');
  assert.deepEqual(watch('since=2&wait=0').changes, { version: 3, slots: ['a'] }, 'only a is past 2');
  assert.equal(watch('since=3&wait=0').changes, undefined, 'nothing is newer than the client version');
  assert.deepEqual(watch('since=3&wait=7'), { since: 3n, seconds: 7 }, 'nothing newer: the caller may park for the wait it asked');
  assert.deepEqual(watch('since=3&wait=0'), { since: 3n, seconds: 0 }, 'wait 0 never parks');
  assert.deepEqual(watch('since=99&wait=0').changes, undefined, 'a since past the version is simply nothing newer');
});

test('R1: the version is per client, and slot names come back sorted', () => {
  const { slot, watch } = open();
  slot('zeta', 'YQ==', PHONE);
  slot('alpha', 'YQ==', PHONE);
  slot('card', 'YQ==', OTHER);
  assert.deepEqual(watch('since=0&wait=0').changes, { version: 2, slots: ['alpha', 'zeta'] });
  assert.deepEqual(watch('since=0&wait=0', OTHER, OTHER).changes, { version: 1, slots: ['card'] }, 'the other phone has its own counter');
});

test('R1: since is a decimal uint64 and wait a number of seconds in 0 to 25; anything else, or a body, is 400', () => {
  const { slot, watch, fabric } = open();
  slot('a');
  for (const bad of [
    'since=-1&wait=0', 'since=x&wait=0', 'since=&wait=0', 'since=1.5&wait=0', 'since=0x10&wait=0', 'since=18446744073709551616&wait=0',
    'since=0&wait=26', 'since=0&wait=-1', 'since=0&wait=NaN', 'since=0&wait=', 'since=0&wait=inf', 'since=0&wait=1e3', 'wait=0', 'since=0',
  ]) {
    assert.equal(code(() => watch(bad)), 'bad_request', bad);
  }
  for (const good of ['since=0&wait=0', 'since=0&wait=0.01', 'since=0&wait=25', 'since=0&wait=.5', 'since=18446744073709551615&wait=0', 'since=0&wait=1e1']) {
    assert.equal(code(() => watch(good)), 'ok', good);
  }
  assert.equal(code(() => fabric.watchSlots({ caller: PHONE, targetDevice: PHONE, query: query('since=0&wait=0'), body: enc.encode('{}') })), 'bad_request', 'a body');
});

test('R1: the wait asks authority before it answers: a computer, another phone, or a stranger is refused, in the order 401 then 404', () => {
  const { watch, dir } = open();
  assert.equal(code(() => watch('since=0&wait=0', FULL, PHONE)), 'unauthorized');
  assert.equal(code(() => watch('since=0&wait=0', OTHER, PHONE)), 'unauthorized');
  dir.rows[PHONE] = { ...dir.rows[PHONE], revoked: true };
  assert.equal(code(() => watch('since=0&wait=0', PHONE, PHONE)), 'not_found', 'a revoked client has no slots to watch');
  const gone = 'dev_' + 'e'.repeat(32);
  assert.equal(code(() => watch('since=0&wait=0', gone, gone)), 'unauthorized', 'a caller that is no client at all');
});

test('R1: 128 slots per client; the 129th new name is 429 full, a rewrite at the cap is fine, and 64 is no longer the cap', () => {
  const { slot, sql } = open();
  for (let i = 0; i < 128; i++) slot(`s${i}`);
  assert.equal(sql.exec('SELECT COUNT(*) AS n FROM fabric_slots').one().n, 128);
  const err = (() => {
    try {
      slot('one-too-many');
    } catch (e) {
      return e;
    }
  })();
  assert.equal(err.code, 'full');
  assert.equal(err.status, 429);
  assert.equal(code(() => slot('s0', 'Yg==')), 'ok', 'a rewrite of an existing name at the cap');
  assert.equal(code(() => slot('x', 'YQ==', OTHER)), 'ok', 'the cap is per client');
});

test('R1: a slot table made before versions existed gains the column; its rows read as version 0 and new PUTs count from 1', () => {
  const sql = memorySql();
  sql.exec('CREATE TABLE fabric_slots (device TEXT NOT NULL, name TEXT NOT NULL, sealed TEXT NOT NULL, writer TEXT NOT NULL, at INTEGER NOT NULL, PRIMARY KEY (device, name)) WITHOUT ROWID');
  sql.exec('INSERT INTO fabric_slots VALUES (?,?,?,?,?)', PHONE, 'old', 'YQ==', FULL, 999_999);
  const dir = { read: (kind, id) => ({ [FULL]: { V: 1 }, [PHONE]: { V: 1, kind: 'client' } })[id], devices: () => ({}) };
  const fabric = new Fabric(sql, () => 1_000_000, dir);
  assert.equal(fabric.getSlot({ caller: PHONE, targetDevice: PHONE, name: 'old', body: NONE }).version, 0);
  fabric.putSlot({ caller: FULL, targetDevice: PHONE, name: 'new', body: bytes({ sealed: 'YQ==' }) });
  const again = new Fabric(sql, () => 1_000_000, dir); // a second start over the migrated table must not fail
  const got = again.watchSlots({ caller: PHONE, targetDevice: PHONE, query: query('since=0&wait=0'), body: NONE });
  assert.deepEqual(got.changes, { version: 1, slots: ['new'] });
});

test('R1, V4: a revoke deletes the client\'s slots and version counter, so a later PUT never inherits an old version', () => {
  const { slot, fabric, sql, dir } = open();
  slot('a');
  slot('b');
  fabric.revokeClient(PHONE);
  assert.equal(sql.exec('SELECT COUNT(*) AS n FROM fabric_slots').one().n, 0);
  assert.equal(sql.exec('SELECT COUNT(*) AS n FROM fabric_versions').one().n, 0);
  dir.rows[PHONE] = { V: 1, kind: 'client' };
  slot('a');
  assert.equal(fabric.getSlot({ caller: PHONE, targetDevice: PHONE, name: 'a', body: NONE }).version, 1);
});

// The old test of phone/live (fabric-claims) ported to the Raft-hosted Fabric.
test('R1: only the persisted process token recovers a claim after a lost response; the token never appears in a listing', () => {
  const sql = memorySql();
  const now = () => 1_000_000;
  const dir = { read: (kind, id) => ({ [FULL]: { V: 1 }, [PHONE]: { V: 1, kind: 'client', methods: ['agents.create'] } })[id] };
  const fabric = new Fabric(sql, now, dir);
  const entry = bytes({ number: 1, at: now(), method: 'agents.create', target: 'any', sealed: 'YQ==' });
  fabric.appendQueue({ caller: PHONE, targetDevice: PHONE, body: entry, proof: { signature: 'sig', cert: 'cert', identity: 'identity', requestTime: 'time', requestUri: '/queue', bodyBytes: entry } });
  const request = { caller: FULL, targetDevice: PHONE, numberText: '1', ack: false };
  const token = bytes({ token: 'c'.repeat(64) });
  fabric.claimQueue({ ...request, body: token });
  const reopened = new Fabric(sql, now, dir);
  assert.equal(code(() => reopened.claimQueue({ ...request, body: bytes({ token: 'd'.repeat(64) }) })), 'claimed', 'another token');
  assert.equal(code(() => reopened.claimQueue({ ...request, body: NONE })), 'claimed', 'no token, even from the same caller');
  const queue = reopened.listQueue({ caller: FULL, targetDevice: PHONE, body: NONE });
  assert.equal(queue.length, 1, 'the owner still sees it');
  assert.equal(JSON.stringify(queue).includes('c'.repeat(64)), false, 'and not the token');
  reopened.claimQueue({ ...request, body: token });
  reopened.claimQueue({ ...request, ack: true, body: NONE });
  assert.deepEqual(reopened.listQueue({ caller: FULL, targetDevice: PHONE, body: NONE }), []);
});

test('R1: a claim token is 64 lowercase hex or 400, an ack takes no body, and an ack by a non-owner is 409 claimed', () => {
  const sql = memorySql();
  const now = () => 1_000_000;
  const second = 'dev_' + 'f'.repeat(32);
  const dir = { read: (kind, id) => ({ [FULL]: { V: 1 }, [second]: { V: 1 }, [PHONE]: { V: 1, kind: 'client', methods: ['agents.create'] } })[id] };
  const fabric = new Fabric(sql, now, dir);
  const entry = bytes({ number: 1, at: now(), method: 'agents.create', target: 'any', sealed: 'YQ==' });
  fabric.appendQueue({ caller: PHONE, targetDevice: PHONE, body: entry, proof: { signature: 's', cert: 'c', identity: 'i', requestTime: 't', requestUri: '/q', bodyBytes: entry } });
  const claim = (caller, body, ack = false) => fabric.claimQueue({ caller, targetDevice: PHONE, numberText: '1', body, ack });
  for (const bad of ['c'.repeat(63), 'C'.repeat(64), 'g'.repeat(64), 7]) assert.equal(code(() => claim(FULL, bytes({ token: bad }))), 'bad_request', String(bad));
  assert.equal(code(() => claim(FULL, bytes({ token: 'a'.repeat(64), extra: 1 }))), 'bad_request');
  assert.equal(code(() => claim(FULL, bytes({ token: 'a'.repeat(64) }), true)), 'bad_request', 'an ack takes no body');
  assert.equal(code(() => claim(FULL, NONE, true)), 'claimed', 'an ack of an entry nobody claimed');
  claim(FULL, NONE);
  assert.equal(code(() => claim(second, bytes({ token: 'a'.repeat(64) }))), 'claimed', 'another caller');
  assert.equal(code(() => claim(second, NONE, true)), 'claimed', 'an ack by another caller');
  assert.equal(code(() => claim(FULL, NONE, true)), 'ok', 'the owner acks, a tokenless claim included');
});

// ---------- the watching state (ClientWatch), on a clock held by hand ----------

function watching({ listening = true, kv = new Map() } = {}) {
  const clock = { now: 10_000_000 };
  const told = [];
  const armed = [];
  const stand = {
    get: (k) => kv.get(k),
    put: (k, v) => kv.set(k, v),
    delete: (k) => kv.delete(k),
    list: ({ prefix }) => [...kv].filter(([k]) => k.startsWith(prefix)).sort(),
  };
  const make = (state = { listening }) =>
    new ClientWatch({ clock: () => clock.now, kv: () => stand, tell: (f) => told.push(`${f.t} ${f.device} ${f.on}@${clock.now}`), arm: (at) => armed.push(at), listening: () => state.listening });
  const state = { listening };
  return { clock, told, armed, kv, make: () => make(state), state, clients: make(state) };
}

test('L4: the header rule: watching while a wait is parked, and for 30 s after the last wait ended or slot was read', () => {
  const { clients, clock } = watching();
  assert.equal(clients.headerOn(PHONE), false, 'a phone nobody has heard of is not watching');
  clients.touch(PHONE);
  assert.equal(clients.headerOn(PHONE), true);
  clock.now += HEADER_FOR_MS - 1;
  assert.equal(clients.headerOn(PHONE), true, '29.999 s after the read');
  clock.now += 1;
  assert.equal(clients.headerOn(PHONE), false, 'exactly 30 s after');
  const wait = clients.park(PHONE, { perClient: 2, perIdentity: 32 });
  clock.now += 10 * HEADER_FOR_MS;
  assert.equal(clients.headerOn(PHONE), true, 'a parked wait is watching however long it has been parked');
  clients.release(wait);
  clock.now += HEADER_FOR_MS - 1;
  assert.equal(clients.headerOn(PHONE), true, 'the last wait ended less than 30 s ago');
  clock.now += 1;
  assert.equal(clients.headerOn(PHONE), false);
  assert.equal(clients.headerOn(OTHER), false, 'one phone is not another');
});

test('L4, L7: the event rule: on at the first look, off 45 s after the last, one frame each, by one alarm armed for the off edge', () => {
  const { clients, clock, told, armed, kv } = watching();
  clients.touch(PHONE);
  assert.deepEqual(told, [`watching ${PHONE} true@10000000`]);
  assert.deepEqual(armed, [10_000_000 + EVENT_FOR_MS], 'the alarm is for the off edge');
  assert.equal(kv.get(`watching:${PHONE}`), 10_000_000, 'the last look is stored so an evicted object still knows to say off');
  clock.now += 10_000;
  clients.touch(PHONE);
  assert.equal(told.length, 1, 'a second look while on sends nothing');
  clock.now = 10_000_000 + EVENT_FOR_MS - 1;
  clients.expire();
  assert.equal(told.length, 1, 'one tick before the 45 s that the last look started at 10 s: still on');
  clock.now = 10_010_000 + EVENT_FOR_MS - 1;
  clients.expire();
  assert.equal(told.length, 1, '44.999 s after the last look: still on');
  assert.equal(armed.at(-1), 10_010_000 + EVENT_FOR_MS, 'the alarm that fired early set itself for the real edge');
  clock.now = 10_010_000 + EVENT_FOR_MS;
  clients.expire();
  assert.deepEqual(told.at(-1), `watching ${PHONE} false@${clock.now}`);
  assert.equal(told.length, 2);
  assert.equal(kv.has(`watching:${PHONE}`), false, 'the stored record goes with the off frame');
  armed.length = 0;
  clients.expire();
  assert.deepEqual(armed, [], 'a quiet identity arms nothing');
});

test('L4: a parked wait keeps the phone on without any alarm, and its end starts the 45 s', () => {
  const { clients, clock, told, armed } = watching();
  const wait = clients.park(PHONE, { perClient: 2, perIdentity: 32 });
  assert.deepEqual(told, [`watching ${PHONE} true@10000000`]);
  armed.length = 0;
  clock.now += 100_000;
  clients.expire();
  assert.deepEqual([told.length, armed.length], [1, 0], 'still on, nothing to wake for while the wait is held');
  clients.release(wait);
  assert.deepEqual(armed, [clock.now + EVENT_FOR_MS]);
  clock.now += EVENT_FOR_MS;
  clients.expire();
  assert.equal(told.at(-1).endsWith('false@' + clock.now), true);
});

test('L7: at most one frame per phone every 2 s, and the latest state wins when the held frame is due', () => {
  const { clients, clock, told, armed } = watching();
  clients.touch(PHONE); // on at t0
  clock.now += EVENT_FOR_MS;
  clients.expire(); // off at t1
  const t1 = clock.now;
  assert.equal(told.length, 2);
  clock.now = t1 + 500;
  clients.touch(PHONE); // wants on, but a frame went 500 ms ago
  assert.equal(told.length, 2, 'held');
  assert.equal(armed.at(-1), t1 + DAMPEN_MS, 'the held frame is armed for the moment the 2 s have passed');
  clock.now = t1 + DAMPEN_MS - 1;
  clients.expire();
  assert.equal(told.length, 2, 'not yet');
  clock.now = t1 + DAMPEN_MS;
  clients.expire();
  assert.equal(told.length, 3);
  assert.equal(told.at(-1), `watching ${PHONE} true@${t1 + DAMPEN_MS}`);

  // A flip that is undone inside the 2 s sends nothing at all: on, then revoked before the held frame is due.
  const second = watching();
  second.clients.touch(OTHER);
  second.clock.now += EVENT_FOR_MS;
  second.clients.expire();
  const off = second.clock.now;
  second.clock.now = off + 100;
  second.clients.touch(OTHER);
  second.clients.forget(OTHER);
  second.clock.now = off + DAMPEN_MS;
  second.clients.expire();
  assert.equal(second.told.length, 2, 'on then off, then the held on is withdrawn: nothing new was said');
});

test('L7: a new event socket hears one on frame for every phone watching now and none for a phone that stopped', () => {
  const { clients, clock } = watching();
  clients.touch('dev_' + '2'.repeat(32));
  clients.touch('dev_' + '1'.repeat(32));
  clock.now += EVENT_FOR_MS - 1;
  clients.touch('dev_' + '3'.repeat(32));
  clock.now += 1; // the first two stop being watching exactly now
  const frames = clients.snapshot().map((t) => JSON.parse(t));
  assert.deepEqual(frames.map((f) => f.device), ['dev_' + '3'.repeat(32)]);
  assert.deepEqual(Object.keys(frames[0]), ['t', 'device', 'on', 'at'], 'a frame carries nothing else');
  assert.equal(frames[0].on, true);
  assert.equal(frames[0].at, clock.now);
  assert.ok(JSON.stringify(frames[0]).length <= 512);
});

test('R1: waits are capped at 2 per phone and slotWaitMax per identity (429 full past either), and release makes room', () => {
  const { clients } = watching();
  const a = clients.park(PHONE, { perClient: 2, perIdentity: 3 });
  clients.park(PHONE, { perClient: 2, perIdentity: 3 });
  const third = (() => {
    try {
      clients.park(PHONE, { perClient: 2, perIdentity: 3 });
    } catch (e) {
      return e;
    }
  })();
  assert.equal(third.code, 'full');
  assert.equal(third.status, 429);
  clients.park(OTHER, { perClient: 2, perIdentity: 3 });
  assert.equal(code(() => clients.park(OTHER, { perClient: 2, perIdentity: 3 })), 'full', 'the identity already holds 3');
  clients.release(a);
  assert.equal(code(() => clients.park(OTHER, { perClient: 2, perIdentity: 3 })), 'ok', 'a released wait frees its place');
  clients.release(a);
  assert.equal(code(() => clients.park('dev_' + '4'.repeat(32), { perClient: 2, perIdentity: 3 })), 'full', 'releasing twice does not free two places');
});

test('R1, L7: a slot written for a phone wakes the waits parked for that phone and no other', async () => {
  const { clients } = watching();
  const mine = clients.park(PHONE, { perClient: 2, perIdentity: 32 });
  const theirs = clients.park(OTHER, { perClient: 2, perIdentity: 32 });
  let woke = '';
  mine.woken.then(() => (woke += 'mine'));
  theirs.woken.then(() => (woke += 'theirs'));
  clients.wake(PHONE);
  await new Promise((r) => setImmediate(r));
  assert.equal(woke, 'mine');
});

test('L7: the last look outlives the object: a new instance over the same storage replays it and says off when it is due', () => {
  const first = watching();
  first.clients.touch(PHONE);
  first.clock.now += 20_000;
  const reborn = first.make(); // the object was evicted and woke again: no memory, the same key-value storage
  assert.equal(reborn.headerOn(PHONE), true, 'inside the header window');
  assert.deepEqual(reborn.snapshot().map((t) => JSON.parse(t).device), [PHONE], 'and still announced to a new socket');
  first.clock.now += EVENT_FOR_MS - 20_000;
  reborn.expire();
  assert.equal(first.told.at(-1), `watching ${PHONE} false@${first.clock.now}`, 'the evicted object\'s off edge is not lost');
  assert.equal(first.kv.size, 0);
});

test('L4: with no event socket open nothing is armed for a watching phone, and a first socket arms the off edge', () => {
  const quiet = watching({ listening: false });
  quiet.clients.touch(PHONE);
  assert.deepEqual(quiet.armed, [], 'nobody to tell, nothing to wake for');
  assert.equal(quiet.clients.headerOn(PHONE), true, 'the header rule is read from the state, with or without a listener');
  quiet.state.listening = true;
  quiet.clients.rearm();
  assert.deepEqual(quiet.armed, [10_000_000 + EVENT_FOR_MS]);
});

test('V1: a revoke ends the watching at once and a late release cannot bring it back', () => {
  const { clients, told, clock } = watching();
  const wait = clients.park(PHONE, { perClient: 2, perIdentity: 32 });
  clock.now += DAMPEN_MS;
  clients.forget(PHONE);
  assert.equal(told.at(-1), `watching ${PHONE} false@${clock.now}`);
  assert.equal(clients.headerOn(PHONE), false);
  clients.release(wait);
  assert.equal(clients.headerOn(PHONE), false, 'the parked wait that ends after the revoke does not count as a look');
  assert.deepEqual(clients.snapshot(), []);
  clients.forget(OTHER); // a phone nobody watched
});

test('R1: holdWait answers woken, timeout or aborted, leaves no timer behind, and a revoke rejects it', async () => {
  const timers = new Set();
  const timer = (ms, fn) => {
    const t = { fn, ms };
    timers.add(t);
    return () => timers.delete(t);
  };
  const never = new Promise(() => {});
  const hold = (over) => holdWait({ woken: never, stopped: never, seconds: 25, timer, ...over });

  let first = hold({});
  assert.deepEqual([...timers].map((t) => t.ms), [25_000], 'a wait is a timer of the seconds asked');
  [...timers][0].fn();
  assert.equal(await first, 'timeout');
  assert.equal(timers.size, 0);

  first = hold({ woken: Promise.resolve() });
  assert.equal(await first, 'woken');
  assert.equal(timers.size, 0, 'a wake cancels the timer');

  const listeners = [];
  const signal = { aborted: false, addEventListener: (_, fn) => listeners.push(fn), removeEventListener: (_, fn) => listeners.splice(listeners.indexOf(fn), 1) };
  first = hold({ signal });
  assert.equal(listeners.length, 1);
  listeners[0]();
  assert.equal(await first, 'aborted');
  assert.equal(listeners.length + timers.size, 0, 'no listener and no timer left');
  assert.equal(await hold({ signal: { ...signal, aborted: true } }), 'aborted', 'a request already given up parks nothing');

  const revoked = Object.assign(new Error('revoked'), { code: 'revoked', status: 401 });
  const stopped = Promise.reject(revoked);
  stopped.catch(() => {});
  await assert.rejects(hold({ stopped }), (e) => e.code === 'revoked' && e.status === 401);
  assert.equal(timers.size, 0);
});

// ---------- the sockets: serving, and the watching frames through the real Watchers ----------

test('L8: a socket opened with serving marks its device serving while it is live, and only that socket does', () => {
  const rig = watchRig();
  try {
    rig.dial('dev_runner', 10, { serving: true });
    rig.dial('dev_idle', 10);
    rig.dial('dev_both', 10);
    rig.dial('dev_both', 10, { serving: true });
    assert.deepEqual([...rig.watchers.serving()].sort(), ['dev_both', 'dev_runner'], 'a device with one serving socket among several is serving');
    assert.deepEqual([...rig.watchers.online()].sort(), ['dev_both', 'dev_idle', 'dev_runner'], 'serving adds to online, never replaces it');
    rig.clock.now += 25_001; // the window of beat 10 passes in silence
    assert.deepEqual([...rig.watchers.serving()], [], 'a runner that misses its beats is no longer serving, with no write');
    const pinged = rig.p.open[0];
    pinged.pinged = rig.clock.now;
    assert.deepEqual([...rig.watchers.serving()], ['dev_runner'], 'a ping inside the window keeps it');
    pinged.gone = true;
    assert.deepEqual([...rig.watchers.serving()], [], 'a closed socket leaves getWebSockets and the mark with it');
  } finally {
    rig.restore();
  }
});

test('L8: only the exact value serving=1 marks; true, 0, 2, an empty value or none do not (the Go relay and the Worker parse alike)', () => {
  const marked = (search) => {
    const route = matchRoute('GET', '/v1/dir/watch');
    const request = new Request('https://relay.test/v1/dir/watch' + search, { headers: { upgrade: 'websocket' } });
    return route.handler({ request, device: FULL, tenant: { limits: { maxHolds: 16 }, watch: (_d, opts) => opts } }, route.args).serving;
  };
  assert.equal(marked('?events=1&beat=10&serving=1'), true);
  for (const other of ['?serving=true', '?serving=0', '?serving=2', '?serving=', '?serving=01', '?events=1&beat=10', '']) assert.equal(marked(other), false, other);
});

test('L4, L7: a phone that starts to watch is announced to event sockets, replayed to a socket that connects later, and told off 45 s after', () => {
  const rig = watchRig();
  try {
    const listener = rig.dial('dev_computer', 10, { events: true });
    const bystander = rig.dial('dev_other', 10);
    rig.clients.touch(PHONE);
    assert.deepEqual(rig.watching(listener), [{ t: 'watching', device: PHONE, on: true, at: rig.clock.now }]);
    assert.deepEqual(rig.frames(bystander), [], 'a socket that did not ask for events hears none');
    const t0 = rig.clock.now;
    assert.ok(rig.armed.includes(t0 + EVENT_FOR_MS), 'the off edge is armed on the object\'s one alarm');

    rig.clock.now = t0 + 10_000;
    const late = rig.dial('dev_late', 10, { events: true });
    assert.deepEqual(rig.watching(late), [{ t: 'watching', device: PHONE, on: true, at: rig.clock.now }], 'replayed at connect, like presence');

    rig.fire(t0 + EVENT_FOR_MS - 1);
    assert.equal(rig.watching(listener).length, 1);
    rig.fire(t0 + EVENT_FOR_MS);
    assert.deepEqual(rig.watching(listener).at(-1), { t: 'watching', device: PHONE, on: false, at: t0 + EVENT_FOR_MS });
    assert.deepEqual(rig.watching(late).at(-1), { t: 'watching', device: PHONE, on: false, at: t0 + EVENT_FOR_MS });
    const quiet = rig.dial('dev_after', 10, { events: true });
    assert.deepEqual(rig.watching(quiet), [], 'nothing to replay once it stopped');
  } finally {
    rig.restore();
  }
});

test('L4: the off edge survives the sweep that clears the alarm, and an identity nobody watches arms only presence', () => {
  const rig = watchRig();
  try {
    rig.dial('dev_computer', 10, { events: true });
    rig.armed.length = 0;
    rig.watchers.expire();
    assert.ok(rig.armed.every((at) => at - rig.clock.now <= 25_000), 'with no phone watching only the presence sweep is armed');
    rig.clients.touch(PHONE);
    const t0 = rig.clock.now;
    // sleepMaintenance deletes the alarm and then asks the liveness to set its own again, which must bring the phone's back.
    rig.armed.length = 0;
    rig.watchers.liveness.rearm();
    assert.ok(rig.armed.includes(t0 + EVENT_FOR_MS));
  } finally {
    rig.restore();
  }
});

// ---------- the routes: the header of a slot PUT and what a slot GET counts as ----------

function routes(fabric, clients) {
  const tenant = { fabric, watchers: { clients, online: () => new Set(), serving: () => new Set() } };
  const call = (method, path, { caller, body = NONE, search = '' } = {}) => {
    const route = matchRoute(method, path);
    const request = new Request('https://relay.test' + path + search, { method });
    return route.handler({ tenant, device: caller, body, request }, route.args);
  };
  return call;
}

test('L4: the 204 of a slot PUT carries Codeaf-Client-Watching: 1 exactly while the header rule holds, and never another value', () => {
  const { fabric } = open();
  const { clients, clock } = watching();
  const call = routes(fabric, clients);
  const put = () => call('PUT', `/v1/clients/${PHONE}/slots/card`, { caller: FULL, body: bytes({ sealed: 'YQ==' }) });
  const header = (reply) => reply.headers.get('codeaf-client-watching');
  const first = put();
  assert.equal(first.status, 204);
  assert.equal(header(first), null, 'no one is looking');
  call('GET', `/v1/clients/${PHONE}/slots/card`, { caller: PHONE });
  assert.equal(header(put()), '1', 'the phone read a slot');
  clock.now += HEADER_FOR_MS - 1;
  assert.equal(header(put()), '1');
  clock.now += 1;
  assert.equal(header(put()), null, '30 s after the read');
  const wait = clients.park(PHONE, { perClient: 2, perIdentity: 32 });
  assert.equal(header(put()), '1', 'a parked wait');
  clients.release(wait);
  clock.now += HEADER_FOR_MS;
  assert.equal(header(put()), null);
  assert.equal(header(call('PUT', `/v1/clients/${OTHER}/slots/card`, { caller: FULL, body: bytes({ sealed: 'YQ==' }) })), null, 'another phone was not looked at');
});

test('L4: a read of a slot that is not there still counts as the phone looking; a refused caller does not', () => {
  const { fabric } = open();
  const { clients } = watching();
  const call = routes(fabric, clients);
  assert.throws(() => call('GET', `/v1/clients/${PHONE}/slots/missing`, { caller: PHONE }), { code: 'not_found' });
  assert.equal(clients.headerOn(PHONE), true);
  assert.throws(() => call('GET', `/v1/clients/${OTHER}/slots/missing`, { caller: FULL }), { kind: 'unauthorized' });
  assert.equal(clients.headerOn(OTHER), false, 'a computer asking for another phone\'s slot is not that phone looking');
});

test('R1, L7: the wait route answers at once with what is newer, 204 for wait 0, and says to park only when asked to wait', () => {
  const { fabric, slot } = open();
  const { clients } = watching();
  const watch = (search, reentered = false) => {
    const route = matchRoute('GET', `/v1/clients/${PHONE}/slots`);
    assert.equal(route.wait, true, 'the route is marked as the one a host holds open');
    const request = new Request(`https://relay.test/v1/clients/${PHONE}/slots${search}`);
    return route.handler({ tenant: { fabric, watchers: { clients } }, device: PHONE, body: NONE, request, reentered }, route.args);
  };
  assert.equal(watch('?since=0&wait=0').status, 204);
  assert.deepEqual(watch('?since=0&wait=25'), { park: { device: PHONE, since: 0n, seconds: 25 } });
  slot('card');
  return watch('?since=0&wait=25').json().then((body) => {
    assert.deepEqual(body, { version: 1, slots: ['card'] });
    assert.equal(watch('?since=1&wait=25', true).status, 204, 're-entered after the wait: whatever is not newer is a 204');
  });
});

test('R1: GET /v1/clients has no device in its path and does not take over the slot or register routes', () => {
  assert.equal(matchRoute('GET', '/v1/clients').fabric, true);
  assert.deepEqual(matchRoute('GET', '/v1/clients').args, []);
  assert.equal(matchRoute('GET', `/v1/clients/${PHONE}/slots`).wait, true);
  assert.equal(matchRoute('GET', `/v1/clients/${PHONE}/slots/card`).wait, false, 'a named slot is a read, not the wait');
  assert.equal(matchRoute('GET', `/v1/clients/${PHONE}/computers`).listing, true);
  assert.equal(matchRoute('PUT', `/v1/clients/${PHONE}/enroll`).write, true);
  assert.equal(matchRoute('PUT', `/v1/clients/${PHONE}`).args[0], PHONE);
  assert.throws(() => matchRoute('GET', `/v1/clients/${PHONE}`), { code: 'not_found' });
  assert.throws(() => matchRoute('DELETE', '/v1/clients'), { code: 'not_found' });
});

test('R1, L8: computers lists each live full device of the identity with serving and at, and nothing else, to its own phone only', () => {
  const full2 = 'dev_' + '1'.repeat(32);
  const revoked = 'dev_' + '2'.repeat(32);
  const { fabric, dir } = open();
  dir.rows[full2] = { V: 1, last_seen: 777 };
  dir.rows[FULL] = { V: 1, last_seen: 555, name: 'laptop', caps: { os: 'linux' } };
  dir.rows[revoked] = { V: 1, revoked: true };
  const list = (caller = PHONE, target = PHONE, online = new Set([FULL]), serving = new Set([FULL])) =>
    fabric.computers({ caller, targetDevice: target, body: NONE, online, serving });
  assert.deepEqual(list(), {
    now: 1_000_000,
    computers: [
      { device: full2, serving: false, at: 777 },
      { device: FULL, serving: true, at: 1_000_000 },
    ],
  });
  assert.deepEqual(list(PHONE, PHONE, new Set([FULL]), new Set()).computers[1], { device: FULL, serving: false, at: 1_000_000 }, 'online but no runner: not serving');
  assert.equal(code(() => list(FULL, PHONE)), 'unauthorized');
  assert.equal(code(() => list(OTHER, PHONE)), 'unauthorized');
  dir.rows[PHONE] = { ...dir.rows[PHONE], revoked: true };
  assert.equal(code(() => list()), 'not_found');
});
