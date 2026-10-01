// The directory watch socket of one identity (contract 21). It is the server half of a push that costs
// nothing while idle: the object accepts sockets with the hibernation API, so a socket that is open and
// quiet holds no memory and bills no time, and it sets no timer and opens no connection for any socket.
// A socket carries one number, the directory's version, and never a record. The list of sockets is the
// platform's own (ctx.getWebSockets survives hibernation), so the object keeps none of its own.
import { Liveness } from './liveness.js';
import { vouchedUntil } from './vouch.js';
import { Wire } from './wire.js';

/** The close codes a client acts on: a revoked device stops for good; a rotation stops the watching. */
export const CLOSE_REVOKED = 4401;
export const CLOSE_ROTATED = 4410;

/** The answer header that tells a client this relay counts its socket as proof of life (contract 21.11.1). */
const VOUCH_HEADER = 'Codeaf-Vouch';

const attachmentOf = (ws) => ws.deserializeAttachment() ?? { at: 0, holds: [], events: false };

function signOfLife(ctx, ws) {
  const { at, holds } = attachmentOf(ws);
  return { holds, seenAt: Math.max(at, ctx.getWebSocketAutoResponseTimestamp(ws)?.getTime() ?? 0) };
}

const PENDING = 'offline:';

/** KvPending is the devices owed an offline frame, as a map kept in the object's own storage (it must outlive hibernation). */
class KvPending {
  constructor(storage) {
    this.storage = storage;
  }

  get kv() {
    return this.storage.kv;
  }

  has = (device) => this.kv.get(PENDING + device) !== undefined;
  set = (device, due) => this.kv.put(PENDING + device, due);
  delete = (device) => this.kv.delete(PENDING + device);
  keys = () => [...this.kv.list({ prefix: PENDING })].map(([k]) => k.slice(PENDING.length));
  entries = () => [...this.kv.list({ prefix: PENDING })].map(([k, due]) => [k.slice(PENDING.length), due]);
}

const frame = (version) => `{"v":${version}}`;

/** NOBODY hears nothing: what a directory uses when no socket can ever be open (tests, the pure rules). */
export const NOBODY = { broadcast() {}, closeDevice() {}, closeAll() {}, vouchedUntil: () => 0, announce() {} };

export class Watchers {
  constructor(ctx, cap, ttlMs, arm = () => {}) {
    this.ctx = ctx;
    this.cap = cap;
    this.ttlMs = ttlMs;
    this.liveness = new Liveness({
      peers: () => this.#peers(),
      pending: new KvPending(ctx.storage),
      arm,
      clock: Date.now,
    });
    // The platform answers the client's keepalive itself, without running this object, so it never wakes.
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('ping', 'pong'));
  }

  /**
   * accept opens a socket for `device`, tells it `version` at once, and answers the 101 that completes the upgrade.
   * The holds the socket names and the time it was accepted travel in its attachment, which the platform keeps
   * with the socket through hibernation, so vouching for a lease costs no storage write (contract 21.11).
   * A socket opened with `events` also hears the event frames (presence, joined, revoked) and, at once, who is online.
   */
  accept(device, version, holds, now, events = false) {
    if (this.ctx.getWebSockets().length >= this.cap) throw new Wire('too_many_watchers', 429);
    const { 0: client, 1: server } = new WebSocketPair();
    this.ctx.acceptWebSocket(server, [device]);
    server.serializeAttachment({ at: now, holds, events });
    server.send(frame(version));
    if (events) for (const text of this.liveness.snapshot(device)) server.send(text);
    this.liveness.opened(device);
    return new Response(null, { status: 101, webSocket: client, headers: { [VOUCH_HEADER]: '1' } });
  }

  /**
   * vouchedUntil is how long the sockets of `device` vouch for its lease of `cell` at `fence` (0: not at all). A
   * socket's last sign of life is its last auto-response, which the platform records without waking us, or the
   * time it was accepted if it never pinged.
   */
  vouchedUntil(device, cell, fence) {
    const sockets = this.ctx.getWebSockets(device).map((ws) => signOfLife(this.ctx, ws));
    return vouchedUntil(sockets, cell, fence, this.ttlMs);
  }

  /** broadcast tells every socket of the identity the new version. */
  broadcast(version) {
    const text = frame(version);
    for (const ws of this.ctx.getWebSockets()) attempt(() => ws.send(text));
  }

  #peers() {
    return this.ctx.getWebSockets().map((ws) => ({
      ws,
      device: this.ctx.getTags(ws)[0],
      events: attachmentOf(ws).events === true,
      send: (text) => attempt(() => ws.send(text)),
    }));
  }

  /** left runs when a socket has closed. It answers the device that now holds no socket, or undefined. */
  left(ws) {
    const device = this.ctx.getTags(ws)[0];
    return device !== undefined && this.liveness.closed({ ws, device }) ? device : undefined;
  }

  /** expire sends the offline frames that are due; the object's alarm calls it. */
  expire() {
    this.liveness.expire();
  }

  /** online is the set of devices that hold a watch socket now. */
  online() {
    return this.liveness.online();
  }

  /** announce sends an event frame (contract 5) to every event socket except the sockets of `except`. */
  announce(frame, except) {
    this.liveness.tell(frame, except);
  }

  closeDevice(device, code, reason) {
    for (const ws of this.ctx.getWebSockets(device)) attempt(() => ws.close(code, reason));
  }

  closeAll(code, reason) {
    for (const ws of this.ctx.getWebSockets()) attempt(() => ws.close(code, reason));
  }
}

/** finishClose answers a client's close with a close of ours, so the handshake completes instead of the client timing out as if the link had dropped. */
export function finishClose(ws, code) {
  attempt(() => ws.close(RESERVED_CLOSES.has(code) ? 1000 : code, 'closed'));
}

// Codes a peer reports but may never send on the wire (RFC 6455, 7.4.1).
const RESERVED_CLOSES = new Set([1005, 1006, 1015]);

// A socket that is already closing throws on send or close; the platform forgets it, so that is no failure.
function attempt(fn) {
  try {
    fn();
  } catch {
    /* already closing */
  }
}
