// Lease liveness from watch sockets (contract 21.11) without workerd: the directory over an in-memory
// SQLite, the sockets as a stand-in that answers what the platform would, the clock by hand.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Directory } from '../src/directory.js';
import { AMENDED } from '../src/rules.js';
import { lifted, parseHolds, vouchedUntil } from '../src/vouch.js';
import { Watchers } from '../src/watch.js';
import { memorySql } from './sql.js';
import { init } from './helpers.js';

const TTL = AMENDED.ttlMs;
globalThis.WebSocketRequestResponsePair ??= class {};

/** Sockets stands where the platform's sockets would: each has an attachment and a last auto-response time. */
function sockets() {
  const open = [];
  const ctx = {
    setWebSocketAutoResponse() {},
    getWebSockets: (tag) => open.filter((w) => tag === undefined || w.tag === tag),
    getWebSocketAutoResponseTimestamp: (w) => (w.pinged ? new Date(w.pinged) : null),
  };
  const add = (tag, holds, at) => {
    const w = { tag, pinged: null, deserializeAttachment: () => ({ at, holds }) };
    open.push(w);
    return w;
  };
  return { ctx, add, drop: (w) => open.splice(open.indexOf(w), 1) };
}

function rig() {
  const clock = { now: 1_000_000 };
  const socks = sockets();
  const watchers = new Watchers(socks.ctx, 1000, TTL);
  const heard = [];
  const tell = { ...watchers, vouchedUntil: (d, c, f) => watchers.vouchedUntil(d, c, f), broadcast: (v) => heard.push(v), closeDevice() {}, closeAll() {} };
  const dir = new Directory(memorySql(), 'id_t', () => clock.now, AMENDED, tell);
  return { clock, socks, dir, heard };
}
const code = (fn) => {
  try {
    fn();
    return 'ok';
  } catch (e) {
    return e.code;
  }
};

test('SocketKeepsLeaseLive: a pinging socket keeps the lease live past its stored expiry, in acquire and in the list', () => {
  const { clock, socks, dir } = rig();
  dir.create('c1', init(), 'dev_a');
  const w = socks.add('dev_a', [['c1', 1]], clock.now);
  clock.now += TTL - 1_000;
  w.pinged = clock.now;
  clock.now += TTL - 1_000; // the stored expiry is long past; the last ping is 89 s old
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'lease_held');
  const listed = dir.list().cells.c1.lease;
  assert.ok(listed.expires > clock.now, 'the list shows the lease held');
  assert.equal(dir.cell('c1').cell.lease.expires, listed.expires, 'list and cell read the same lease');
});

test('SilentSocketLapses: a socket that never pings counts from its accept time', () => {
  const { clock, socks, dir } = rig();
  dir.create('c1', init(), 'dev_a');
  clock.now += TTL - 5_000; // the stored expiry is 5 s away
  socks.add('dev_a', [['c1', 1]], clock.now);
  clock.now += 10_000; // past the stored expiry, inside accept + TTL
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'lease_held');
  clock.now += TTL; // accept + TTL has passed
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'ok');
});

test('DeadSocketLapses: the lease lapses TTL after the last ping, and not before', () => {
  const { clock, socks, dir } = rig();
  dir.create('c1', init(), 'dev_a');
  const w = socks.add('dev_a', [['c1', 1]], clock.now);
  clock.now += 600_000;
  w.pinged = clock.now; // the last sign of life
  clock.now += TTL - 1;
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'lease_held');
  clock.now += 2;
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'ok');
});

test('TakeoverSilencesOldSocket: a forced acquire raises the fence and the old socket vouches for nothing', () => {
  const { clock, socks, dir } = rig();
  dir.create('c1', init(), 'dev_a');
  const old = socks.add('dev_a', [['c1', 1]], clock.now);
  dir.acquire('c1', 'dev_b', true);
  clock.now += TTL + 1_000;
  old.pinged = clock.now; // still pinging
  assert.equal(dir.cell('c1').cell.lease.fence, 2);
  assert.ok(dir.cell('c1').cell.lease.expires <= clock.now, 'the new lease lapses on its own clock');
  assert.equal(code(() => dir.acquire('c1', 'dev_c')), 'ok');
});

test('HoldOfUnheldCellVouchesNothing, HoldAtStaleFenceVouchesNothing, OtherDevicesSocketVouchesNothing', () => {
  const { clock, socks, dir } = rig();
  dir.create('c1', init(), 'dev_a');
  socks.add('dev_b', [['c1', 1], ['nope', 1]], clock.now); // another device names a cell dev_a holds, and one that does not exist
  socks.add('dev_a', [['c1', 7]], clock.now); // the holder at a fence it does not hold
  clock.now += TTL + 1;
  assert.equal(dir.cell('c1').cell.lease.expires <= clock.now, true);
  assert.equal(code(() => dir.acquire('c1', 'dev_c')), 'ok');
});

test('ReleaseEndsVouching: after a release the socket that named the lease no longer keeps it', () => {
  const { clock, socks, dir, heard } = rig();
  dir.create('c1', init(), 'dev_a');
  const w = socks.add('dev_a', [['c1', 1]], clock.now);
  clock.now += TTL + 1;
  w.pinged = clock.now;
  heard.length = 0;
  dir.release('c1', 'dev_a', 1);
  assert.deepEqual(heard, [2], 'held to free is visible once');
  assert.equal(dir.cell('c1').cell.lease.expires, 0);
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'ok');
});

test('SocketWithoutHoldVouchesNothing: the home socket holds nothing', () => {
  const { clock, socks, dir } = rig();
  dir.create('c1', init(), 'dev_a');
  socks.add('dev_a', [], clock.now);
  clock.now += TTL + 1;
  assert.equal(code(() => dir.acquire('c1', 'dev_b')), 'ok');
});

test('PingIsNotAChange and LapseBumpsNothing: evidence moves no version, in either direction', () => {
  const { clock, socks, dir, heard } = rig();
  dir.create('c1', init(), 'dev_a');
  const w = socks.add('dev_a', [['c1', 1]], clock.now);
  heard.length = 0;
  for (let i = 0; i < 5; i += 1) {
    clock.now += 30_000;
    w.pinged = clock.now;
    dir.list();
  }
  clock.now += TTL + 1; // the holder goes quiet and the lease lapses
  assert.ok(dir.list().cells.c1.lease.expires <= clock.now);
  assert.deepEqual(heard, []);
  assert.equal(dir.version, 1);
});

test('a heartbeat while a socket vouches is silent: the lease is held before and after', () => {
  const { clock, socks, dir, heard } = rig();
  dir.create('c1', init(), 'dev_a');
  const w = socks.add('dev_a', [['c1', 1]], clock.now);
  clock.now += TTL + 10_000;
  w.pinged = clock.now;
  heard.length = 0;
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 0 });
  assert.deepEqual(heard, [], 'held (by the socket) before and after');
});

test('a lapsed lease with no socket is renewed by the heartbeat as before, and counts as a change', () => {
  const { clock, dir, heard } = rig();
  dir.create('c1', init(), 'dev_a');
  clock.now += TTL + 1;
  heard.length = 0;
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 0 });
  assert.deepEqual(heard, [2]);
});

test('lifted raises an expiry only, leaves a released lease free, and never lowers one', () => {
  const c = { lease: { device: 'd', fence: 1, expires: 100, pending: 0 } };
  assert.equal(lifted(c, 0), c);
  assert.equal(lifted(c, 50), c);
  assert.equal(lifted(c, 500).lease.expires, 500);
  const released = { lease: { ...c.lease, expires: 0 } };
  assert.equal(lifted(released, 500), released);
});

test('vouchedUntil takes the newest sign of life among the sockets that name this cell and fence', () => {
  const s = [
    { holds: [['a', 1]], seenAt: 10 },
    { holds: [['a', 1], ['b', 2]], seenAt: 30 },
    { holds: [['a', 2]], seenAt: 99 },
  ];
  assert.equal(vouchedUntil(s, 'a', 1, 1000), 1030);
  assert.equal(vouchedUntil(s, 'b', 1, 1000), 0);
  assert.equal(vouchedUntil([], 'a', 1, 1000), 0);
});

test('parseHolds reads distinct pairs, splits at the last colon, and refuses what is malformed or too many', () => {
  assert.deepEqual(parseHolds(['a:1', 'a:1', 'x:y:22'], 16), [['a', 1], ['x:y', 22]]);
  assert.deepEqual(parseHolds([], 16), []);
  for (const bad of [['a'], [':1'], ['a:'], ['a:-1'], ['a:1.5'], ['a:x'], ['a: 1'], ['a:12345678901234567'], [`${'c'.repeat(65)}:1`], ['a:9007199254740993']]) {
    assert.equal(code(() => parseHolds(bad, 16)), 'bad_request', JSON.stringify(bad));
  }
  assert.equal(code(() => parseHolds(Array.from({ length: 17 }, (_, i) => `c${i}:1`), 16)), 'bad_request');
  assert.equal(code(() => parseHolds(Array.from({ length: 16 }, (_, i) => `c${i}:1`), 16)), 'ok');
});
