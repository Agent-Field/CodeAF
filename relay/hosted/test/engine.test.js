import { test } from 'node:test';
import assert from 'node:assert/strict';
import { nameFor, validName } from '../src/engine/names.js';
import {
  DIALS_PER_MINUTE, ENGINE_PATH, HEADER_KEY, HEADER_NAME, MAX_PAYLOAD, MAX_STREAMS, OP, PROTOCOL,
  REGISTRATIONS_PER_MINUTE, TOO_MANY_DIALS, TOO_MANY_REGISTRATIONS,
  frame, readFrame, registrationProof, verdict,
} from '../src/engine/carrier.js';
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
    storage: { kv: { get: (k) => kv.get(k), put: (k, v) => kv.set(k, v) } },
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

  async function dial(name, ip = '198.51.100.2') {
    const req = new Request('https://relay.example/v1/dial/' + name, {
      headers: {
        upgrade: 'websocket',
        'sec-websocket-protocol': PROTOCOL,
        'cf-connecting-ip': ip,
      },
    });
    return room.fetch(req);
  }

  return { room, ctx, sockets, clock, register, dial, restore };
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
