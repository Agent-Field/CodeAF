// What a phone is doing at the relay, as far as the relay can tell: waiting on its slots, or reading them. Computers
// publish at streaming speed only while a phone looks, so this is the state that decides whether a reply costs a write
// every quarter second or one every 30 seconds (design 3.7, 3.9, 7.3; contract L4). It lives in the object that holds
// the directory watch sockets, because the answer has to reach those sockets and the slot routes in one place.
//
// A phone is WATCHING while a slot wait is parked for it, and afterwards for a while from the last moment it waited or
// read. Two rules read that one moment (`last`) with two lengths: the header rule (30 s) answers a computer that is
// writing a slot, and the event rule (45 s) tells the computers' watch sockets, which are hibernating and have no
// other way to learn. The event rule is the longer one on purpose: a computer that already holds the header never
// has to hear that the phone went quiet before the phone did.
//
// The parked count and `last` are kept in memory, because a held wait keeps the object alive and a phone that reads
// or waits again writes them again. Two things must still outlive an eviction: the off edge (a computer told "on"
// must one day be told "off", or it streams at full speed to nobody) and the replay a new socket hears. So while a phone
// is announced as watching, its `last` is stored in the object's key-value storage, refreshed at most every
// PERSIST_EVERY_MS, and removed when the "off" frame goes out. A quiet identity stores nothing and arms nothing.
import { Flight } from './flight.js';
import { eventFrame } from './liveness.js';
import { Wire } from './wire.js';

/** The header a slot PUT reply carries while the phone is watching in the header sense (value always "1"). */
export const WATCHING_HEADER = 'Codeaf-Client-Watching';
export const HEADER_FOR_MS = 30_000;
export const EVENT_FOR_MS = 45_000;
/** At most one frame per phone in this long; a faster flip is held and the latest state is sent when it has passed. */
export const DAMPEN_MS = 2_000;
const PERSIST_EVERY_MS = 5_000;
const PREFIX = 'watching:';

/**
 * ClientWatch holds the watching state of every phone of one identity. Its seams are small and hold no platform object:
 *   clock()       the time, so a test controls it
 *   kv()          the object's synchronous key-value storage (get, put, delete, list({prefix}))
 *   tell(frame)   sends an event frame ({t, device, on}) to the event sockets
 *   arm(at)       wakes the object's alarm at `at` or sooner
 *   listening()   whether any event socket is open: with none, nobody needs an off frame and no alarm is armed
 */
export class ClientWatch {
  #clients = new Map(); // device -> {parked, handles, last, stored, sent, sentAt, dead}
  #total = 0;
  #loaded = false;

  constructor({ clock, kv, tell, arm, listening }) {
    this.clock = clock;
    this.kv = kv;
    this.tell = tell;
    this.arm = arm;
    this.listening = listening;
  }

  /** #loadAll reads the phones an earlier instance announced as watching, once: after that, memory is the truth. */
  #loadAll() {
    if (this.#loaded) return;
    this.#loaded = true;
    for (const [key, last] of this.kv().list({ prefix: PREFIX })) {
      const device = key.slice(PREFIX.length);
      // A stored record means an "on" frame is outstanding, whatever the age of `last`: the next settle sends the "off".
      if (!this.#clients.has(device)) this.#clients.set(device, { parked: 0, handles: new Set(), last, stored: last, sent: true, sentAt: undefined, dead: false });
    }
  }

  #entry(device) {
    this.#loadAll();
    let e = this.#clients.get(device);
    if (!e) {
      e = { parked: 0, handles: new Set(), last: 0, stored: 0, sent: false, sentAt: undefined, dead: false };
      this.#clients.set(device, e);
    }
    return e;
  }

  // The event rule: a wait is parked, or the last wait or read is younger than 45 s.
  #want(e, now) {
    return !e.dead && (e.parked > 0 || (e.last > 0 && now - e.last < EVENT_FOR_MS));
  }

  /** headerOn is the header rule: a wait is parked, or the last wait ended or slot was read less than 30 s ago. */
  headerOn(device) {
    this.#loadAll();
    const e = this.#clients.get(device);
    return e !== undefined && !e.dead && (e.parked > 0 || (e.last > 0 && this.clock() - e.last < HEADER_FOR_MS));
  }

  /** touch records that the phone read a slot now. */
  touch(device) {
    const e = this.#entry(device);
    e.last = this.clock();
    this.#settle(device);
  }

  /**
   * park counts one held slot wait for `device` and answers its handle: `woken` settles when a slot is written for the
   * phone, and `release` ends the wait. At most `perClient` may be parked for one phone and `perIdentity` for the whole
   * identity, which is the cap the Go relay's process-wide counter has no Worker equivalent for; past either, 429 full.
   */
  park(device, { perClient, perIdentity }) {
    const e = this.#entry(device);
    if (this.#total >= perIdentity || e.parked >= perClient) {
      this.#settle(device); // an entry made just for this refusal must not be left behind
      throw new Wire('full', 429);
    }
    const handle = { device, released: false, fire: () => {}, woken: null };
    handle.woken = new Promise((resolve) => {
      handle.fire = resolve;
    });
    e.parked++;
    this.#total++;
    e.handles.add(handle);
    e.last = this.clock();
    this.#settle(device);
    return handle;
  }

  /** release ends a parked wait; the moment it ends is the last moment the phone was waiting. It is safe to call twice. */
  release(handle) {
    if (handle.released) return;
    handle.released = true;
    this.#total--;
    const e = this.#clients.get(handle.device);
    if (!e) return;
    e.parked--;
    e.handles.delete(handle);
    if (!e.dead) e.last = this.clock();
    this.#settle(handle.device);
  }

  /** wake tells the waits parked for `device` that a slot was written. */
  wake(device) {
    for (const handle of this.#clients.get(device)?.handles ?? []) handle.fire();
  }

  /** forget is a revoke: the phone is no longer watching, however recently it waited, and nothing it does counts again. */
  forget(device) {
    this.#loadAll();
    const e = this.#clients.get(device);
    if (!e) return;
    e.dead = true;
    e.last = 0;
    this.#settle(device);
  }

  /** snapshot is the frames a new event socket hears at connect: one "on" for every phone that is watching now. */
  snapshot() {
    this.#loadAll();
    const now = this.clock();
    return [...this.#clients]
      .filter(([, e]) => this.#want(e, now))
      .map(([device]) => device)
      .sort()
      .map((device) => eventFrame('watching', { device, on: true, at: now }));
  }

  /** expire runs on the alarm: it settles every phone whose edge is due, the off edges and the frames the damping held. */
  expire() {
    this.#loadAll();
    for (const device of [...this.#clients.keys()]) this.#settle(device);
  }

  /** rearm sets the alarm again after something cleared it, and when a first event socket arrives. */
  rearm() {
    this.#loadAll();
    const now = this.clock();
    for (const [device, e] of this.#clients) this.#schedule(device, e, now);
  }

  /**
   * #settle makes the state the sockets were told equal to the state the rules give now, if the damping allows a frame.
   * The latest state wins: a flip held back is judged again when it is due, so on, off, on inside two seconds sends only
   * what is true then (and nothing, when that is what was already sent).
   */
  #settle(device) {
    const e = this.#clients.get(device);
    if (!e) return;
    const now = this.clock();
    const want = this.#want(e, now);
    if (want !== e.sent && (e.sentAt === undefined || now - e.sentAt >= DAMPEN_MS)) {
      e.sent = want;
      e.sentAt = now;
      this.tell({ t: 'watching', device, on: want });
      if (!want) {
        this.kv().delete(PREFIX + device);
        e.stored = 0;
      }
    }
    // While a phone is announced as watching its last moment is stored, so an evicted object still knows when to say "off".
    if (e.sent && e.last > 0 && (e.stored === 0 || e.last - e.stored >= PERSIST_EVERY_MS)) {
      this.kv().put(PREFIX + device, e.last);
      e.stored = e.last;
    }
    this.#schedule(device, e, now);
    // A quiet phone is forgotten, but not before its last frame is 2 s old: the damping of its next frame needs that time.
    if (!e.sent && !want && e.parked === 0 && (e.sentAt === undefined || now - e.sentAt >= DAMPEN_MS)) this.#clients.delete(device);
  }

  // Arms the alarm for the next thing this phone is owed: the held frame, or the off edge. Only a listener is worth waking for.
  #schedule(device, e, now) {
    if (!this.listening()) return;
    const want = this.#want(e, now);
    // An entry an earlier instance announced has no frame time here, so its overdue "off" is due at once.
    if (want !== e.sent) this.arm(e.sentAt === undefined ? now : e.sentAt + DAMPEN_MS);
    else if (e.sent && e.parked === 0) this.arm(e.last + EVENT_FOR_MS);
  }
}

/**
 * byFlight is the one timer a held wait uses, and it is flight.js's: a Flight with one put in flight is idle only when that
 * put leaves, so `idle(ms)` is a deadline that the release cancels. The identity object may hold no timer of its own (the
 * request-scoped law lets flight.js keep the one), and a wait that has to end after at most 25 s is a wait for the puts
 * of a Flight that never finishes. It answers the cancel.
 */
function byFlight(ms, fn) {
  const flight = new Flight();
  const cancel = flight.enter();
  flight.idle(ms).then((idle) => idle || fn());
  return cancel;
}

/**
 * holdWait waits for the first of: a slot written for the phone (`woken`), the end of the wait (`seconds`), the request
 * being given up (`signal`), or the wait being stopped by a revoke (`stopped` rejects, and so does holdWait). It answers
 * 'woken', 'timeout' or 'aborted', and leaves no timer or listener behind. It holds nothing of the host's queue: the
 * request just sits here, which is the held-request transport of design 3.7, bounded by the 25 s a wait may last.
 */
export function holdWait({ woken, stopped, seconds, signal, timer = byFlight }) {
  return new Promise((resolve, reject) => {
    let cancel = () => {};
    const cleanup = () => {
      cancel();
      signal?.removeEventListener('abort', abort);
    };
    const finish = (how) => {
      cleanup();
      resolve(how);
    };
    const abort = () => finish('aborted');
    if (signal?.aborted) return resolve('aborted');
    signal?.addEventListener('abort', abort);
    cancel = timer(seconds * 1000, () => finish('timeout'));
    woken.then(() => finish('woken'));
    stopped.catch((error) => {
      cleanup();
      reject(error);
    });
  });
}
