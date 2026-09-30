// PairGate: the one Durable Object that all of pairing's global decisions pass through. Pairing
// is unauthenticated (the joining device has no identity yet), so it must not become free
// storage: this object allocates nameplates, caps the live mailboxes, and counts each caller's
// creates, writes and open polls by IP. Its counters are stored, not remembered in memory, so
// idling until the object is evicted does not reset them.
import { DurableObject } from 'cloudflare:workers';
import { Counters } from '../counters.js';
import { limitsOf } from '../limits.js';
import { guarded, rateLimited, Wire } from '../wire.js';

const HOUR_MS = 3_600_000;
const MINUTE_MS = 60_000;
const POLL_LEASE_MS = 30_000; // a poll lasts 25 s at most; a slot whose release was lost frees itself after this

/** digitsFor is the smallest nameplate length, 2 to 4, that keeps the live boxes under a tenth of the space. */
export function digitsFor(live) {
  let k = 2;
  while (k < 4 && live * 10 >= 10 ** k) k++;
  return k;
}

const draw = (k) => crypto.getRandomValues(new Uint32Array(1))[0] % 10 ** k;

export class PairGate extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.limits = limitsOf(env);
    this.sql = ctx.storage.sql;
    this.sql.exec('CREATE TABLE IF NOT EXISTS plates (np TEXT PRIMARY KEY, expires INTEGER NOT NULL)');
    this.counters = new Counters(this.sql);
    this.sql.exec('CREATE TABLE IF NOT EXISTS polls (token TEXT PRIMARY KEY, ip TEXT NOT NULL, expires INTEGER NOT NULL)');
  }

  /** create allocates a nameplate for a new mailbox, opens it, and answers {nameplate, expires_in_ms}. */
  create(ip, keyHash) {
    return guarded(async () => {
      const now = Date.now();
      this.#hit(`create:${ip}`, HOUR_MS, this.limits.pairCreatePerHour, now);
      const nameplate = this.#allocate(now);
      await this.env.MAILBOX.get(this.env.MAILBOX.idFromName(nameplate)).open(keyHash, now + this.limits.pairTtlMs);
      return { nameplate, expires_in_ms: this.limits.pairTtlMs };
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
      if (open >= this.limits.pairConcurrentPolls) throw rateLimited(1);
      const token = crypto.randomUUID();
      this.sql.exec('INSERT INTO polls VALUES (?,?,?)', token, ip, now + POLL_LEASE_MS);
      return token;
    });
  }

  releasePoll(token) {
    this.sql.exec('DELETE FROM polls WHERE token = ?', token);
  }

  /** release frees a nameplate whose mailbox is gone. */
  release(nameplate) {
    this.sql.exec('DELETE FROM plates WHERE np = ?', nameplate);
  }

  // #hit counts one event for key in a fixed window and throws rate_limited past max.
  #hit(key, windowMs, max, now) {
    const { n, retryAfter } = this.counters.hit(key, windowMs, now);
    if (n > max) throw rateLimited(retryAfter);
  }

  // #allocate reserves a free nameplate, or throws full when the relay holds all the mailboxes it will.
  #allocate(now) {
    this.sql.exec('DELETE FROM plates WHERE expires <= ?', now);
    const { live } = this.sql.exec('SELECT COUNT(*) AS live FROM plates').one();
    if (live >= this.limits.pairMaxBoxes) throw new Wire('full', 503);
    for (;;) {
      const np = String(draw(digitsFor(live)));
      if (this.sql.exec('SELECT 1 FROM plates WHERE np = ?', np).toArray().length) continue;
      this.sql.exec('INSERT INTO plates VALUES (?,?)', np, now + this.limits.pairTtlMs);
      return np;
    }
  }
}
