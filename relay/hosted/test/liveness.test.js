import { test } from 'node:test';
import assert from 'node:assert/strict';
import { OFFLINE_DEBOUNCE_MS, Liveness } from '../src/liveness.js';
import { windowMs } from '../src/limits.js';

/** A room is a set of fake sockets and the clock; each socket keeps the frames it was sent. */
function room() {
  const now = { t: 1_000 };
  const sockets = [];
  const armed = [];
  const pending = new Map();
  const stamped = [];
  const liveness = new Liveness({
    peers: () => sockets,
    pending,
    arm: (at) => armed.push(at),
    clock: () => now.t,
    seen: (device, at) => stamped.push([device, at]),
  });
  // A socket declares a beat (default 30 s, as a client from before presence does); lapse() is what a sweep calls.
  const join = (device, events = true, beat = 30) => {
    const s = {
      ws: Symbol(device),
      device,
      events,
      got: [],
      send: (t) => s.got.push(JSON.parse(t)),
      sign: now.t,
      windowMs: windowMs(beat),
      lapsed: false,
      lapse: () => ((s.lapsed = true), (s.closedSilent = true)),
    };
    sockets.push(s);
    liveness.opened(device);
    return s;
  };
  const ping = (s) => (s.sign = now.t);
  const leave = (s) => {
    sockets.splice(sockets.indexOf(s), 1);
    return liveness.closed(s);
  };
  return { now, sockets, armed, pending, stamped, liveness, join, leave, ping };
}

const flat = (s) => s.got.map((f) => `${f.device}:${f.online}`);

test('the first socket of a device is news to the other event sockets, a second socket is not', () => {
  const r = room();
  const a = r.join('dev_a');
  const b = r.join('dev_b');
  r.join('dev_b');
  assert.deepEqual(flat(a), ['dev_b:true']);
  assert.deepEqual(b.got, [], 'a device is not told of itself');
});

test('a socket without events never receives an event frame', () => {
  const r = room();
  const old = r.join('dev_a', false);
  r.join('dev_b');
  r.liveness.tell({ t: 'joined', device: 'dev_c' }, 'dev_c');
  assert.deepEqual(old.got, []);
});

test('the last close starts a 15 s debounce; the alarm then says offline once', () => {
  const r = room();
  const a = r.join('dev_a');
  const b = r.join('dev_b');
  assert.equal(r.leave(b), true);
  assert.deepEqual(r.armed, [1_000 + OFFLINE_DEBOUNCE_MS]);
  a.got.length = 0;
  r.now.t += OFFLINE_DEBOUNCE_MS - 1;
  r.liveness.expire();
  assert.deepEqual(a.got, [], 'not yet');
  r.now.t += 1;
  r.liveness.expire();
  assert.deepEqual(flat(a), ['dev_b:false']);
  r.liveness.expire();
  assert.equal(a.got.length, 1, 'once');
});

test('closing one of two sockets of a device is not the last close', () => {
  const r = room();
  r.join('dev_a');
  const b1 = r.join('dev_b');
  r.join('dev_b');
  assert.equal(r.leave(b1), false);
  assert.deepEqual(r.armed, []);
});

test('a reconnect inside the gap sends nothing at all, neither offline nor online', () => {
  const r = room();
  const a = r.join('dev_a');
  const b = r.join('dev_b');
  a.got.length = 0;
  r.leave(b);
  r.now.t += 5_000;
  r.join('dev_b');
  r.now.t += 20_000;
  r.liveness.expire();
  assert.deepEqual(a.got, []);
  assert.equal(r.pending.size, 0);
});

test('a reconnect after the gap is a fresh online', () => {
  const r = room();
  const a = r.join('dev_a');
  const b = r.join('dev_b');
  r.leave(b);
  r.now.t += OFFLINE_DEBOUNCE_MS;
  r.liveness.expire();
  r.join('dev_b');
  assert.deepEqual(flat(a), ['dev_b:true', 'dev_b:false', 'dev_b:true']);
});

test('expire re-arms for a device that is still waiting', () => {
  const r = room();
  r.join('dev_a');
  const b = r.join('dev_b');
  const c = r.join('dev_c');
  r.leave(b);
  r.now.t += 10_000;
  r.leave(c);
  r.armed.length = 0;
  r.now.t += 5_000;
  r.liveness.expire();
  assert.equal(r.armed[0], 1_000 + 10_000 + OFFLINE_DEBOUNCE_MS, 'the debounce first; the sweep of the live viewer follows it');
});

test('a new event socket hears who is online, devices still in their gap included, and not itself', () => {
  const r = room();
  r.join('dev_a');
  const b = r.join('dev_b');
  r.leave(b);
  const c = r.join('dev_c');
  r.liveness.snapshot('dev_c').forEach((t) => c.send(t));
  assert.deepEqual(flat(c).sort(), ['dev_a:true', 'dev_b:true']);
  assert.deepEqual([...r.liveness.online()].sort(), ['dev_a', 'dev_c']);
});

test('an event is dropped, not cut, when it would pass 512 bytes', () => {
  const r = room();
  const a = r.join('dev_a');
  r.liveness.tell({ t: 'joined', device: 'dev_b', name: 'x'.repeat(600) }, 'dev_b');
  r.liveness.tell({ t: 'joined', device: 'dev_b', name: 'bg==', platform: 'linux', at: 5 }, 'dev_b');
  assert.deepEqual(a.got, [{ t: 'joined', device: 'dev_b', name: 'bg==', platform: 'linux', at: 5 }]);
});

// Presence from the sign of life (contract 21.12). Beat 10 gives a window of 25 s; the default beat 30 gives 75 s.
const WINDOW = windowMs(10);

test('the window is two and a half beats, 25 s for a beat of 10 s and 75 s for a client that declared none', () => {
  assert.equal(WINDOW, 25_000);
  assert.equal(windowMs(30), 75_000);
});

test('no sweep without a viewer: nothing is armed when no socket asked for events', () => {
  const r = room();
  r.join('dev_a', false, 10);
  r.join('dev_b', false, 10);
  assert.equal(r.liveness.nextSweep(), undefined);
  r.liveness.rearm();
  assert.deepEqual(r.armed, []);
});

test('no sweep without a live socket either: the alarm stops when everything is silent and told', () => {
  const r = room();
  r.join('dev_a', true, 10);
  r.now.t += WINDOW + 1;
  assert.equal(r.liveness.nextSweep(), undefined);
});

test('the sweep is armed at the earliest lapse among live sockets, never sooner than a second from now', () => {
  const r = room();
  r.join('dev_a', true, 10);
  r.now.t += 10_000;
  const b = r.join('dev_b', false, 10);
  r.now.t += 5_000;
  assert.equal(r.liveness.nextSweep(), 1_000 + WINDOW, 'dev_a, accepted first, lapses first');
  r.ping(b);
  r.now.t = 1_000 + WINDOW - 200;
  assert.equal(r.liveness.nextSweep(), r.now.t + 1_000, 'a lapse a hair away waits the minimum gap');
});

test('a frozen device is told offline once, stamped with its last sign of life, and its socket is lapsed', () => {
  const r = room();
  const viewer = r.join('dev_a', true, 10);
  const frozen = r.join('dev_b', false, 10);
  viewer.got.length = 0;
  r.now.t += 10_000;
  r.ping(viewer);
  r.now.t += 15_000;
  r.liveness.sweep();
  assert.deepEqual(viewer.got, [], 'inside its window until 25 s of silence is over');
  r.now.t += 1;
  r.ping(viewer);
  r.liveness.sweep();
  assert.deepEqual(flat(viewer), ['dev_b:false']);
  assert.deepEqual(r.stamped, [['dev_b', 1_000]]);
  assert.equal(frozen.closedSilent, true);
  r.liveness.sweep();
  r.liveness.sweep();
  assert.equal(viewer.got.length, 1, 'announced once');
  assert.equal(r.stamped.length, 1);
});

test('a sweep through the alarm re-arms at the next lapse while a viewer and a live socket remain', () => {
  const r = room();
  const viewer = r.join('dev_a', true, 10);
  r.join('dev_b', false, 10);
  r.now.t += 20_000;
  r.ping(viewer);
  r.liveness.expire();
  assert.deepEqual(r.armed.slice(-1), [1_000 + WINDOW], 'dev_b, never pinged since its accept, lapses before the viewer that just did');
  r.now.t += 6_000;
  r.liveness.expire();
  assert.deepEqual(flat(viewer), ['dev_b:true', 'dev_b:false']);
});

test('the lapsed socket closes silently: no debounce, nothing announced', () => {
  const r = room();
  const viewer = r.join('dev_a', true, 10);
  const frozen = r.join('dev_b', false, 10);
  r.now.t += WINDOW + 1;
  r.ping(viewer);
  r.liveness.sweep();
  viewer.got.length = 0;
  r.armed.length = 0;
  assert.equal(r.leave(frozen), false);
  assert.deepEqual(r.armed, []);
  assert.equal(r.pending.size, 0);
  r.liveness.expire();
  assert.deepEqual(viewer.got, []);
});

test('legacy window: a socket that declared no beat is online at 60 s without a ping and offline after 75 s', () => {
  const r = room();
  const viewer = r.join('dev_a', true, 10);
  r.join('dev_old');
  r.now.t += 60_000;
  r.ping(viewer);
  assert.deepEqual([...r.liveness.online()].sort(), ['dev_a', 'dev_old']);
  r.now.t += 15_001;
  r.ping(viewer);
  assert.deepEqual([...r.liveness.online()], ['dev_a']);
});

test('one live socket keeps the device online and unannounced although another socket of it is stale', () => {
  const r = room();
  const viewer = r.join('dev_a', true, 10);
  r.join('dev_b', false, 10);
  r.now.t += 20_000;
  const second = r.join('dev_b', false, 10);
  r.ping(viewer);
  r.now.t += 6_000;
  r.ping(viewer);
  r.ping(second);
  viewer.got.length = 0;
  r.liveness.sweep();
  assert.deepEqual(viewer.got, []);
  assert.deepEqual([...r.liveness.online()].sort(), ['dev_a', 'dev_b']);
});

test('a stale socket is no one of the online set and not in a new viewer snapshot', () => {
  const r = room();
  r.join('dev_a', true, 10);
  r.join('dev_b', false, 10);
  r.now.t += WINDOW + 1;
  const c = r.join('dev_c', true, 10);
  r.liveness.snapshot('dev_c').forEach((t) => c.send(t));
  assert.deepEqual(c.got.map((f) => f.device), []);
  assert.deepEqual([...r.liveness.online()], ['dev_c']);
});

test('a device that comes back after being told offline is told online at once', () => {
  const r = room();
  const viewer = r.join('dev_a', true, 10);
  const frozen = r.join('dev_b', false, 10);
  r.now.t += WINDOW + 1;
  r.ping(viewer);
  r.liveness.sweep();
  r.leave(frozen);
  r.join('dev_b', false, 10);
  assert.deepEqual(flat(viewer), ['dev_b:true', 'dev_b:false', 'dev_b:true']);
});
