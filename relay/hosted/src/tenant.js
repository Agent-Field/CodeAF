// One identity's namespace: its directory, its frames, its quota, and the relay's own count of
// what the identity asked of it. Route handlers talk to this and to nothing beneath it.
import { Directory } from './directory.js';
import { Quota } from './limits.js';
import { Meta } from './meta.js';
import { Stats } from './stats.js';
import { encode } from './frame.js';
import { R2Store, sealFrame } from './store.js';
import { Wire } from './wire.js';

// How long Has waits for the puts in flight before it fails: a stuck put must not hang Has,
// and must not make it lie (a "no" would make the client send a frame that is about to land).
const HAS_WAIT_MS = 30_000;

// The cell key id of the frame that answers a batch get: the answer belongs to no cell, so all zeros.
const MANY_KEY_ID = '0'.repeat(32);

export class Tenant {
  constructor({ identity, sql, bucket, clock, policy, limits, flight, scheduleFlush }) {
    this.identity = identity;
    this.clock = clock;
    this.flight = flight;
    this.dir = new Directory(sql, identity, clock, policy);
    this.quota = new Quota(sql, limits);
    this.store = new R2Store(bucket, sql, identity);
    this.meta = new Meta(sql);
    this.stats = new Stats(this.meta, scheduleFlush);
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
}
