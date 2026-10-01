// Who is online, and the event frames that say so (docs/ux-pairing-contract.md, section 5). A device is online
// iff it holds at least one open watch socket. The only thing kept besides the platform's own socket list is the
// set of devices whose last socket closed less than OFFLINE_DEBOUNCE_MS ago: they were told to be online and have
// not yet been told otherwise. That set is in storage, because the alarm that ends it wakes a fresh object.

/** How long a device may be without a socket before its peers are told it is offline (contract 5). */
export const OFFLINE_DEBOUNCE_MS = 15_000;

/** The largest event frame the relay sends (contract 5). A longer one is dropped, never cut. */
export const MAX_EVENT_BYTES = 512;

/** eventFrame is the text of one event: a string key `t`, no key `v`. */
export const eventFrame = (t, fields) => JSON.stringify({ t, ...fields });

/**
 * Liveness reads and writes through three small seams, so it holds no platform object:
 *   peers()   every open socket as { device, events, send(text) }
 *   pending   a map of device -> due time, the devices whose offline frame is still to come
 *   arm(at)   wakes the object's alarm at `at` (or sooner)
 */
export class Liveness {
  constructor({ peers, pending, arm, clock }) {
    this.peers = peers;
    this.pending = pending;
    this.arm = arm;
    this.clock = clock;
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

  /** opened runs after a socket of `device` was accepted: the first one is news, unless the device was merely reconnecting. */
  opened(device) {
    if (this.peers().filter((p) => p.device === device).length !== 1) return;
    if (this.pending.has(device)) this.pending.delete(device);
    else this.#announce(device, true);
  }

  /** closed runs when `gone` closes. It answers whether that was its device's last socket, and then starts the debounce. */
  closed(gone) {
    const rest = this.peers().filter((p) => p.ws !== gone.ws);
    if (rest.some((p) => p.device === gone.device)) return false;
    const due = this.clock() + OFFLINE_DEBOUNCE_MS;
    this.pending.set(gone.device, due);
    this.arm(due);
    return true;
  }

  /** expire tells that every device whose debounce is over is offline, and re-arms for the ones still waiting. */
  expire() {
    const now = this.clock();
    let next = Infinity;
    for (const [device, due] of this.pending.entries()) {
      if (due > now) next = Math.min(next, due);
      else (this.pending.delete(device), this.#announce(device, false));
    }
    if (next !== Infinity) this.arm(next);
  }

  /** online is the set of devices that hold a socket now. */
  online() {
    return new Set(this.peers().map((p) => p.device));
  }

  /** snapshot is the presence frames a new event socket of `device` hears on connect: the others that peers believe online. */
  snapshot(device) {
    const believed = new Set([...this.online(), ...this.pending.keys()]);
    believed.delete(device);
    const at = this.clock();
    return [...believed].map((d) => eventFrame('presence', { device: d, online: true, at }));
  }
}
