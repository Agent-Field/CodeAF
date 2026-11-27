// A relay's watchers over the platform's sockets as a stand-in, for the tests of the phone's fabric: a socket keeps an
// attachment, a last auto-response time and what it was sent; the clock is Date.now, replaced by hand; the alarm is a
// list of the times it was set for. Nothing here is the Durable Object runtime, so no workerd is needed.
import { Watchers } from '../src/watch.js';

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
    getWebSockets: (tag) => open.filter((w) => !w.gone && (tag === undefined || w.tags.includes(tag))),
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
  return { ctx, socket, open, kv };
}

/**
 * watchRig builds Watchers over that stand-in. `dial` opens a socket the way the upgrade does (the globals a 101 needs are
 * swapped in only for that call, so Response stays real everywhere else) and answers the socket; `frames` reads the
 * parsed event frames a socket was sent after the version frame. `armed` is every time the alarm was asked for, and
 * `fire` moves the clock to a time and runs the alarm's work the way the object's alarm does.
 */
export function watchRig() {
  const real = { now: Date.now };
  const clock = { now: 5_000_000 };
  const p = platform();
  Date.now = () => clock.now;
  const armed = [];
  const watchers = new Watchers(p.ctx, 1000, 90_000, (at) => armed.push(at));
  const restore = () => {
    Date.now = real.now;
  };
  // swapped puts in the globals a 101 needs (a socket pair, and a Response that may carry status 101) and answers the function that puts the real ones back.
  const swapped = () => {
    const saved = { Pair: globalThis.WebSocketPair, Response: globalThis.Response };
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
    return () => {
      globalThis.WebSocketPair = saved.Pair;
      globalThis.Response = saved.Response;
    };
  };
  const dial = (device, beat = 10, { events = false, serving = false } = {}) => {
    const back = swapped();
    try {
      watchers.accept(device, 1, clock.now, { holds: [], beat, events, serving });
    } finally {
      back();
    }
    return p.open.at(-1);
  };
  // upgrading runs an async function (the host's watch branch) under the same globals, and restores them when it ends.
  const upgrading = async (fn) => {
    const back = swapped();
    try {
      return await fn();
    } finally {
      back();
    }
  };
  const frames = (ws) => ws.sent.slice(1).map((text) => JSON.parse(text));
  const watching = (ws) => frames(ws).filter((f) => f.t === 'watching');
  const fire = (at) => {
    clock.now = at;
    watchers.expire();
  };
  return { clock, armed, watchers, dial, upgrading, frames, watching, fire, restore, p, clients: watchers.clients };
}
