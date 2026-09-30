// One identity's namespace: its directory, its frames, its quota, and the relay's own count of
// what the identity asked of it. Route handlers talk to this and to nothing beneath it.
import { Directory } from './directory.js';
import { Quota } from './limits.js';
import { R2Store, sealFrame } from './store.js';
import { Wire } from './wire.js';

// How long Has waits for the puts in flight before it fails: a stuck put must not hang Has,
// and must not make it lie (a "no" would make the client send a frame that is about to land).
const HAS_WAIT_MS = 30_000;

export class Tenant {
  // stats is "the server's own count for this identity since start" (contract 3): it lives in memory.
  stats = { puts: 0, gets: 0, has: 0, bytes_in: 0, bytes_out: 0 };

  constructor({ identity, sql, bucket, clock, policy, limits, flight }) {
    this.identity = identity;
    this.clock = clock;
    this.flight = flight;
    this.dir = new Directory(sql, identity, clock, policy);
    this.quota = new Quota(sql, limits);
    this.store = new R2Store(bucket, identity);
  }

  /**
   * putFrame validates the frame, stores it unless it is over quota, and counts it once. A frame
   * the store already holds costs the identity nothing, so a client that resends is never refused for it.
   */
  async putFrame(bytes) {
    this.stats.puts++;
    this.stats.bytes_in += bytes.length;
    const frame = await sealFrame(bytes);
    const cost = { size: frame.size, objects: frame.objects.length };
    const stored = await this.store.put(frame, () => this.quota.admit(cost, this.clock()));
    if (stored) this.quota.record(cost, this.clock());
    return { frame: frame.id, objects: cost.objects };
  }

  async getObject(rid) {
    this.stats.gets++;
    const bytes = await this.store.get(rid);
    if (!bytes) throw new Wire('not_found', 404);
    this.stats.bytes_out += bytes.length;
    return bytes;
  }

  /** has waits for this identity's puts in flight, then answers from the store. */
  async has(rids) {
    this.stats.has++;
    if (!(await this.flight.idle(HAS_WAIT_MS))) throw new Wire('unreachable', 503);
    return this.store.has(rids);
  }
}
