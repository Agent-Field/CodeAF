// The machine doors: `codeaf serve` registers at /v1/engine and a surface dials it at /v1/dial/<name>. A port of
// internal/relay's server (server.go, handshake.go, mux.go) onto one Durable Object per machine name.
//
// THE RELAY READS NO PAYLOAD. What crosses here is the registration proof and the carrier's frames; a frame's
// payload is the tunnel's ciphertext, moved between two sockets and never decoded.
//
// EVERY SOCKET HIBERNATES. Each frame is one WebSocket message (the Go client writes a frame in one write), so the
// object keeps no half-read bytes across a wake-up, and the routing lives in socket tags and attachments, which
// survive hibernation. A quiet machine therefore costs nothing: its liveness is proved by WebSocket pings from the
// machine, which the platform answers without waking the object, and the object sets no timer.
import { DurableObject } from 'cloudflare:workers';
import { nameFor } from './names.js';
import { MAX_PAYLOAD, MAX_STREAMS, OP, PROTOCOL, HEADER_NAME, HEADER_KEY, challenge, frame, readFrame, verdict } from './carrier.js';

/** How long a registration waits for an older socket under the same name to prove it is still there. */
const PROBE_MS = 5000;

const ENGINE = 'engine';
const DIAL = 'dial';

const sameBytes = (a, b) => a.length === b.length && a.every((x, i) => x === b[i]);
const b64 = (bytes) => btoa(String.fromCharCode(...bytes));
const unb64 = (text) => Uint8Array.from(atob(text), (c) => c.charCodeAt(0));
const bytesOf = (message) => (typeof message === 'string' ? new TextEncoder().encode(message) : new Uint8Array(message));
const refuse = (status, sentence) => new Response(sentence, { status, headers: { 'content-type': 'text/plain; charset=utf-8' } });

export class MachineDO extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    /** Registrations waiting for an older socket's pong, which is in-memory because the object is awake while they wait. */
    this.probes = new Set();
  }

  async fetch(request) {
    const path = new URL(request.url).pathname;
    return path === '/v1/engine' ? this.#register(request) : this.#dial();
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
    server.serializeAttachment({ role: 'pending', want: b64(want), name });
    server.send(sent);
    return new Response(null, { status: 101, webSocket: client, headers: { 'sec-websocket-protocol': PROTOCOL } });
  }

  /** dial matches a surface to the machine and opens a stream on its carrier. */
  async #dial() {
    const engine = this.#engine();
    if (!engine) return refuse(404, 'no machine is connected under that name');
    if (this.ctx.getWebSockets(DIAL).length >= MAX_STREAMS) {
      return refuse(503, `that machine already has ${MAX_STREAMS} connections open, which is as many as the relay carries at once`);
    }
    const id = String((this.ctx.storage.kv.get('next') ?? 0) + 1);
    this.ctx.storage.kv.put('next', Number(id));
    const { 0: client, 1: server } = new WebSocketPair();
    this.ctx.acceptWebSocket(server, [DIAL, 's:' + id]);
    server.serializeAttachment({ role: 'surface', id });
    engine.send(frame(OP.open, id));
    return new Response(null, { status: 101, webSocket: client, headers: { 'sec-websocket-protocol': PROTOCOL } });
  }

  /** engine is the registered machine's socket, or null. */
  #engine(except) {
    return this.ctx.getWebSockets(ENGINE).find((ws) => ws !== except && ws.deserializeAttachment()?.role === 'engine') ?? null;
  }

  async webSocketMessage(ws, message) {
    const seen = ws.deserializeAttachment();
    const bytes = bytesOf(message);
    if (seen.role === 'surface') return this.#fromSurface(seen.id, bytes);
    if (seen.role === 'pending') return this.#proof(ws, seen, bytes);
    if (seen.role === 'engine') return this.#fromMachine(ws, bytes);
  }

  /** fromSurface carries a surface's bytes to the machine, cut into carrier-sized frames. */
  #fromSurface(id, bytes) {
    const engine = this.#engine();
    if (!engine) return this.#closeStream(id, 1011, 'the machine left');
    for (let at = 0; at < bytes.length; at += MAX_PAYLOAD) engine.send(frame(OP.data, id, bytes.subarray(at, at + MAX_PAYLOAD)));
  }

  /** fromMachine routes one carrier frame from the machine. */
  #fromMachine(ws, bytes) {
    const got = readFrame(bytes);
    if (!got) return this.#endEngine(ws, 1002, 'that frame is not one carrier frame');
    if (got.op === OP.pong) return this.probes.forEach((probe) => probe(ws));
    if (got.op === OP.ping) return ws.send(frame(OP.pong, got.stream));
    const surface = this.ctx.getWebSockets('s:' + got.stream)[0];
    if (!surface) return;
    if (got.op === OP.data) surface.send(got.payload);
    if (got.op === OP.close) surface.close(1000, 'closed');
  }

  /** proof checks the machine's answer and, rule three, never lets a live registration be taken over. */
  async #proof(ws, seen, bytes) {
    if (!sameBytes(bytes, unb64(seen.want))) {
      return this.#refuseRegistration(ws, 'that machine did not prove it holds the key its name is derived from');
    }
    const older = this.#engine(ws);
    if (older && (await this.#answers(older))) {
      return this.#refuseRegistration(ws, 'that machine is already connected to the relay under this name');
    }
    if (older) this.#endEngine(older, 1001, 'replaced by a newer registration after it stopped answering');
    ws.serializeAttachment({ role: 'engine', name: seen.name });
    ws.send(verdict());
  }

  /**
   * answers pings an older socket under this name and reports whether it pongs. A machine whose network dropped can
   * leave a socket the platform has not yet closed; a registration that holds the key replaces only a silent one.
   */
  #answers(older) {
    return new Promise((resolve) => {
      const probe = (from) => from === older && done(true);
      const done = (alive) => {
        this.probes.delete(probe);
        clearTimeout(timer);
        resolve(alive);
      };
      const timer = setTimeout(() => done(false), PROBE_MS);
      this.probes.add(probe);
      try {
        older.send(frame(OP.ping, 0));
      } catch {
        done(false);
      }
    });
  }

  #refuseRegistration(ws, reason) {
    try {
      ws.send(verdict(reason));
      ws.close(1008, 'registration refused');
    } catch {}
  }

  async webSocketClose(ws, code) {
    this.#gone(ws);
    try {
      ws.close(code === 1005 || code === 1006 ? 1000 : code, 'closed');
    } catch {}
  }

  async webSocketError(ws) {
    this.#gone(ws);
  }

  /** gone tells the other end of whatever the socket that ended was carrying. */
  #gone(ws) {
    const seen = ws.deserializeAttachment();
    if (seen?.role === 'surface') {
      try {
        this.#engine()?.send(frame(OP.close, seen.id));
      } catch {}
    } else if (seen?.role === 'engine') {
      ws.serializeAttachment({ ...seen, role: 'ended' });
      for (const surface of this.ctx.getWebSockets(DIAL)) this.#closeStream(surface.deserializeAttachment().id, 1001, 'the machine left');
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
