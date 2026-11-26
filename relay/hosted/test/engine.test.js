import { test } from 'node:test';
import assert from 'node:assert/strict';
import { nameFor, validName } from '../src/engine/names.js';
import { readFileSync } from 'node:fs';
import {
  BREAKER_BYTES_PER_SECOND, BREAKER_REASON, BREAKER_WINDOW_MS, DIALS_PER_MINUTE, DOOR_LIVE, DOOR_MOVE, ENGINE_PATH, HEADER_DOOR,
  HEADER_KEY, HEADER_NAME, LIVE_STREAMS, MAX_PAYLOAD, MAX_STREAMS, OP, PROTOCOL,
  REGISTRATIONS_PER_MINUTE, TOO_MANY_DIALS, TOO_MANY_LIVE, TOO_MANY_REGISTRATIONS,
  doorOf, frame, readFrame, registrationProof, secondsToNextMinute, verdict,
} from '../src/engine/carrier.js';
import { Breaker } from '../src/engine/breaker.js';
import { serveMachineDoor } from '../src/engine/door.js';
import { MachineRoom } from '../src/engine/room.js';

const hex = (text) => Uint8Array.from(text.match(/../g), (b) => parseInt(b, 16));
const toHex = (bytes) => [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
const run = (from, n = 32) => Uint8Array.from({ length: n }, (_, i) => (from + i) & 0xff);
const b64url = (bytes) => Buffer.from(bytes).toString('base64url');

// PKCS#8 header for a raw 32-byte X25519 private seed (RFC 8410).
const PKCS8_X25519 = hex('302e020100300506032b656e04220420');

async function keyFromSeed(seed32) {
  const pkcs8 = new Uint8Array(PKCS8_X25519.length + 32);
  pkcs8.set(PKCS8_X25519);
  pkcs8.set(seed32, PKCS8_X25519.length);
  const privateKey = await crypto.subtle.importKey('pkcs8', pkcs8, { name: 'X25519' }, true, ['deriveBits']);
  // JWK export of an X25519 private key carries the derived raw public key in `x`.
  const jwk = await crypto.subtle.exportKey('jwk', privateKey);
  const publicKey = new Uint8Array(Buffer.from(jwk.x, 'base64url'));
  return { privateKey, publicKey, name: await nameFor(publicKey) };
}

let cachedCollision = null;

// collidingKeys finds two distinct X25519 keypairs whose public keys derive the exact same machine name.
async function collidingKeys() {
  if (cachedCollision) return cachedCollision;
  const seen = new Map();
  for (let i = 1; ; i += 1) {
    const seed = new Uint8Array(32);
    new DataView(seed.buffer).setUint32(0, i, true);
    const kp = await keyFromSeed(seed);
    const other = seen.get(kp.name);
    if (other && toHex(other.publicKey) !== toHex(kp.publicKey)) {
      cachedCollision = [other, kp];
      return cachedCollision;
    }
    seen.set(kp.name, kp);
  }
}

async function answerChallenge(challengeMessage, name, kp) {
  const relayPub = await crypto.subtle.importKey('raw', challengeMessage.subarray(0, 32), { name: 'X25519' }, false, []);
  const shared = new Uint8Array(await crypto.subtle.deriveBits({ name: 'X25519', public: relayPub }, kp.privateKey, 256));
  const nonce = challengeMessage.subarray(32, 64);
  return registrationProof(shared, nonce, name, kp.publicKey);
}

// makeRoom harnesses MachineRoom with a real in-memory hibernatable-WebSocket context in Node.
function makeRoom(options = {}) {
  const realPair = globalThis.WebSocketPair;
  const realResponse = globalThis.Response;
  const sockets = [];
  const kv = new Map();
  const clock = { now: 1_000_000 };
  /** Every storage write and every alarm the object asks for, so a test can say none happened beyond the stream counter. */
  const puts = [];
  const alarms = [];

  function makeSocket(peerRef) {
    const ws = {
      tags: [],
      attachment: null,
      sent: [],
      closed: null,
      accepted: false,
      sendFail: false,
      peer: peerRef,
      serializeAttachment(value) {
        this.attachment = structuredClone(value);
      },
      deserializeAttachment() {
        return this.attachment ? structuredClone(this.attachment) : null;
      },
      send(data) {
        if (this.sendFail || this.closed) throw new Error('WebSocket is closing');
        const copy = typeof data === 'string' ? data : new Uint8Array(data);
        this.sent.push(copy);
        if (this.peer && this.peer.onmessage) this.peer.onmessage(copy);
      },
      close(code = 1000, reason = '') {
        this.closed = [code, reason];
      },
    };
    return ws;
  }

  globalThis.WebSocketPair = class {
    constructor() {
      const client = { sent: [], onmessage: null };
      const server = makeSocket(client);
      client.server = server;
      Object.assign(this, { 0: client, 1: server });
    }
  };

  globalThis.Response = class extends realResponse {
    constructor(body, init = {}) {
      const { status = 200, webSocket, ...rest } = init;
      super(status === 101 ? null : body, { ...rest, status: status === 101 ? 200 : status });
      this.realStatus = status;
      this.webSocket = webSocket;
    }
    get status() {
      return this.realStatus;
    }
  };

  const ctx = {
    storage: {
      kv: { get: (k) => kv.get(k), put: (k, v) => (puts.push(k), kv.set(k, v)) },
      setAlarm: (at) => alarms.push(['set', at]),
      deleteAlarm: () => alarms.push(['delete']),
    },
    acceptWebSocket(ws, tags = []) {
      ws.accepted = true;
      ws.tags = [...tags];
      sockets.push(ws);
    },
    getWebSockets(tag) {
      // keepClosing simulates Cloudflare keeping a socket in getWebSockets while a silent peer has not echoed Close.
      return sockets.filter((ws) => (!ws.closed || ws.keepClosing) && (tag === undefined || ws.tags.includes(tag)));
    },
  };

  const env = options.env ?? {};
  const room = new MachineRoom(ctx, env, {
    probeMs: options.probeMs ?? 25,
    handshakeMs: options.handshakeMs ?? 20_000,
    clock: () => clock.now,
  });

  const restore = () => {
    globalThis.WebSocketPair = realPair;
    globalThis.Response = realResponse;
  };

  async function register(kp, { answerPings = true, ip = '198.51.100.1' } = {}) {
    const req = new Request('https://relay.example' + ENGINE_PATH, {
      headers: {
        upgrade: 'websocket',
        'sec-websocket-protocol': PROTOCOL,
        'cf-connecting-ip': ip,
        [HEADER_NAME]: kp.name,
        [HEADER_KEY]: b64url(kp.publicKey),
      },
    });
    const res = await room.fetch(req);
    assert.equal(res.status, 101);
    const server = res.webSocket.server;
    assert.equal(server.sent.length, 1);
    const proof = await answerChallenge(server.sent[0], kp.name, kp);
    res.webSocket.onmessage = (msg) => {
      if (!answerPings) return;
      const parsed = readFrame(msg);
      if (parsed && parsed.op === OP.ping) {
        void room.webSocketMessage(server, frame(OP.pong, parsed.stream));
      }
    };
    await room.webSocketMessage(server, proof);
    const last = server.sent.at(-1);
    return { res, client: res.webSocket, server, verdict: last };
  }

  async function dial(name, ip = '198.51.100.2', headers = {}) {
    const req = new Request('https://relay.example/v1/dial/' + name, {
      headers: {
        upgrade: 'websocket',
        'sec-websocket-protocol': PROTOCOL,
        'cf-connecting-ip': ip,
        ...headers,
      },
    });
    return room.fetch(req);
  }

  return { room, ctx, sockets, clock, register, dial, restore, puts, alarms };
}

async function withRoom(options, fn) {
  const harness = makeRoom(options);
  try {
    return await fn(harness);
  } finally {
    harness.restore();
  }
}

// stubLimiter mimics Cloudflare's [[ratelimits]] binding (`env.<NAME>.limit({ key })`).
function stubLimiter(limit) {
  const counts = new Map();
  return {
    counts,
    async limit({ key }) {
      const next = (counts.get(key) ?? 0) + 1;
      counts.set(key, next);
      return { success: next <= limit };
    },
  };
}

// Computed by internal/relay's NameFor; the port must derive the same name from the same key.
test('a machine name is the one the Go side derives from the same key, and validName accepts only real names', async () => {
  for (const [key, name] of [
    ['000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f', 'lantern-amber-86'],
    ['0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20', 'jasmine-dragon-58'],
    ['7f808182838485868788898a8b8c8d8e8f909192939495969798999a9b9c9d9e', 'quartz-donkey-28'],
    ['ff000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e', 'lavender-rhubarb-13'],
  ]) {
    assert.equal(await nameFor(hex(key)), name);
    assert.ok(validName(name));
  }
  assert.ok(validName('otter-lamp-10'));
  assert.ok(validName('willow-acorn-99'));
  for (const bad of [
    'otter-lamp-4',
    'Otter-lamp-42',
    'otter-lamp',
    'otter-lamp-42-x',
    'otterlampotter-x-42',
    'a-a-42',
    'notaword-lamp-42',
    'otter-notaword-42',
    'otter-lamp-00',
    'otter-lamp-09',
  ]) {
    assert.ok(!validName(bad), bad);
  }
});

// Computed by internal/relay's registrationProof over the same shared secret, nonce, name and key.
test('the possession proof is the one the Go side computes', async () => {
  const key = run(200);
  const proof = await registrationProof(run(0), run(100), 'bluff-meadow-35', key);
  assert.equal(toHex(proof), '858616a2eaed21cdcae7e1c815c02ec366f7cc550f63ccf189881aa641feff82');
});

test('a frame is one message and a message that is not exactly one frame is refused', () => {
  const sent = frame(2, '7', new Uint8Array([1, 2, 3]));
  assert.deepEqual([...sent.subarray(0, 13)], [2, 0, 0, 0, 0, 0, 0, 0, 7, 0, 0, 0, 3]);
  const got = readFrame(sent);
  assert.equal(got.op, 2);
  assert.equal(got.stream, '7');
  assert.deepEqual([...got.payload], [1, 2, 3]);
  assert.equal(readFrame(sent.subarray(0, 15)), null);
  assert.equal(readFrame(new Uint8Array([...sent, 9])), null);
  const huge = new Uint8Array(13);
  new DataView(huge.buffer).setUint32(9, MAX_PAYLOAD + 1);
  assert.equal(readFrame(huge), null);
});

test('a verdict is ready, or refused with its sentence', () => {
  assert.deepEqual([...verdict()], [1, 0]);
  const said = verdict('no');
  assert.deepEqual([...said], [2, 2, 110, 111]);
});

// Contract item 4 (part 1): invalid names are refused in the Worker front before any Durable Object is touched.
test('the Worker front refuses invalid machine names before touching a Durable Object', async () => {
  let touched = 0;
  const env = {
    MACHINE_DIAL_LIMIT: stubLimiter(DIALS_PER_MINUTE),
    MACHINE_REGISTER_LIMIT: stubLimiter(REGISTRATIONS_PER_MINUTE),
    MACHINE: {
      idFromName() {
        touched += 1;
        throw new Error('MACHINE.idFromName must not be called for an invalid name');
      },
      get() {
        touched += 1;
        throw new Error('MACHINE.get must not be called for an invalid name');
      },
    },
  };

  for (const bad of ['a-a-42', 'otter-lamp-00', 'otter-lamp-09', 'notaword-lamp-42', 'otter-lamp-999']) {
    const dialRes = await serveMachineDoor(
      new Request('https://relay.example/v1/dial/' + bad, {
        headers: { upgrade: 'websocket', 'sec-websocket-protocol': PROTOCOL, 'cf-connecting-ip': '203.0.113.7' },
      }),
      env,
    );
    assert.equal(dialRes.status, 400);
    assert.equal(await dialRes.text(), 'that is not a machine name');
  }

  const regRes = await serveMachineDoor(
    new Request('https://relay.example' + ENGINE_PATH, {
      headers: {
        upgrade: 'websocket',
        'sec-websocket-protocol': PROTOCOL,
        'cf-connecting-ip': '203.0.113.7',
        [HEADER_NAME]: 'a-a-42',
      },
    }),
    env,
  );
  assert.equal(regRes.status, 400);
  assert.equal(await regRes.text(), 'that is not a machine name');
  assert.equal(touched, 0);
});

// Contract item 4 (part 2): rate limits follow the caller network across distinct names before touching env.MACHINE.
test('the 31st dial and 11th registration in a minute from one network across distinct names are refused with 429', async () => {
  const dialLimiter = stubLimiter(DIALS_PER_MINUTE);
  const regLimiter = stubLimiter(REGISTRATIONS_PER_MINUTE);
  const reached = [];
  const env = {
    MACHINE_DIAL_LIMIT: dialLimiter,
    MACHINE_REGISTER_LIMIT: regLimiter,
    MACHINE: {
      idFromName: (name) => name,
      get: (id) => ({
        async fetch() {
          reached.push(id);
          return new Response('no machine is connected under that name', { status: 404 });
        },
      }),
    },
  };

  for (let i = 0; i < DIALS_PER_MINUTE; i += 1) {
    const name = `acorn-alder-${10 + i}`;
    const res = await serveMachineDoor(
      new Request('https://relay.example/v1/dial/' + name, {
        // Distinct hosts inside one IPv6 /64 share one caller network bucket, just as ipOf enforces elsewhere.
        headers: { upgrade: 'websocket', 'sec-websocket-protocol': PROTOCOL, 'cf-connecting-ip': `2001:db8:1:2::${i + 1}` },
      }),
      env,
    );
    assert.equal(res.status, 404);
  }
  assert.equal(reached.length, DIALS_PER_MINUTE);

  const thirtyFirst = await serveMachineDoor(
    new Request('https://relay.example/v1/dial/acorn-alder-50', {
      headers: { upgrade: 'websocket', 'sec-websocket-protocol': PROTOCOL, 'cf-connecting-ip': '2001:db8:1:2::99' },
    }),
    env,
  );
  assert.equal(thirtyFirst.status, 429);
  assert.equal(await thirtyFirst.text(), TOO_MANY_DIALS);
  assert.equal(reached.length, DIALS_PER_MINUTE, 'the 31st dial must not wake a Durable Object');

  for (let i = 0; i < REGISTRATIONS_PER_MINUTE; i += 1) {
    const name = `amber-anchor-${10 + i}`;
    const res = await serveMachineDoor(
      new Request('https://relay.example' + ENGINE_PATH, {
        headers: {
          upgrade: 'websocket',
          'sec-websocket-protocol': PROTOCOL,
          'cf-connecting-ip': '198.51.100.44',
          [HEADER_NAME]: name,
        },
      }),
      env,
    );
    assert.equal(res.status, 404);
  }
  const eleventh = await serveMachineDoor(
    new Request('https://relay.example' + ENGINE_PATH, {
      headers: {
        upgrade: 'websocket',
        'sec-websocket-protocol': PROTOCOL,
        'cf-connecting-ip': '198.51.100.44',
        [HEADER_NAME]: 'amber-anchor-30',
      },
    }),
    env,
  );
  assert.equal(eleventh.status, 429);
  assert.equal(await eleventh.text(), TOO_MANY_REGISTRATIONS);
  assert.equal(reached.length, DIALS_PER_MINUTE + REGISTRATIONS_PER_MINUTE, 'the 11th registration must not wake a Durable Object');
});

// Contract item 3: key-checked replacement on MachineRoom (different key + silent old -> refused;
// same key + live old -> refused; same key + silent old -> replaces).
test('only the same public key may replace a silent registration', async () => {
  const [owner, squatter] = await collidingKeys();
  assert.equal(owner.name, squatter.name);
  assert.notEqual(toHex(owner.publicKey), toHex(squatter.publicKey));

  // 1. Different key + silent old -> refused without evicting the owner.
  await withRoom({ probeMs: 20 }, async (h) => {
    const first = await h.register(owner, { answerPings: false });
    assert.deepEqual([...first.verdict], [1, 0]);

    const impostor = await h.register(squatter, { answerPings: true, ip: '198.51.100.9' });
    assert.equal(impostor.verdict[0], 2, 'a colliding key must be refused even when the owner is silent');
    assert.match(new TextDecoder().decode(impostor.verdict.subarray(2)), /already connected/);
    assert.equal(first.server.closed, null, 'the silent owner must not be evicted by a different key');
  });

  // 2. Same key + live old -> refused.
  await withRoom({ probeMs: 20 }, async (h) => {
    const first = await h.register(owner, { answerPings: true });
    assert.deepEqual([...first.verdict], [1, 0]);

    const second = await h.register(owner, { answerPings: true });
    assert.equal(second.verdict[0], 2, 'a live registration must not be replaced even by the same key');
    assert.equal(first.server.closed, null);
  });

  // 3. Same key + silent old -> replaces and routes new dials to the new socket.
  await withRoom({ probeMs: 20 }, async (h) => {
    const first = await h.register(owner, { answerPings: false });
    assert.deepEqual([...first.verdict], [1, 0]);

    const second = await h.register(owner, { answerPings: true });
    assert.deepEqual([...second.verdict], [1, 0], 'the same key must replace its own silent socket');
    assert.equal(first.server.closed?.[0], 1001);

    const surface = await h.dial(owner.name);
    assert.equal(surface.status, 101);
    const opened = readFrame(second.server.sent.at(-1));
    assert.equal(opened?.op, OP.open);
  });
});

// Contract item 7 (Finding 6 & Finding 1 socket hygiene): throwing engine.send in #dial leaks no stream slot,
// closing/errored surface sockets free MAX_STREAMS immediately, dropped pending registrants do not evict older,
// and abandoned pending upgrades are swept past HANDSHAKE_MS.
test('MachineRoom cleans up throwing sends, closing surfaces, dropped challengers and stale pending sockets', async () => {
  const [owner] = await collidingKeys();

  // 1. engine.send throwing during #dial does not leak a DIAL socket or return 500.
  await withRoom({ probeMs: 20 }, async (h) => {
    const first = await h.register(owner);
    first.server.sendFail = true;
    const failedDial = await h.dial(owner.name);
    assert.equal(failedDial.status, 404);
    assert.equal(h.ctx.getWebSockets('dial').length, 0, 'failed #dial must not accept an orphaned surface socket');
  });

  // 2. 16 surface sockets that error/close while their peer withholds Close echo still free MAX_STREAMS at once.
  await withRoom({ probeMs: 20 }, async (h) => {
    await h.register(owner);
    const surfaces = [];
    for (let i = 0; i < MAX_STREAMS; i += 1) {
      const res = await h.dial(owner.name, `198.51.100.${10 + i}`);
      assert.equal(res.status, 101);
      res.webSocket.server.keepClosing = true;
      surfaces.push(res.webSocket.server);
    }
    const full = await h.dial(owner.name, '198.51.100.99');
    assert.equal(full.status, 429);

    await h.room.webSocketError(surfaces[0]);
    assert.equal(surfaces[0].deserializeAttachment()?.role, 'ended');
    const reopened = await h.dial(owner.name, '198.51.100.100');
    assert.equal(reopened.status, 101, 'an errored surface socket must release its MAX_STREAMS slot immediately');
  });

  // 3. A new same-key registrant that drops during the PROBE_MS wait does not evict older.
  await withRoom({ probeMs: 30 }, async (h) => {
    const first = await h.register(owner, { answerPings: false });
    const req = new Request('https://relay.example' + ENGINE_PATH, {
      headers: {
        upgrade: 'websocket',
        'sec-websocket-protocol': PROTOCOL,
        'cf-connecting-ip': '198.51.100.2',
        [HEADER_NAME]: owner.name,
        [HEADER_KEY]: b64url(owner.publicKey),
      },
    });
    const res = await h.room.fetch(req);
    const challenger = res.webSocket.server;
    const proof = await answerChallenge(challenger.sent[0], owner.name, owner);
    const pendingProof = h.room.webSocketMessage(challenger, proof);
    await h.room.webSocketClose(challenger, 1006);
    await pendingProof;
    assert.equal(first.server.closed, null, 'a challenger that disconnected mid-probe must not evict the existing socket');
    assert.equal(challenger.deserializeAttachment()?.role, 'ended');
  });

  // 4. Abandoned pending /v1/engine sockets are swept once HANDSHAKE_MS elapses.
  await withRoom({ handshakeMs: 500 }, async (h) => {
    const req = new Request('https://relay.example' + ENGINE_PATH, {
      headers: {
        upgrade: 'websocket',
        'sec-websocket-protocol': PROTOCOL,
        'cf-connecting-ip': '198.51.100.3',
        [HEADER_NAME]: owner.name,
        [HEADER_KEY]: b64url(owner.publicKey),
      },
    });
    const res = await h.room.fetch(req);
    const pending = res.webSocket.server;
    assert.equal(pending.deserializeAttachment()?.role, 'pending');
    h.clock.now += 501;
    await h.dial(owner.name);
    assert.equal(pending.closed?.[0], 1008);
    assert.equal(pending.deserializeAttachment()?.role, 'ended');
  });
});

// ---------------------------------------------------------------------------------------------------------------
// The dial class (contract V21). A live dial stops at LIVE_STREAMS of a machine's streams, a move keeps the last
// MAX_STREAMS - LIVE_STREAMS, and a dial that claims no class behaves exactly as it did before the header existed.
// ---------------------------------------------------------------------------------------------------------------

const asClass = (door) => (door === undefined ? {} : { [HEADER_DOOR]: door });
const ipN = (n) => `198.51.100.${n}`;

// dialN opens `count` dials of one class from distinct networks, so only the stream ceiling can refuse them.
async function dialN(h, name, count, door, from = 10) {
  const opened = [];
  for (let i = 0; i < count; i += 1) {
    const res = await h.dial(name, ipN(from + i), asClass(door));
    assert.equal(res.status, 101, `dial ${i + 1} of class ${JSON.stringify(door)} should have been accepted`);
    opened.push(res.webSocket.server);
  }
  return opened;
}

test('V21 live dials stop at fourteen and a move still gets the last two', async () => {
  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    await h.register(owner);
    await dialN(h, owner.name, LIVE_STREAMS, DOOR_LIVE);
    const fifteenthLive = await h.dial(owner.name, ipN(100), asClass(DOOR_LIVE));
    assert.equal(fifteenthLive.status, 429, 'the 15th live dial is refused');
    assert.equal(await fifteenthLive.text(), TOO_MANY_LIVE);

    assert.equal((await h.dial(owner.name, ipN(101), asClass(DOOR_MOVE))).status, 101, 'a move dial is accepted at 14 streams');
    assert.equal((await h.dial(owner.name, ipN(102), asClass(DOOR_MOVE))).status, 101, 'a second move dial is accepted at 15 streams');
    const seventeenth = await h.dial(owner.name, ipN(103), asClass(DOOR_MOVE));
    assert.equal(seventeenth.status, 429, 'nothing is accepted past the sixteen the machine carries');
    assert.equal((await h.dial(owner.name, ipN(104))).status, 429, 'an unclassified dial is refused at sixteen too');
  });
});

test('V21 a dial with no class, or a class nobody knows, behaves as every dial did before: sixteen, first come', async () => {
  const [owner] = await collidingKeys();
  for (const door of [undefined, '', 'bogus', 'LIVE', 'live,move']) {
    await withRoom({}, async (h) => {
      await h.register(owner);
      await dialN(h, owner.name, MAX_STREAMS, door);
      const refused = await h.dial(owner.name, ipN(100), asClass(door));
      assert.equal(refused.status, 429, `class ${JSON.stringify(door)}: the 17th dial is refused`);
      assert.match(await refused.text(), new RegExp(`already has ${MAX_STREAMS} connections open`));
    });
  }
});

test('V21 the reservation is measured against the total, so unclassified streams cannot crowd a move out of its two', async () => {
  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    await h.register(owner);
    await dialN(h, owner.name, LIVE_STREAMS); // fourteen older clients
    assert.equal((await h.dial(owner.name, ipN(100), asClass(DOOR_LIVE))).status, 429, 'a live dial is refused at a total of 14');
    assert.equal((await h.dial(owner.name, ipN(101), asClass(DOOR_MOVE))).status, 101, 'a move is not');
    assert.equal((await h.dial(owner.name, ipN(102))).status, 101, 'an unclassified dial is not, either');
    assert.equal((await h.dial(owner.name, ipN(103), asClass(DOOR_MOVE))).status, 429, 'the machine is full at 16');
  });
});

test('V21 a live stream that ends gives its slot back to the next live dial', async () => {
  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    await h.register(owner);
    const live = await dialN(h, owner.name, LIVE_STREAMS, DOOR_LIVE);
    assert.equal((await h.dial(owner.name, ipN(100), asClass(DOOR_LIVE))).status, 429);
    await h.room.webSocketClose(live[3], 1000);
    assert.equal((await h.dial(owner.name, ipN(101), asClass(DOOR_LIVE))).status, 101);
  });
});

test('the class is read only from the exact header values, and other dials carry no class on their socket', async () => {
  const request = (value) => new Request('https://relay.example/v1/dial/x', { headers: value === undefined ? {} : { [HEADER_DOOR]: value } });
  assert.equal(doorOf(request(DOOR_LIVE)), DOOR_LIVE);
  assert.equal(doorOf(request(DOOR_MOVE)), DOOR_MOVE);
  for (const odd of [undefined, '', 'Live', 'LIVE', 'door', 'live;move', 'live,move']) assert.equal(doorOf(request(odd)), '', JSON.stringify(odd));

  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    await h.register(owner);
    const [unclassified] = await dialN(h, owner.name, 1, undefined, 10);
    const [move] = await dialN(h, owner.name, 1, DOOR_MOVE, 20);
    const [live] = await dialN(h, owner.name, 1, DOOR_LIVE, 30);
    assert.deepEqual(Object.keys(unclassified.deserializeAttachment()).sort(), ['id', 'role']);
    assert.deepEqual(Object.keys(move.deserializeAttachment()).sort(), ['id', 'role']);
    assert.equal(live.deserializeAttachment().door, DOOR_LIVE);
  });
});

// ---------------------------------------------------------------------------------------------------------------
// The abuse breaker. It cuts a live stream that sustains more than BREAKER_BYTES_PER_SECOND across BREAKER_WINDOW_MS,
// and it is computed from the arrival of the message itself: no timer, no alarm.
// ---------------------------------------------------------------------------------------------------------------

const MiB = 1 << 20;
const payload = (n) => new Uint8Array(n);
const toMachine = (h, machine, id, n) => h.room.webSocketMessage(machine.server, frame(OP.data, id, payload(n)));
const closesFor = (machine, id) => machine.server.sent.filter((m) => m instanceof Uint8Array && m.length === 13 && m[0] === OP.close && readFrame(m)?.stream === String(id));

test('the breaker is a leaky bucket: a burst over the window, or a flow over the rate, trips it, and a flow under the rate never does', () => {
  const capacity = (BREAKER_BYTES_PER_SECOND * BREAKER_WINDOW_MS) / 1000;
  const burst = new Breaker();
  assert.equal(burst.charge(capacity, 0), false, 'exactly the window of allowance is not over');
  assert.equal(burst.charge(1, 0), true, 'one byte more is');

  const over = new Breaker();
  let at = 0;
  let tripped = null;
  // 8 MiB every second is twice the rate: it must trip, and not before the allowance has been spent.
  for (let second = 1; second <= 20 && tripped === null; second += 1) {
    at = second * 1000;
    if (over.charge(8 * MiB, at)) tripped = second;
  }
  assert.ok(tripped !== null && tripped >= 5 && tripped <= 6, `twice the rate tripped at second ${tripped}, want 5 or 6`);

  const under = new Breaker();
  for (let second = 1; second <= 3600; second += 1) {
    assert.equal(under.charge(3 * MiB, second * 1000), false, `3 MiB a second is under the rate (second ${second})`);
  }

  const rests = new Breaker();
  assert.equal(rests.charge(capacity, 0), false);
  assert.equal(rests.charge(capacity, BREAKER_WINDOW_MS), false, 'a stream that rested for the window starts again from empty');
  assert.equal(new Breaker().charge(capacity + 1, 0), true);
});

test('the breaker holds the Go side to the same rate and window', () => {
  assert.equal(BREAKER_BYTES_PER_SECOND, 4 * MiB);
  assert.equal(BREAKER_WINDOW_MS, 5000);
});

test('a live stream sending far faster than a correct host is cut with 1008 and the machine is told; a correct pace never is', async () => {
  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    const machine = await h.register(owner);
    const [paced, abusive] = await dialN(h, owner.name, 2, DOOR_LIVE);
    const [pacedId, abusiveId] = [paced, abusive].map((s) => s.deserializeAttachment().id);

    // One MiB a second for a minute: the pace a correct host holds. Nothing is cut.
    for (let second = 0; second < 60; second += 1) {
      h.clock.now += 1000;
      await h.room.webSocketMessage(paced, payload(MiB));
      machine.server.sent.length = 0; // the harness keeps every frame it is sent; this test does not need them
    }
    assert.equal(paced.closed, null, 'a stream at the correct pace is left alone');

    // 8 MiB every second is twice the rate. It is cut, with 1008, inside the window and after its allowance is spent.
    let cutAt = null;
    for (let second = 1; second <= 20 && cutAt === null; second += 1) {
      h.clock.now += 1000;
      await h.room.webSocketMessage(abusive, payload(8 * MiB));
      if (abusive.closed) cutAt = second;
    }
    assert.ok(cutAt >= 5 && cutAt <= 6, `cut at second ${cutAt}`);
    assert.deepEqual(abusive.closed, [1008, BREAKER_REASON]);
    assert.equal(abusive.deserializeAttachment().role, 'ended');
    assert.equal(closesFor(machine, abusiveId).length, 1, 'the machine is told the stream is over');
    assert.equal(closesFor(machine, pacedId).length, 0);
    assert.equal(h.room.breakers.size, 1, 'the cut stream left no counts behind; only the paced stream has its one');

    // The cut stream's own close arriving afterwards is not a second cut and sends nothing more.
    await h.room.webSocketClose(abusive, 1006);
    assert.equal(closesFor(machine, abusiveId).length, 1);
  });
});

test('the machine sending a live stream too fast is cut the same way, and the surface is told 1008', async () => {
  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    const machine = await h.register(owner);
    const [live] = await dialN(h, owner.name, 1, DOOR_LIVE);
    const id = live.deserializeAttachment().id;
    let cutAt = null;
    for (let second = 1; second <= 20 && cutAt === null; second += 1) {
      h.clock.now += 1000;
      // The carrier's own frame is at most MAX_PAYLOAD, so 8 MiB a second is many frames a second.
      for (let sent = 0; sent < 8 * MiB; sent += MAX_PAYLOAD) await toMachine(h, machine, id, MAX_PAYLOAD);
      if (live.closed) cutAt = second;
    }
    assert.ok(cutAt >= 5 && cutAt <= 6, `cut at second ${cutAt}`);
    assert.equal(live.closed[0], 1008);
    assert.equal(closesFor(machine, id).length, 1, 'the machine hears the stream closed');
  });
});

test('streams that did not claim the live class are never counted: a move may carry its large answers', async () => {
  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    const machine = await h.register(owner);
    const [move] = await dialN(h, owner.name, 1, DOOR_MOVE, 10);
    const [old] = await dialN(h, owner.name, 1, undefined, 20);
    for (const stream of [move, old]) {
      const id = stream.deserializeAttachment().id;
      for (let i = 0; i < 64; i += 1) {
        await toMachine(h, machine, id, MAX_PAYLOAD); // 8 MiB at once, in no time at all
        await h.room.webSocketMessage(stream, payload(MAX_PAYLOAD));
      }
      assert.equal(stream.closed, null);
    }
    assert.equal(h.room.breakers.size, 0, 'nothing was counted for a stream with no live class');
  });
});

// ---------------------------------------------------------------------------------------------------------------
// A live session costs the object nothing it did not cost before (contract V5 and V15, the Worker's half): no new stored
// object, no timer and no alarm, so an idle live view lets the object hibernate.
// ---------------------------------------------------------------------------------------------------------------

test('V5 V15 a whole live session stores only the stream counter, and schedules no timer and no alarm', async () => {
  const [owner] = await collidingKeys();
  const real = { setTimeout: globalThis.setTimeout, setInterval: globalThis.setInterval };
  const fromRoom = [];
  const spy = (kind) => (...args) => {
    // Only a timer the room's own code asked for counts: the test runner has timers of its own.
    if (new Error().stack.includes('/src/engine/')) fromRoom.push(kind);
    return real[kind](...args);
  };
  await withRoom({}, async (h) => {
    const machine = await h.register(owner);
    globalThis.setTimeout = spy('setTimeout');
    globalThis.setInterval = spy('setInterval');
    try {
      const putsBefore = h.puts.length;
      const [viewer] = await dialN(h, owner.name, 1, DOOR_LIVE);
      const id = viewer.deserializeAttachment().id;
      for (let i = 0; i < 100; i += 1) {
        h.clock.now += 40;
        await h.room.webSocketMessage(viewer, payload(512));
        await toMachine(h, machine, id, 512);
      }
      await h.room.webSocketClose(viewer, 1000);
      assert.deepEqual(h.puts.slice(putsBefore), ['next'], 'the one write is the stream-id counter, once per accepted dial');
    } finally {
      globalThis.setTimeout = real.setTimeout;
      globalThis.setInterval = real.setInterval;
    }
    assert.deepEqual(fromRoom, [], 'the object scheduled no timer while a live stream was dialled, carried and closed');
    assert.deepEqual(h.alarms, [], 'the object asked for no alarm');
  });
});

// ---------------------------------------------------------------------------------------------------------------
// Retry-After on both rate limits' refusals, so a client waits for its window and not for a guess.
// ---------------------------------------------------------------------------------------------------------------

test('the room tells a refused caller how long is left in its minute', async () => {
  const [owner] = await collidingKeys();
  await withRoom({}, async (h) => {
    await h.register(owner);
    // The clock starts 40 s into a minute, so 20 s of it are left.
    assert.equal(secondsToNextMinute(h.clock.now), 20);
    for (let i = 0; i < DIALS_PER_MINUTE; i += 1) await h.dial(owner.name, '198.51.100.7');
    const refused = await h.dial(owner.name, '198.51.100.7');
    assert.equal(refused.status, 429);
    assert.equal(await refused.text(), TOO_MANY_DIALS);
    assert.equal(refused.headers.get('retry-after'), '20');

    h.clock.now += 19_500;
    const later = await h.dial(owner.name, '198.51.100.7');
    assert.equal(later.status, 429);
    assert.equal(later.headers.get('retry-after'), '1', 'never less than a second, so a client is not told to retry at once');

    h.clock.now += 1000;
    const rolled = await h.dial(owner.name, '198.51.100.7');
    assert.notEqual(await rolled.text(), TOO_MANY_DIALS, 'the minute rolled over, and the caller is not refused for its rate');
    assert.equal(rolled.headers.get('retry-after'), null);

    for (let i = 0; i < REGISTRATIONS_PER_MINUTE; i += 1) await h.room.fetch(new Request('https://relay.example' + ENGINE_PATH, {
      headers: { upgrade: 'websocket', 'sec-websocket-protocol': PROTOCOL, 'cf-connecting-ip': '198.51.100.8', [HEADER_NAME]: 'amber-anchor-10' },
    }));
    const refusedRegistration = await h.room.fetch(new Request('https://relay.example' + ENGINE_PATH, {
      headers: { upgrade: 'websocket', 'sec-websocket-protocol': PROTOCOL, 'cf-connecting-ip': '198.51.100.8', [HEADER_NAME]: 'amber-anchor-10' },
    }));
    assert.equal(refusedRegistration.status, 429);
    assert.equal(await refusedRegistration.text(), TOO_MANY_REGISTRATIONS);
    assert.match(refusedRegistration.headers.get('retry-after'), /^\d+$/);
  });
});

test('the front door tells a refused caller when to come back, on both its paths', async () => {
  const reached = [];
  const machine = { idFromName: (n) => n, get: (id) => ({ async fetch() { reached.push(id); return new Response('no machine is connected under that name', { status: 404 }); } }) };
  const ws = { upgrade: 'websocket', 'sec-websocket-protocol': PROTOCOL };

  // The Cloudflare binding does not say where in its window a caller is, so a refusal it gives is told the whole period.
  const refusing = { async limit() { return { success: false }; } };
  const bound = await serveMachineDoor(
    new Request('https://relay.example/v1/dial/acorn-alder-10', { headers: { ...ws, 'cf-connecting-ip': '198.51.100.1' } }),
    { MACHINE: machine, MACHINE_DIAL_LIMIT: refusing, MACHINE_REGISTER_LIMIT: refusing },
  );
  assert.equal(bound.status, 429);
  assert.equal(bound.headers.get('retry-after'), '60');
  const boundRegistration = await serveMachineDoor(
    new Request('https://relay.example' + ENGINE_PATH, { headers: { ...ws, 'cf-connecting-ip': '198.51.100.1', [HEADER_NAME]: 'acorn-alder-10' } }),
    { MACHINE: machine, MACHINE_DIAL_LIMIT: refusing, MACHINE_REGISTER_LIMIT: refusing },
  );
  assert.equal(boundRegistration.status, 429);
  assert.equal(boundRegistration.headers.get('retry-after'), '60');

  // With no binding the Worker counts in memory, knows its minute, and says exactly how much of it is left.
  const ip = '203.0.113.' + (Date.now() % 200);
  let last;
  for (let i = 0; i <= DIALS_PER_MINUTE; i += 1) {
    last = await serveMachineDoor(new Request('https://relay.example/v1/dial/acorn-alder-10', { headers: { ...ws, 'cf-connecting-ip': ip } }), { MACHINE: machine });
  }
  assert.equal(last.status, 429);
  const wait = Number(last.headers.get('retry-after'));
  assert.ok(Number.isInteger(wait) && wait >= 1 && wait <= 60, `retry-after ${wait}`);
  assert.equal(reached.length, DIALS_PER_MINUTE, 'the refused dial woke no object');
});

// ---------------------------------------------------------------------------------------------------------------
// One source of truth: the Worker's numbers are the Go relay's, under the same names (internal/relay/relay.go).
// ---------------------------------------------------------------------------------------------------------------

test('the numbers the Worker enforces are the Go relay\'s: live streams, the class names and the breaker', () => {
  const go = readFileSync(new URL('../../../internal/relay/relay.go', import.meta.url), 'utf8');
  const number = (name) => {
    const found = go.match(new RegExp(`^\\s*(?:const\\s+)?${name}\\s*=\\s*(.+?)\\s*(?://.*)?$`, 'm'));
    assert.ok(found, `relay.go has no ${name}`);
    return found[1];
  };
  assert.equal(Number(number('MaxStreams')), MAX_STREAMS);
  assert.equal(Number(number('LiveStreams')), LIVE_STREAMS);
  assert.equal(number('DoorLive'), JSON.stringify(DOOR_LIVE));
  assert.equal(number('DoorMove'), JSON.stringify(DOOR_MOVE));
  assert.equal(number('HeaderDoor').toLowerCase(), JSON.stringify(HEADER_DOOR));
  assert.equal(number('BreakerRate'), '4 << 20');
  assert.equal(number('BreakerWindow'), '5 * time.Second');
  assert.equal(BREAKER_BYTES_PER_SECOND, 4 << 20);
  assert.equal(BREAKER_WINDOW_MS, 5 * 1000);
  assert.equal(MAX_STREAMS - LIVE_STREAMS, 2, 'two streams are kept for a move');
});
