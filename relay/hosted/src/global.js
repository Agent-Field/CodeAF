// One durable budget bounds the whole deployment, including frames written before this budget existed.
import { Counters } from './counters.js';
import { DAY_MS } from './newcount.js';
import { Lazy } from './lazy.js';
import { Serial } from './serial.js';
import { Wire } from './wire.js';

export class GlobalBudget {
  #serial = new Serial();
  #ready = new Lazy(() => this.#inventory());

  constructor(sql, bucket, limits) {
    this.sql = sql;
    this.bucket = bucket;
    this.limits = limits;
    this.counters = new Counters(sql);
    sql.exec('CREATE TABLE IF NOT EXISTS global_frames (key TEXT PRIMARY KEY, owner TEXT NOT NULL, bytes INTEGER NOT NULL) WITHOUT ROWID');
    sql.exec('CREATE INDEX IF NOT EXISTS global_owner ON global_frames(owner)');
    sql.exec('CREATE TABLE IF NOT EXISTS global_usage (one INTEGER PRIMARY KEY CHECK (one = 1), bytes INTEGER NOT NULL, ready INTEGER NOT NULL)');
    sql.exec('INSERT OR IGNORE INTO global_usage VALUES (1, 0, 0)');
  }

  // Inventory completes before admitting writes; interrupted scans are safe to repeat.
  async #inventory() {
    if (this.sql.exec('SELECT ready FROM global_usage').one().ready) return;
    let cursor;
    do {
      const page = await this.bucket.list({ cursor, limit: 1000 });
      for (const object of page.objects) this.#record(object.key, object.key.split('/')[0], object.size);
      cursor = page.truncated ? page.cursor : undefined;
    } while (cursor);
    this.sql.exec('UPDATE global_usage SET ready = 1');
  }

  #record(key, owner, bytes) {
    if (this.sql.exec('SELECT key FROM global_frames WHERE key = ?', key).toArray().length) return;
    this.sql.exec('INSERT INTO global_frames VALUES (?, ?, ?)', key, owner, bytes);
    this.sql.exec('UPDATE global_usage SET bytes = bytes + ?', bytes);
  }

  #full(extra = 0) {
    const used = this.sql.exec('SELECT bytes FROM global_usage').one().bytes;
    return used + extra > this.limits.globalStoreBytes || (extra === 0 && used >= this.limits.globalStoreBytes);
  }

  // Admission counts successful new identities only; existing identities bypass this door.
  admit(network, now) {
    return this.#serial.run(async () => {
      await this.#ready.get();
      if (this.#full()) throw new Wire('relay_storage_full', 507);
      for (const [key, limit, code] of [
        [`new:${network}`, this.limits.newIdentitiesPerIpPerDay, 'too_many_identities'],
        ['global:new', this.limits.globalNewIdentitiesPerDay, 'relay_admission_full'],
      ]) {
        const { n, retryAfter } = this.counters.peek(key, now);
        if (n >= limit) throw new Wire(code, 429, retryAfter || DAY_MS / 1000);
      }
      this.counters.hit(`new:${network}`, DAY_MS, now);
      this.counters.hit('global:new', DAY_MS, now);
    });
  }

  // Reservations precede R2 writes and survive a crash; a failed write can retain capacity until owner erasure.
  reserve(owner, frame, bytes) {
    return this.#serial.run(async () => {
      await this.#ready.get();
      const key = `${owner}/f/${frame}`;
      if (this.sql.exec('SELECT key FROM global_frames WHERE key = ?', key).toArray().length) return;
      if (this.#full(bytes)) throw new Wire('relay_storage_full', 507);
      this.#record(key, owner, bytes);
    });
  }

  // Capacity is returned only after the caller has erased every R2 object of the owner.
  forget(owner) {
    return this.#serial.run(async () => {
      await this.#ready.get();
      const bytes = this.sql.exec('SELECT COALESCE(SUM(bytes), 0) AS bytes FROM global_frames WHERE owner = ?', owner).one().bytes;
      this.sql.exec('DELETE FROM global_frames WHERE owner = ?', owner);
      this.sql.exec('UPDATE global_usage SET bytes = bytes - ?', bytes);
    });
  }
}
