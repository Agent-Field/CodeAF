// The directory watch socket of one identity (contract 21). It is the server half of a push that costs
// nothing while idle: the object accepts sockets with the hibernation API, so a socket that is open and
// quiet holds no memory and bills no time, and it sets no timer and opens no connection for any socket.
// A socket carries one number, the directory's version, and never a record. The list of sockets is the
// platform's own (ctx.getWebSockets survives hibernation), so the object keeps none of its own.
import { Wire } from './wire.js';

/** The close codes a client acts on: a revoked device stops for good; a rotation stops the watching. */
export const CLOSE_REVOKED = 4401;
export const CLOSE_ROTATED = 4410;

const frame = (version) => `{"v":${version}}`;

/** NOBODY hears nothing: what a directory uses when no socket can ever be open (tests, the pure rules). */
export const NOBODY = { broadcast() {}, closeDevice() {}, closeAll() {} };

export class Watchers {
  constructor(ctx, cap) {
    this.ctx = ctx;
    this.cap = cap;
    // The platform answers the client's keepalive itself, without running this object, so it never wakes.
    ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('ping', 'pong'));
  }

  /** accept opens a socket for `device`, tells it `version` at once, and answers the 101 that completes the upgrade. */
  accept(device, version) {
    if (this.ctx.getWebSockets().length >= this.cap) throw new Wire('too_many_watchers', 429);
    const { 0: client, 1: server } = new WebSocketPair();
    this.ctx.acceptWebSocket(server, [device]);
    server.send(frame(version));
    return new Response(null, { status: 101, webSocket: client });
  }

  /** broadcast tells every socket of the identity the new version. */
  broadcast(version) {
    const text = frame(version);
    for (const ws of this.ctx.getWebSockets()) attempt(() => ws.send(text));
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
