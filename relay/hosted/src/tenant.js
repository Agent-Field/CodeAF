// One identity's namespace: its directory, its frames, its quota, and the relay's own count of
// what the identity asked of it. Route handlers talk to this and to nothing beneath it.
import { Directory } from './directory.js';
import { Quota } from './limits.js';
import { Meta } from './meta.js';
import { Stats } from './stats.js';
import { encode } from './frame.js';
import { rotateBy, rotationView } from './rotation.js';
import { R2Store, sealFrame } from './store.js';
import { RuleError } from './rules.js';
import { Wire } from './wire.js';

// How long Has waits for the puts in flight before it fails: a stuck put must not hang Has,
// and must not make it lie (a "no" would make the client send a frame that is about to land).
const HAS_WAIT_MS = 30_000;

// The cell key id of the frame that answers a batch get: the answer belongs to no cell, so all zeros.
const MANY_KEY_ID = '0'.repeat(32);

export class Tenant {
  constructor({ identity, sql, bucket, clock, policy, limits, flight, arm, watchers }) {
    this.identity = identity;
    this.clock = clock;
    this.limits = limits;
    this.arm = arm;
    this.flight = flight;
    this.watchers = watchers;
    this.dir = new Directory(sql, identity, clock, policy, watchers);
    this.quota = new Quota(sql, limits);
    this.store = new R2Store(bucket, sql, identity);
    this.meta = new Meta(sql);
    this.stats = new Stats(this.meta, () => arm(clock() + limits.statsFlushMs));
  }

  /**
   * putFrame validates the frame, stores it unless it is over quota, and counts it once. A frame
   * the store already holds costs the identity nothing, so a client that resends is never refused for it.
   */
  async putFrame(bytes) {
    this.stats.add({ puts: 1, bytes_in: bytes.length });
    const frame = await sealFrame(bytes);
    const cost = { size: frame.size, objects: frame.objects.length };
    const stored = await this.store.put(frame, () => this.quota.admit(cost, this.clock()));
    if (stored) this.quota.record(cost, this.clock());
    return { frame: frame.id, objects: cost.objects };
  }

  async getObject(rid) {
    this.stats.add({ gets: 1 });
    const bytes = await this.store.get(rid);
    if (!bytes) throw new Wire('not_found', 404);
    this.stats.add({ bytes_out: bytes.length });
    return bytes;
  }

  /** getMany answers a batch get: a frame holding a prefix of the objects asked for. */
  async getMany(rids) {
    this.stats.add({ gets: 1 });
    const objects = await this.store.getMany(rids);
    if (objects.length === 0) throw new Wire('not_found', 404);
    this.stats.add({ bytes_out: objects.reduce((n, o) => n + o.bytes.length, 0) });
    return encode(MANY_KEY_ID, objects);
  }

  /** has waits for this identity's puts in flight, then answers from the store. */
  async has(rids) {
    this.stats.add({ has: 1 });
    if (!(await this.flight.idle(HAS_WAIT_MS))) throw new Wire('unreachable', 503);
    return this.store.has(rids);
  }

  /** watch opens a socket for `device` that is told the directory's version now and on every visible change, and that vouches for the leases it names in `holds`. */
  watch(device, holds, events = false) {
    const now = this.clock();
    this.dir.seen(device, now);
    return this.watchers.accept(device, this.dir.version, holds, now, events);
  }

  /** join stores the device a paired device approved, and tells the identity's other sockets it joined. */
  join(id, record) {
    const device = this.dir.approveDevice(id, record);
    this.announce({ t: 'joined', device: id, name: device.name, platform: device.platform, at: this.clock() }, id);
  }

  /**
   * announce hands an event frame (contract 5) to the watchers, to be sent to every event socket but the
   * ones of `except`. A watcher that has no event frames to send ignores it.
   */
  announce(frame, except) {
    this.watchers.announce?.(frame, except);
  }

  /** assertWritable refuses a write to an identity a rotation has replaced, whichever device asks. */
  assertWritable() {
    if (this.dir.rotation()) throw new RuleError('rotated');
  }

  /**
   * rotate makes one move of the rotation state machine; a retire also wakes the object at its deadline to delete.
   * The move and the answer share one reading of the clock, so the deadline an answer names is always its own time plus the grace.
   */
  rotate(device, req) {
    const now = this.clock();
    const next = rotateBy(this.dir.rotation(), device, req, now, this.limits);
    this.dir.setRotation(next);
    if (next?.retire_at) this.arm(next.retire_at);
    return rotationView(next, now, this.limits);
  }

  rotationView() {
    return rotationView(this.dir.rotation(), this.clock(), this.limits);
  }

  /** retireAt is the directory time the identity is deleted at, or undefined while it is not retired. */
  retireAt() {
    return this.dir.rotation()?.retire_at;
  }
}
