// One identity's namespace: its directory, its frames, its quota, and the relay's own count of
// what the identity asked of it. Route handlers talk to this and to nothing beneath it.
import { Directory } from './directory.js';
import { Quota } from './limits.js';
import { Meta } from './meta.js';
import { Stats } from './stats.js';
import { encode } from './frame.js';
import { rotateBy, rotationView } from './rotation.js';
import { R2Store, Unsatisfiable, sealFrame } from './store.js';
import { RuleError } from './rules.js';
import { Wire } from './wire.js';

// How long Has waits for the puts in flight before it fails: a stuck put must not hang Has,
// and must not make it lie (a "no" would make the client send a frame that is about to land).
const HAS_WAIT_MS = 30_000;

// The cell key id of the frame that answers a batch get: the answer belongs to no cell, so all zeros.
const MANY_KEY_ID = '0'.repeat(32);

// The bytes a frame answer serves: all of them, or the slice one Range header asks for, clamped to
// what the frame holds (R2 clamps a range whose end runs past the frame).
const servedOf = (range, size) => (range ? Math.min(range.length ?? Infinity, size - range.offset) : size);

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

  /**
   * getFrame answers one frame's bytes by id, streaming: the body is R2's own stream, so a frame of
   * up to 16 MiB is piped to the client and never held whole in the isolate's 128 MB. Counted as one
   * get with the bytes actually served, a range included.
   */
  async getFrame(id, range) {
    this.stats.add({ gets: 1 });
    const f = await this.store.getFrame(id, range).catch((e) => {
      if (e instanceof Unsatisfiable) throw new Wire('range_not_satisfiable', 416);
      throw e;
    });
    if (!f) throw new Wire('not_found', 404);
    const served = servedOf(range, f.size);
    this.stats.add({ bytes_out: served });
    return { body: f.body, size: f.size, served };
  }

  /**
   * locate answers where held rids live, and does not wait for puts in flight: the publish ordering
   * rule moves a head only after its objects are stored, so a take's locate never races a put that matters.
   */
  async locate(rids) {
    this.stats.add({ has: 1 });
    return this.store.locate(rids);
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

  /** watch opens a socket for `device` that is told the directory's version now and on every visible change. */
  watch(device) {
    return this.watchers.accept(device, this.dir.version);
  }

  /** assertWritable refuses a write to an identity a rotation has replaced, whichever device asks. */
  assertWritable() {
    if (this.dir.rotation()) throw new RuleError('rotated');
  }

  /** rotate makes one move of the rotation state machine; a retire also wakes the object at its deadline to delete. */
  rotate(device, req) {
    const next = rotateBy(this.dir.rotation(), device, req, this.clock(), this.limits);
    this.dir.setRotation(next);
    if (next?.retire_at) this.arm(next.retire_at);
    return this.rotationView();
  }

  rotationView() {
    return rotationView(this.dir.rotation(), this.clock(), this.limits);
  }

  /** retireAt is the directory time the identity is deleted at, or undefined while it is not retired. */
  retireAt() {
    return this.dir.rotation()?.retire_at;
  }
}
