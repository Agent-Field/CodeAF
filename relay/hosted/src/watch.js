// The directory watch socket of one identity (contract 21). It is the server half of a push that costs
// nothing while idle: the object accepts sockets with the hibernation API, so a socket that is open and
// quiet holds no memory and bills no time, and it sets no timer and opens no connection for any socket.
// A socket carries one number, the directory's version, and never a record. The list of sockets is the
// platform's own (ctx.getWebSockets survives hibernation), so the object keeps none of its own.
import { Liveness } from './liveness.js';
import { PRESENCE, windowMs } from './limits.js';
import { vouchedUntil } from './vouch.js';
import { Wire } from './wire.js';

/** The close codes a client acts on: a revoked device stops for good; a rotation stops the watching. */
export const CLOSE_REVOKED = 4401;
export const CLOSE_ROTATED = 4410;
/** A socket that stopped answering is closed with this code; a client that reads it redials at once (contract 21.12.4). */
export const CLOSE_SILENT = 4408;

/** The answer headers that tell a client this relay counts its socket as proof of life, and judges presence by it (contract 21.11.1, 21.12.3). */
const ANSWER_HEADERS = { 'Codeaf-Vouch': '1', 'Codeaf-Presence': '1' };

/** What a socket carries through hibernation; an attachment from before a field existed reads as its default. */
const FRESH = { at: 0, holds: [], events: false, beat: PRESENCE.defaultBeat, lapsed: false };
const attachmentOf = (ws) => ({ ...FRESH, ...ws.deserializeAttachment() });

/**
 * signOfLife is the later of the time the socket was accepted and its last auto-response, which the platform records
 * without waking us. A socket whose timestamp cannot be read (one that is closing) counts as heard from now, so a
 * close the platform reports is never mistaken for silence.
 */
function signOfLife(ctx, ws, at, now) {
  try {
    return Math.max(at, ctx.getWebSocketAutoResponseTimestamp(ws)?.getTime() ?? 0);
  } catch {
    return now;
  }
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
  constructor(ctx, cap, ttlMs, arm = () => {}, seen = () => {}) {
    this.ctx = ctx;
    this.cap = cap;
    this.ttlMs = ttlMs;
    this.liveness = new Liveness({
      peers: () => this.#peers(),
      pending: new KvPending(ctx.storage),
      arm,
      clock: Date.now,
      seen,
    });
    // The platform answers the client's keepalive itself, without running this object, so it never wakes.
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('ping', 'pong'));
  }

  /**
   * accept opens a socket for `device`, tells it `version` at once, and answers the 101 that completes the upgrade.
   * The holds the socket names and the time it was accepted travel in its attachment, which the platform keeps
   * with the socket through hibernation, so vouching for a lease costs no storage write (contract 21.11).
   * A socket opened with `events` also hears the event frames (presence, joined, revoked) and, at once, who is online;
   * it is also the viewer that makes the presence sweep worth arming (contract 21.12.4). `beat` is the ping period the
   * client declared, from which the socket's window follows.
   */
  accept(device, version, now, { holds, beat, events = false }) {
    if (this.ctx.getWebSockets().length >= this.cap) throw new Wire('too_many_watchers', 429);
    const { 0: client, 1: server } = new WebSocketPair();
    this.ctx.acceptWebSocket(server, [device]);
    server.serializeAttachment({ ...FRESH, at: now, holds, events, beat });
    server.send(frame(version));
    if (events) for (const text of this.liveness.snapshot(device)) server.send(text);
    this.liveness.opened(device);
    this.liveness.rearm(); // any new live socket may lapse before the sweep that is set, but only a viewer makes a sweep worth setting
    return new Response(null, { status: 101, webSocket: client, headers: ANSWER_HEADERS });
  }

  /**
   * vouchedUntil is how long the sockets of `device` vouch for its lease of `cell` at `fence` (0: not at all). A
   * socket's last sign of life is its last auto-response, which the platform records without waking us, or the
   * time it was accepted if it never pinged.
   */
  vouchedUntil(device, cell, fence) {
    const sockets = this.ctx.getWebSockets(device).map((ws) => this.#evidence(ws));
    return vouchedUntil(sockets, cell, fence, this.ttlMs);
  }

  /** broadcast tells every socket of the identity the new version. */
  broadcast(version) {
    const text = frame(version);
    for (const ws of this.ctx.getWebSockets()) attempt(() => ws.send(text));
  }

  /** #evidence is what a socket names and when it last showed life, which is all a lease needs of it. */
  #evidence(ws) {
    const { holds, at } = attachmentOf(ws);
    return { holds, seenAt: signOfLife(this.ctx, ws, at, Date.now()) };
  }

  /** #peer is one socket as Liveness reads it: its device, what it hears, and the evidence of its life. */
  #peer(ws) {
    const att = attachmentOf(ws);
    return {
      ws,
      device: this.ctx.getTags(ws)[0],
      events: att.events === true,
      sign: signOfLife(this.ctx, ws, att.at, Date.now()),
      windowMs: windowMs(att.beat),
      lapsed: att.lapsed,
      send: (text) => attempt(() => ws.send(text)),
      // The flag goes first and is stored with the socket, so a sweep that is cut short never tells of it twice.
      lapse: () => (ws.serializeAttachment({ ...att, lapsed: true }), attempt(() => ws.close(CLOSE_SILENT, 'silent'))),
    };
  }

  #peers() {
    return this.ctx.getWebSockets().map((ws) => this.#peer(ws));
  }

  /** left runs when a socket has closed. It answers the device that now holds no live socket, or undefined: a socket that was already silent leaves nothing to tell. */
  left(ws) {
    const peer = this.#peer(ws);
    return peer.device !== undefined && this.liveness.closed(peer) ? peer.device : undefined;
  }

  /** expire sends the offline frames that are due and sweeps the sockets that went silent; the object's alarm calls it. */
  expire() {
    this.liveness.expire();
  }

  /** online is the set of devices that hold a live watch socket now: one inside its window (contract 21.12.2). */
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
