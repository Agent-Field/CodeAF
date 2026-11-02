// One machine name's room: `codeaf serve` registers at /v1/engine and a surface dials it at /v1/dial/<name>. Kept free
// of the `cloudflare:workers` base class so Node unit tests can exercise the real socket state machine directly, while
// MachineDO in machine.js wraps it as a Durable Object.
//
// THE RELAY READS NO PAYLOAD. What crosses here is the registration proof and the carrier's frames; a frame's
// payload is the tunnel's ciphertext, moved between two sockets and never decoded.
//
// EVERY SOCKET HIBERNATES. Each frame is one WebSocket message (the Go client writes a frame in one write), so the
// object keeps no half-read bytes across a wake-up, and the routing lives in socket tags and attachments, which
// survive hibernation. A quiet machine therefore costs nothing: its liveness is proved by WebSocket pings from the
// machine, which the platform answers without waking the object. The one timer is a registration's bounded wait for
// an older socket's pong under the same key, while the object is awake anyway.
import { nameFor } from './names.js';
import { ipOf } from '../wire.js';
import {
  DIALS_PER_MINUTE, ENGINE_PATH, HANDSHAKE_MS, HEADER_KEY, HEADER_NAME, MAX_PAYLOAD, MAX_STREAMS, OP, PROBE_MS,
  PROTOCOL, REGISTRATIONS_PER_MINUTE, TOO_MANY_DIALS, TOO_MANY_REGISTRATIONS,
  challenge, frame, plain as refuse, readFrame, verdict,
} from './carrier.js';

const ENGINE = 'engine';
const DIAL = 'dial';

function sameBytes(a, b) {
  if (a.length !== b.length) return false;
  let diff = 0;
  for (let i = 0; i < a.length; i += 1) diff |= a[i] ^ b[i];
  return diff === 0;
}

const b64 = (bytes) => btoa(String.fromCharCode(...bytes));
const unb64 = (text) => Uint8Array.from(atob(text), (c) => c.charCodeAt(0));
const bytesOf = (message) => (typeof message === 'string' ? new TextEncoder().encode(message) : new Uint8Array(message));

export class MachineRoom {
  constructor(ctx, env, options = {}) {
    this.ctx = ctx;
    this.env = env;
    this.probeMs = options.probeMs ?? PROBE_MS;
    this.handshakeMs = options.handshakeMs ?? HANDSHAKE_MS;
    this.clock = options.clock ?? Date.now;
    /** Registrations waiting for an older socket's pong, kept in memory because the object is awake while they wait. */
    this.probes = new Set();
    /** claiming is a registration between its proof and its verdict; a second one meanwhile is refused. */
    this.claiming = false;
    /** buckets are this minute's counts per caller network and door, kept as a second line behind door.js. */
    this.buckets = new Map();
  }

  async fetch(request) {
    this.#sweepPending();
    const register = new URL(request.url).pathname === ENGINE_PATH;
    if (!this.#allow(ipOf(request, this.env), register ? 'register' : 'dial', register ? REGISTRATIONS_PER_MINUTE : DIALS_PER_MINUTE)) {
      return refuse(429, register ? TOO_MANY_REGISTRATIONS : TOO_MANY_DIALS);
    }
    return register ? this.#register(request) : this.#dial();
  }

  /** allow spends one of a caller's per-minute allowance at this machine's door. */
  #allow(ip, door, perMinute) {
    const minute = Math.floor(this.clock() / 60000);
    for (const [key, bucket] of this.buckets) if (bucket.minute !== minute) this.buckets.delete(key);
    const key = door + ' ' + ip;
    const bucket = this.buckets.get(key) ?? { minute, count: 0 };
    bucket.count += 1;
    this.buckets.set(key, bucket);
    return bucket.count <= perMinute;
  }

  /**
   * sweepPending evicts /v1/engine upgrades that never sent their possession proof within HANDSHAKE_MS. Running on
   * arrivals instead of a resting timer keeps idle registered machines hibernating at zero duration cost.
   */
  #sweepPending() {
    const now = this.clock();
    for (const ws of this.ctx.getWebSockets(ENGINE)) {
      const seen = ws.deserializeAttachment();
      if (seen?.role === 'pending' && typeof seen.at === 'number' && now - seen.at >= this.handshakeMs) {
        ws.serializeAttachment({ ...seen, role: 'ended' });
        try {
          ws.close(1008, 'registration timed out');
        } catch {}
      }
    }
  }

  /** register accepts a machine's socket and sends the challenge; the proof arrives as its first message. */
  async #register(request) {
    const name = request.headers.get(HEADER_NAME) ?? '';
    let key;
    try {
      key = unb64((request.headers.get(HEADER_KEY) ?? '').replace(/-/g, '+').replace(/_/g, '/'));
    } catch {
      return refuse(400, 'that key is not readable');
    }
    if (key.length !== 32) return refuse(400, `a machine key is 32 bytes and that one is ${key.length}`);
    // RULE ONE: the name must be the one this key derives, recomputed rather than trusted.
    if ((await nameFor(key)) !== name) return refuse(400, 'that name does not belong to that key');
    let sent, want;
    try {
      ({ sent, want } = await challenge(name, key));
    } catch {
      return refuse(400, 'that key is not a usable machine key');
    }
    const { 0: client, 1: server } = new WebSocketPair();
    this.ctx.acceptWebSocket(server, [ENGINE]);
    server.serializeAttachment({ role: 'pending', want: b64(want), key: b64(key), name, at: this.clock() });
    try {
      server.send(sent);
    } catch {
      server.serializeAttachment({ role: 'ended', name });
      try {
        server.close(1011, 'closed');
      } catch {}
      return refuse(503, 'the registration closed before it began');
    }
    return new Response(null, { status: 101, webSocket: client, headers: { 'sec-websocket-protocol': PROTOCOL } });
  }

  /** dial matches a surface to the machine and opens a stream on its carrier. */
  async #dial() {
    const engine = this.#engine();
    if (!engine) return refuse(404, 'no machine is connected under that name');
    const active = this.ctx.getWebSockets(DIAL).filter((ws) => ws.deserializeAttachment()?.role === 'surface');
    if (active.length >= MAX_STREAMS) {
      return refuse(429, `that machine already has ${MAX_STREAMS} connections open, which is as many as the relay carries at once`);
    }
    const id = String((this.ctx.storage.kv.get('next') ?? 0) + 1);
    this.ctx.storage.kv.put('next', Number(id));
    // Tell the machine before accepting the surface socket into ctx: if engine has already begun closing, throwing
    // here must not leave an orphaned DIAL socket occupying one of MAX_STREAMS forever.
    try {
      engine.send(frame(OP.open, id));
    } catch {
      this.#endEngine(engine, 1011, 'the machine left');
      return refuse(404, 'no machine is connected under that name');
    }
    const { 0: client, 1: server } = new WebSocketPair();
    this.ctx.acceptWebSocket(server, [DIAL, 's:' + id]);
    server.serializeAttachment({ role: 'surface', id });
    return new Response(null, { status: 101, webSocket: client, headers: { 'sec-websocket-protocol': PROTOCOL } });
  }

  /** engine is the registered machine's socket, or null. */
  #engine(except) {
    return this.ctx.getWebSockets(ENGINE).find((ws) => ws !== except && ws.deserializeAttachment()?.role === 'engine') ?? null;
  }

  async webSocketMessage(ws, message) {
    const seen = ws.deserializeAttachment();
    if (!seen || seen.role === 'ended') return;
    const bytes = bytesOf(message);
    if (seen.role === 'surface') return this.#fromSurface(seen.id, bytes);
    if (seen.role === 'pending') return this.#proof(ws, seen, bytes);
    if (seen.role === 'engine') return this.#fromMachine(ws, bytes);
  }

  /** fromSurface carries a surface's bytes to the machine, cut into carrier-sized frames. */
  #fromSurface(id, bytes) {
    const engine = this.#engine();
    if (!engine) return this.#closeStream(id, 1011, 'the machine left');
    try {
      for (let at = 0; at < bytes.length; at += MAX_PAYLOAD) {
        engine.send(frame(OP.data, id, bytes.subarray(at, at + MAX_PAYLOAD)));
      }
    } catch {
      this.#endEngine(engine, 1011, 'the machine left');
    }
  }

  /** fromMachine routes one carrier frame from the machine. */
  #fromMachine(ws, bytes) {
    const got = readFrame(bytes);
    if (!got) return this.#endEngine(ws, 1002, 'that frame is not one carrier frame');
    if (got.op === OP.pong) return this.probes.forEach((probe) => probe(ws));
    if (got.op === OP.ping) {
      try {
        ws.send(frame(OP.pong, got.stream));
      } catch {
        this.#endEngine(ws, 1011, 'closed');
      }
      return;
    }
    const surface = this.ctx.getWebSockets('s:' + got.stream).find((s) => s.deserializeAttachment()?.role === 'surface');
    if (!surface) return;
    if (got.op === OP.data) {
      try {
        surface.send(got.payload);
      } catch {
        this.#gone(surface);
        try {
          surface.close(1011, 'closed');
        } catch {}
      }
    }
    if (got.op === OP.close) this.#closeStream(got.stream, 1000, 'closed');
  }

  /**
   * proof checks the machine's answer and, rule three, never lets a live registration be taken over. Only the SAME
   * public key may probe and replace an older socket that went silent; a colliding key is refused whether or not the
   * current socket answers within PROBE_MS.
   */
  async #proof(ws, seen, bytes) {
    this.#sweepPending();
    if (ws.deserializeAttachment()?.role !== 'pending') return;
    let want;
    try {
      want = unb64(seen.want);
    } catch {
      return this.#refuseRegistration(ws, 'that machine did not prove it holds the key its name is derived from');
    }
    if (!sameBytes(bytes, want)) {
      return this.#refuseRegistration(ws, 'that machine did not prove it holds the key its name is derived from');
    }
    const older = this.#engine(ws);
    if (this.claiming) {
      return this.#refuseRegistration(ws, 'that machine is already connected to the relay under this name');
    }
    if (older) {
      const olderKey = older.deserializeAttachment()?.key;
      if (!olderKey || !seen.key || !sameBytes(unb64(olderKey), unb64(seen.key))) {
        return this.#refuseRegistration(ws, 'that machine is already connected to the relay under this name');
      }
    }
    this.claiming = true;
    try {
      if (older && (await this.#answers(older))) {
        return this.#refuseRegistration(ws, 'that machine is already connected to the relay under this name');
      }
      // If the new registrant disconnected while we waited on older's pong, neither evict older nor promote ws.
      if (ws.deserializeAttachment()?.role !== 'pending') return;
      try {
        ws.send(verdict());
      } catch {
        ws.serializeAttachment({ ...seen, role: 'ended' });
        try {
          ws.close(1011, 'closed');
        } catch {}
        return;
      }
      if (older) this.#endEngine(older, 1001, 'replaced by a newer registration after it stopped answering');
      ws.serializeAttachment({ role: 'engine', name: seen.name, key: seen.key });
    } finally {
      this.claiming = false;
    }
  }

  /**
   * answers pings an older socket under the same key and reports whether it pongs. A machine whose network dropped can
   * leave a socket the platform has not yet closed; a registration that holds the same key replaces only a silent one.
   */
  #answers(older) {
    return new Promise((resolve) => {
      const probe = (from) => from === older && done(true);
      const done = (alive) => {
        this.probes.delete(probe);
        clearTimeout(timer);
        resolve(alive);
      };
      const timer = setTimeout(() => done(false), this.probeMs);
      this.probes.add(probe);
      try {
        older.send(frame(OP.ping, 0));
      } catch {
        done(false);
      }
    });
  }

  #refuseRegistration(ws, reason) {
    const seen = ws.deserializeAttachment();
    if (seen) ws.serializeAttachment({ ...seen, role: 'ended' });
    try {
      ws.send(verdict(reason));
      ws.close(1008, 'registration refused');
    } catch {}
  }

  async webSocketClose(ws, code) {
    this.#gone(ws);
    try {
      ws.close(code === 1005 || code === 1006 || code === 1015 ? 1000 : code, 'closed');
    } catch {}
  }

  async webSocketError(ws) {
    this.#gone(ws);
    try {
      ws.close(1011, 'closed');
    } catch {}
  }

  /** gone tells the other end of whatever the socket that ended was carrying, once. */
  #gone(ws) {
    const seen = ws.deserializeAttachment();
    if (!seen || seen.role === 'ended') return;
    ws.serializeAttachment({ ...seen, role: 'ended' });
    if (seen.role === 'surface') {
      try {
        this.#engine()?.send(frame(OP.close, seen.id));
      } catch {}
    } else if (seen.role === 'engine') {
      for (const surface of this.ctx.getWebSockets(DIAL)) {
        const id = surface.deserializeAttachment()?.id;
        if (id) this.#closeStream(id, 1001, 'the machine left');
      }
    }
  }

  #endEngine(ws, code, reason) {
    this.#gone(ws);
    try {
      ws.close(code, reason);
    } catch {}
  }

  #closeStream(id, code, reason) {
    for (const surface of this.ctx.getWebSockets('s:' + id)) {
      surface.serializeAttachment({ role: 'ended', id });
      try {
        surface.close(code, reason);
      } catch {}
    }
  }
}
