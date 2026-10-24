import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Directory } from '../src/directory.js';
import { STAGE1 } from '../src/rules.js';
import { windowMs } from '../src/limits.js';
import { parseBeat } from '../src/presence.js';
import { CLOSE_REVOKED, CLOSE_ROTATED, CLOSE_SILENT, Watchers } from '../src/watch.js';
import { memorySql } from './sql.js';
import { init } from './helpers.js';

const DEVICE = { V: 1, name: '', added_by: '', revoked: false, caps: { os: 'linux', arch: 'arm64', sandbox: null, container: null, gpu: null, cow: 'none' } };

/** A recorder stands where the sockets would: it keeps what the directory told them, in order. */
function open(sql = memorySql()) {
  const clock = { now: 1_000_000 };
  const heard = [];
  const watchers = {
    broadcast: (v) => heard.push(`v${v}`),
    closeDevice: (d, code) => heard.push(`close ${d} ${code}`),
    closeAll: (code) => heard.push(`closeAll ${code}`),
    announce: (f) => heard.push(`${f.t} ${f.device}`),
  };
  return { sql, clock, heard, dir: new Directory(sql, 'id_t', () => clock.now, STAGE1, watchers) };
}

test('a fresh directory is at version 0 and each visible change moves it by exactly one', () => {
  const { dir, heard } = open();
  assert.equal(dir.version, 0);
  dir.create('c1', init(), 'dev_a');
  const { cell } = dir.acquire('c1', 'dev_a');
  dir.publish('c1', 'dev_a', { fence: cell.lease.fence, old_head: 'h0', head: 'h1', size: 1, class: 'work', pending: 0 });
  dir.archive('c1');
  dir.putDevice('dev_a', DEVICE);
  dir.setVault('', 'x');
  assert.deepEqual(heard, ['v1', 'v2', 'v3', 'v4', 'v5', 'v6']);
  assert.equal(dir.version, 6);
});

test('a heartbeat that only moves the expiry is silent, and one that changes pending is not', () => {
  const { dir, clock, heard } = open();
  dir.create('c1', init(), 'dev_a');
  heard.length = 0;
  clock.now += 10_000;
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 0 });
  assert.deepEqual(heard, [], 'the lease ran on, nothing a person sees changed');
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 2 });
  assert.deepEqual(heard, ['v2']);
});

test('a lease that is held, released, then renewed is seen to change each time, a second release is not', () => {
  const { dir, clock, heard } = open();
  dir.create('c1', init(), 'dev_a');
  dir.release('c1', 'dev_a', 1);
  dir.release('c1', 'dev_a', 1);
  assert.deepEqual(heard, ['v1', 'v2'], 'held to free once; free to free changes nothing');
  clock.now += 1;
  dir.heartbeat('c1', 'dev_a', { fence: 1, pending: 0 });
  assert.deepEqual(heard, ['v1', 'v2', 'v3'], 'free to held is visible');
});

test('an identical device record, a repeated archive and a repeated revoke change nothing', () => {
  const { dir, heard } = open();
  dir.putDevice('dev_a', DEVICE);
  dir.putDevice('dev_b', DEVICE);
  dir.create('c1', init(), 'dev_a');
  heard.length = 0;
  dir.putDevice('dev_a', DEVICE);
  dir.archive('c1');
  dir.archive('c1');
  dir.revoke('dev_b', 'dev_a');
  dir.revoke('dev_b', 'dev_a');
  assert.deepEqual(heard, ['v4', 'v5', 'revoked dev_b', 'close dev_b 4401', 'revoked dev_b', 'close dev_b 4401']);
});

test('a refused rule writes nothing and counts nothing', () => {
  const { dir, heard } = open();
  dir.create('c1', init(), 'dev_a');
  heard.length = 0;
  assert.throws(() => dir.acquire('c1', 'dev_b'));
  assert.throws(() => dir.publish('c1', 'dev_a', { fence: 9, old_head: 'h0', head: 'h1', size: 1, class: 'work', pending: 0 }));
  assert.deepEqual(heard, []);
  assert.equal(dir.version, 1);
});

test('revoke asks for its device sockets to close with 4401, rotation for all with 4410, thaw for none', () => {
  const { dir, heard } = open();
  dir.putDevice('dev_b', DEVICE);
  heard.length = 0;
  dir.revoke('dev_b', 'dev_a');
  dir.setRotation({ state: 'frozen', by: 'dev_a', at: 1 });
  dir.setRotation(undefined);
  assert.deepEqual(heard, ['v2', 'revoked dev_b', `close dev_b ${CLOSE_REVOKED}`, 'v3', `closeAll ${CLOSE_ROTATED}`, 'v4']);
});

test('the version outlives the object: a new Directory over the same storage continues the count', () => {
  const first = open();
  first.dir.create('c1', init(), 'dev_a');
  first.dir.archive('c1');
  const second = open(first.sql);
  assert.equal(second.dir.version, 2);
  second.dir.putDevice('dev_a', DEVICE);
  assert.equal(second.dir.version, 3);
});

test('key order does not make two equal records different', () => {
  const { dir, heard } = open();
  dir.putDevice('dev_a', DEVICE);
  heard.length = 0;
  dir.putDevice('dev_a', { caps: DEVICE.caps, revoked: false, added_by: '', name: '', V: 1 });
  assert.deepEqual(heard, []);
});

test('an idempotent freeze, and a thaw of a live identity, bump nothing; a real freeze bumps once then closes all', () => {
  const { dir, heard } = open();
  dir.setRotation(undefined);
  assert.deepEqual(heard, [], 'a thaw of an identity that never rotated is no change');
  dir.setRotation({ state: 'frozen', by: 'dev_a', at: 1 });
  dir.setRotation({ state: 'frozen', by: 'dev_a', at: 1 });
  assert.equal(dir.version, 1, 'the owner freezing again stores the same record');
  assert.deepEqual(heard.filter((h) => h.startsWith('v')), ['v1']);
  dir.setRotation(undefined);
  assert.equal(dir.version, 2, 'a thaw of a frozen identity is visible');
});

test('last_seen is stamped without a version, and a device PUT neither sets nor clears it', () => {
  const { dir, heard } = open();
  dir.putDevice('dev_a', DEVICE);
  heard.length = 0;
  dir.seen('dev_a', 777);
  dir.seen('dev_nobody', 778);
  assert.deepEqual(heard, [], 'a sign of life is no change a person sees');
  assert.equal(dir.read('devices', 'dev_a').last_seen, 777);
  dir.putDevice('dev_a', { ...DEVICE, last_seen: 5 });
  assert.equal(dir.read('devices', 'dev_a').last_seen, 777, 'the client value is ignored');
  assert.deepEqual(heard, [], 'and the identical record is no change');
  assert.equal(dir.read('devices', 'dev_nobody'), null, 'seen never creates a device');
});

// Presence on the hosted sockets (contract 21.12): the platform's sockets are a stand-in that keeps an attachment,
// a last auto-response time and the close it was given; the clock is Date.now, replaced by hand.
globalThis.WebSocketRequestResponsePair ??= class {};

function platform() {
  const open = [];
  const kv = new Map();
  const ctx = {
    storage: {
      kv: {
        get: (k) => kv.get(k),
        put: (k, v) => kv.set(k, v),
        delete: (k) => kv.delete(k),
        list: ({ prefix }) => [...kv].filter(([k]) => k.startsWith(prefix)).sort(),
      },
    },
    setWebSocketAutoResponse() {},
    acceptWebSocket: (ws, tags) => open.push(Object.assign(ws, { tags })),
    getWebSockets: (tag) => open.filter((w) => tag === undefined || w.tags.includes(tag)),
    getTags: (ws) => ws.tags,
    getWebSocketAutoResponseTimestamp: (ws) => (ws.pinged ? new Date(ws.pinged) : null),
  };
  const socket = () => {
    const ws = { sent: [], closed: null, attachment: null };
    ws.serializeAttachment = (a) => (ws.attachment = a);
    ws.deserializeAttachment = () => ws.attachment;
    ws.send = (t) => ws.sent.push(t);
    ws.close = (code, reason) => (ws.closed = [code, reason]);
    return ws;
  };
  return { ctx, socket, open };
}

/** A relay of watchers over the stand-in, answering the response the 101 would carry. */
function relay() {
  const real = { Date: Date.now, Pair: globalThis.WebSocketPair, Response: globalThis.Response };
  const clock = { now: 5_000_000 };
  const p = platform();
  globalThis.WebSocketPair = class {
    constructor() {
      Object.assign(this, { 0: {}, 1: p.socket() });
    }
  };
  globalThis.Response = class {
    constructor(body, init) {
      Object.assign(this, init, { headers: new Headers(init.headers) });
    }
  };
  Date.now = () => clock.now;
  const armed = [];
  const stamped = [];
  const watchers = new Watchers(p.ctx, 1000, 90_000, (at) => armed.push(at), (d, at) => stamped.push([d, at]));
  const restore = () => ((Date.now = real.Date), (globalThis.WebSocketPair = real.Pair), (globalThis.Response = real.Response));
  const dial = (device, beat, events = false) => {
    const answer = watchers.accept(device, 1, clock.now, { holds: [], beat, events });
    return { answer, ws: p.open.at(-1) };
  };
  return { clock, armed, stamped, watchers, dial, restore, p };
}

function inRelay(fn) {
  const r = relay();
  try {
    return fn(r);
  } finally {
    r.restore();
  }
}

test('the 101 carries Codeaf-Presence beside Codeaf-Vouch', () =>
  inRelay((r) => {
    const { answer } = r.dial('dev_a', 10);
    assert.equal(answer.headers.get('codeaf-presence'), '1');
    assert.equal(answer.headers.get('codeaf-vouch'), '1');
  }));

test('beat is a plain decimal of 1 to 60: none is 30, the same twice is one, anything else is 400 bad_request', () => {
  assert.equal(parseBeat([]), 30);
  assert.equal(parseBeat(['10', '10']), 10);
  assert.equal(parseBeat(['1']), 1);
  assert.equal(parseBeat(['60']), 60);
  for (const bad of [['0'], ['61'], ['-1'], ['1.5'], ['abc'], [''], ['010'], ['1e1'], [' 5'], ['10', '11']]) {
    assert.throws(() => parseBeat(bad), (e) => e.status === 400 && e.code === 'bad_request', bad.join(','));
  }
});

test('only live sockets are online, and one live socket keeps a device online beside a stale one', () =>
  inRelay((r) => {
    r.dial('dev_a', 10);
    r.dial('dev_b', 10);
    r.clock.now += windowMs(10) + 1;
    const fresh = r.dial('dev_b', 10);
    assert.deepEqual([...r.watchers.online()], ['dev_b'], 'dev_a is stale; dev_b has one live socket');
    assert.equal(fresh.ws.attachment.lapsed, false);
  }));

test('a pinged socket stays live past its accept window; a socket with no beat has the 75 s window', () =>
  inRelay((r) => {
    const pinger = r.dial('dev_a', 10);
    const legacy = r.dial('dev_old', parseBeat([]));
    r.clock.now += 60_000;
    pinger.ws.pinged = r.clock.now;
    assert.deepEqual([...r.watchers.online()].sort(), ['dev_a', 'dev_old']);
    r.clock.now += 15_001;
    assert.deepEqual([...r.watchers.online()], ['dev_a'], 'the old client is silent past 75 s; the pinger is 15 s since its ping');
    assert.equal(legacy.ws.attachment.beat, 30);
  }));

test('a sweep tells the viewers once, stamps last_seen, lapses and closes the socket 4408, and keeps one alarm for the viewer', () =>
  inRelay((r) => {
    const viewer = r.dial('dev_a', 10, true);
    const frozen = r.dial('dev_b', 10);
    viewer.ws.sent.length = 0;
    r.clock.now += 20_000;
    viewer.ws.pinged = r.clock.now;
    r.clock.now += 6_000;
    viewer.ws.pinged = r.clock.now;
    r.watchers.expire();
    assert.deepEqual(frozen.ws.closed, [CLOSE_SILENT, 'silent']);
    assert.equal(frozen.ws.attachment.lapsed, true);
    assert.deepEqual(r.stamped, [['dev_b', 5_000_000]]);
    assert.deepEqual(viewer.ws.sent.map((t) => JSON.parse(t)).map(({ t, device, online }) => [t, device, online]), [['presence', 'dev_b', false]]);
    r.watchers.expire();
    assert.equal(viewer.ws.sent.length, 1, 'announced once');
    assert.equal(r.armed.at(-1), r.clock.now + windowMs(10), 'the viewer lapses a window after its ping');
  }));

test('the close of a lapsed socket starts no debounce and answers no device; that of a live one answers its device', () =>
  inRelay((r) => {
    r.dial('dev_a', 10, true);
    const frozen = r.dial('dev_b', 10);
    const live = r.dial('dev_c', 10);
    r.clock.now += windowMs(10) + 1;
    live.ws.pinged = r.clock.now;
    r.armed.length = 0;
    assert.equal(r.watchers.left(frozen.ws), undefined);
    assert.deepEqual(r.armed, []);
    assert.equal(r.watchers.left(live.ws), 'dev_c');
    assert.equal(r.armed.length, 1, 'the debounce of a live close is armed as before');
  }));

test('no sweep without a viewer: sockets without events arm nothing; an events socket arms, and a later socket that lapses sooner moves it', () =>
  inRelay((r) => {
    r.dial('dev_a', 10);
    assert.deepEqual(r.armed, []);
    r.dial('dev_b', 30, true);
    assert.deepEqual(r.armed, [r.clock.now + windowMs(10)], 'dev_a lapses before the viewer');
    r.clock.now += 1_000;
    r.dial('dev_c', 2);
    assert.equal(r.armed.at(-1), r.clock.now + windowMs(2), 'a beat of 2 s lapses before the sweep that was set');
  }));

test('an attachment from before presence reads as a live-by-default beat of 30 and not lapsed', () =>
  inRelay((r) => {
    const old = r.p.socket();
    old.attachment = { at: r.clock.now, holds: [], events: false };
    old.tags = ['dev_a'];
    r.p.open.push(old);
    r.clock.now += 74_000;
    assert.deepEqual([...r.watchers.online()], ['dev_a']);
    r.clock.now += 1_001;
    assert.deepEqual([...r.watchers.online()], []);
  }));
