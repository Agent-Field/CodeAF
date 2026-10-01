import { test } from 'node:test';
import assert from 'node:assert/strict';
import { OFFLINE_DEBOUNCE_MS, Liveness } from '../src/liveness.js';

/** A room is a set of fake sockets and the clock; each socket keeps the frames it was sent. */
function room() {
  const now = { t: 1_000 };
  const sockets = [];
  const armed = [];
  const pending = new Map();
  const liveness = new Liveness({ peers: () => sockets, pending, arm: (at) => armed.push(at), clock: () => now.t });
  const join = (device, events = true) => {
    const s = { ws: Symbol(device), device, events, got: [], send: (t) => s.got.push(JSON.parse(t)) };
    sockets.push(s);
    liveness.opened(device);
    return s;
  };
  const leave = (s) => {
    sockets.splice(sockets.indexOf(s), 1);
    return liveness.closed(s);
  };
  return { now, sockets, armed, pending, liveness, join, leave };
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
  assert.deepEqual(r.armed, [1_000 + 10_000 + OFFLINE_DEBOUNCE_MS]);
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
