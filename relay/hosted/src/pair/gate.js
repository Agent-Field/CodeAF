// PairGate: the one Durable Object that all of pairing's global decisions pass through. Pairing
// is unauthenticated (the joining device has no identity yet), so it must not become free
// storage: this object allocates nameplates, caps the live mailboxes, and counts each caller's
// creates, writes and open polls by IP. Its counters are stored, not remembered in memory, so
// idling until the object is evicted does not reset them.
import { DurableObject } from 'cloudflare:workers';
import { Counters } from '../counters.js';
import { limitsOf } from '../limits.js';
import { guarded, unwrap } from '../wire.js';
import { newGeneration } from './generation.js';
import { Plates } from './plates.js';
import { slowDown } from './refusals.js';

const HOUR_MS = 3_600_000;
const MINUTE_MS = 60_000;
const POLL_LEASE_MS = 30_000; // a poll lasts 25 s at most; a slot whose release was lost frees itself after this

export class PairGate extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.limits = limitsOf(env);
    this.sql = ctx.storage.sql;
    this.plates = new Plates(this.sql, this.limits);
    this.counters = new Counters(this.sql);
    this.sql.exec('CREATE TABLE IF NOT EXISTS polls (token TEXT PRIMARY KEY, ip TEXT NOT NULL, expires INTEGER NOT NULL)');
  }

  /** create allocates a nameplate for a new mailbox, opens it as a new generation, and answers {nameplate, expires_in_ms, gen}. */
  create(ip, keyHash) {
    return guarded(async () => {
      const now = Date.now();
      this.#hit(`create:${ip}`, HOUR_MS, this.limits.pairCreatePerHour, now);
      const nameplate = this.plates.allocate(now);
      const gen = newGeneration();
      unwrap(await this.env.MAILBOX.get(this.env.MAILBOX.idFromName(nameplate)).open(keyHash, now + this.limits.pairTtlMs, gen));
      return { nameplate, expires_in_ms: this.limits.pairTtlMs, gen };
    });
  }

  /** admitWrite charges one write to the caller's IP. */
  admitWrite(ip) {
    return guarded(() => this.#hit(`write:${ip}`, MINUTE_MS, this.limits.pairWritePerMinute, Date.now()));
  }

  /** acquirePoll takes one of the caller's open-poll slots and answers the token that gives it back. */
  acquirePoll(ip) {
    return guarded(() => {
      const now = Date.now();
      this.sql.exec('DELETE FROM polls WHERE expires <= ?', now);
      const { open } = this.sql.exec('SELECT COUNT(*) AS open FROM polls WHERE ip = ?', ip).one();
      if (open >= this.limits.pairConcurrentPolls) throw slowDown(1);
      const token = crypto.randomUUID();
      this.sql.exec('INSERT INTO polls VALUES (?,?,?)', token, ip, now + POLL_LEASE_MS);
      return token;
    });
  }

  releasePoll(token) {
    this.sql.exec('DELETE FROM polls WHERE token = ?', token);
  }

  /** release ends a nameplate whose mailbox is gone; the plate stays quarantined before it is drawn again. */
  release(nameplate) {
    this.plates.release(nameplate, Date.now());
  }

  // #hit counts one event for key in a fixed window and throws rate_limited past max.
  #hit(key, windowMs, max, now) {
    const { n, retryAfter } = this.counters.hit(key, windowMs, now);
    if (n > max) throw slowDown(retryAfter);
  }
}
