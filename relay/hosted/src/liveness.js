// Who is online, and the event frames that say so (docs/ux-pairing-contract.md, section 5; contract 21.12). A device
// is online iff it holds at least one live watch socket: one that has not been flagged lapsed and whose sign of life
// is inside its window. The only thing kept besides the platform's own socket list is the set of devices whose last
// socket closed less than OFFLINE_DEBOUNCE_MS ago: they were told to be online and have not yet been told otherwise.
// That set is in storage, because the alarm that ends it wakes a fresh object.

/** How long a device may be without a socket before its peers are told it is offline (contract 5). */
export const OFFLINE_DEBOUNCE_MS = 15_000;

/** The largest event frame the relay sends (contract 5). A longer one is dropped, never cut. */
export const MAX_EVENT_BYTES = 512;

/** eventFrame is the text of one event: a string key `t`, no key `v`. */
export const eventFrame = (t, fields) => JSON.stringify({ t, ...fields });

/** The soonest a sweep may follow another, so a socket that is a hair from its lapse does not make the alarm spin. */
const MIN_SWEEP_GAP_MS = 1_000;

/** isLive is the one rule of presence: a socket counts while it is not lapsed and its last sign of life is within its window. */
export const isLive = (p, now) => !p.lapsed && now - p.sign <= p.windowMs;

/** lapseAt is the moment a socket stops being live if it stays silent. */
const lapseAt = (p) => p.sign + p.windowMs;

/**
 * Liveness reads and writes through small seams, so it holds no platform object:
 *   peers()      every open socket as { ws, device, events, send(text), sign, windowMs, lapsed, lapse() }, where
 *                sign is the time of its last sign of life and lapse() flags it lapsed and closes it
 *   pending      a map of device -> due time, the devices whose offline frame is still to come
 *   arm(at)      wakes the object's alarm at `at` (or sooner)
 *   seen(d, at)  records when device d was last heard from (it moves no version)
 */
export class Liveness {
  constructor({ peers, pending, arm, clock, seen = () => {} }) {
    this.peers = peers;
    this.pending = pending;
    this.arm = arm;
    this.clock = clock;
    this.seen = seen;
  }

  #livePeers() {
    const now = this.clock();
    return this.peers().filter((p) => isLive(p, now));
  }

  /** tell sends one event frame (an object with a `t`) to every event socket except the sockets of `except`; `at` defaults to now. */
  tell(frame, except) {
    const text = JSON.stringify({ t: frame.t, ...frame, at: frame.at ?? this.clock() });
    if (text.length > MAX_EVENT_BYTES) return;
    for (const p of this.peers()) if (p.events && p.device !== except) p.send(text);
  }

  #announce(device, online) {
    this.tell({ t: 'presence', device, online }, device);
  }

  /** opened runs after a socket of `device` was accepted: the first live one is news, unless the device was merely reconnecting. */
  opened(device) {
    if (this.#livePeers().filter((p) => p.device === device).length !== 1) return;
    if (this.pending.has(device)) this.pending.delete(device);
    else this.#announce(device, true);
  }

  /**
   * closed runs when `gone` closes. It answers whether that was its device's last live socket, and then starts the
   * debounce. A socket that was not live counted for nothing, so its close is silent: either a sweep already told
   * the device offline, or its peers were never told it was online by this socket.
   */
  closed(gone) {
    if (!isLive(gone, this.clock())) return false;
    const rest = this.#livePeers().filter((p) => p.ws !== gone.ws);
    if (rest.some((p) => p.device === gone.device)) return false;
    const due = this.clock() + OFFLINE_DEBOUNCE_MS;
    this.pending.set(gone.device, due);
    this.arm(due);
    return true;
  }

  /**
   * expire runs on the alarm: it settles the debounces that are over, sweeps the silent, and sets the alarm once for
   * whichever comes first of the debounces still waiting and the next sweep (two sets in one turn could overwrite each other).
   */
  expire() {
    const debounce = this.#expireDebounces();
    this.sweep();
    const at = Math.min(debounce, this.nextSweep() ?? Infinity);
    if (at !== Infinity) this.arm(at);
  }

  /** #expireDebounces tells the devices whose debounce is over that they are offline, and answers when the next debounce ends. */
  #expireDebounces() {
    const now = this.clock();
    let next = Infinity;
    for (const [device, due] of this.pending.entries()) {
      if (due > now) next = Math.min(next, due);
      else (this.pending.delete(device), this.#announce(device, false));
    }
    return next;
  }

  /**
   * sweep tells every device that holds sockets but no live one that it is offline, once: it stamps the device's last
   * sign of life, and lapses each of its sockets, which is what keeps the next sweep from telling it again. It does
   * nothing for a device whose sockets are all lapsed already, because that device was told on an earlier sweep.
   */
  sweep() {
    const now = this.clock();
    const silent = new Map();
    for (const p of this.peers()) silent.set(p.device, [...(silent.get(p.device) ?? []), p]);
    for (const [device, sockets] of silent) {
      if (sockets.some((p) => isLive(p, now)) || sockets.every((p) => p.lapsed)) continue;
      this.#silenced(device, sockets);
    }
  }

  #silenced(device, sockets) {
    this.pending.delete(device); // the debounce, if one was running, is overtaken: the device is told offline now
    this.#announce(device, false);
    this.seen(device, Math.max(...sockets.map((p) => p.sign)));
    for (const p of sockets) if (!p.lapsed) p.lapse();
  }

  /**
   * nextSweep is when the next sweep is due: the earliest lapse among the live sockets, and never sooner than a
   * second from now. It answers undefined when nobody is watching (no event socket) or nothing is live, because a
   * device that nobody looks at needs no push and a later reader applies the window at read time (contract 21.12.4).
   */
  nextSweep() {
    const live = this.#livePeers();
    if (live.length === 0 || !this.peers().some((p) => p.events)) return undefined;
    return Math.max(this.clock() + MIN_SWEEP_GAP_MS, Math.min(...live.map(lapseAt)));
  }

  /** rearm sets the alarm for the next sweep, if one is due. */
  rearm() {
    const at = this.nextSweep();
    if (at !== undefined) this.arm(at);
  }

  /** online is the set of devices that hold a live socket now. */
  online() {
    return new Set(this.#livePeers().map((p) => p.device));
  }

  /** snapshot is the presence frames a new event socket of `device` hears on connect: the others that peers believe online. */
  snapshot(device) {
    const believed = new Set([...this.online(), ...this.pending.keys()]);
    believed.delete(device);
    const at = this.clock();
    return [...believed].map((d) => eventFrame('presence', { device: d, online: true, at }));
  }
}
