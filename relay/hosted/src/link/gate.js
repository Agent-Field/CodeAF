// LinkGate: the one Durable Object that link pairing's global decisions pass through, as PairGate is
// for the mailbox. A request is created by a device with no identity, so every limit is by the caller's
// IP: creates an hour, requests pending at once, reads and misses a minute, open polls. It also
// allocates the code, so two requests never share one, and counts the live requests in all. Its
// counters are stored, so waiting for the object to be evicted does not reset them.
import { DurableObject } from 'cloudflare:workers';
import { Counters } from '../counters.js';
import { limitsOf } from '../limits.js';
import { guarded, unwrap } from '../wire.js';
import { newCode } from './code.js';
import { relayFull, slowDown } from './refusals.js';

const HOUR_MS = 3_600_000;
const MINUTE_MS = 60_000;
const POLL_LEASE_MS = 30_000; // a poll lasts 25 s at most; a slot whose release was lost frees itself after this

export class LinkGate extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.limits = limitsOf(env);
    this.sql = ctx.storage.sql;
    this.sql.exec('CREATE TABLE IF NOT EXISTS live (code TEXT PRIMARY KEY, ip TEXT NOT NULL, pending INTEGER NOT NULL, expires INTEGER NOT NULL) WITHOUT ROWID');
    this.sql.exec('CREATE TABLE IF NOT EXISTS polls (token TEXT PRIMARY KEY, ip TEXT NOT NULL, expires INTEGER NOT NULL)');
    this.counters = new Counters(this.sql);
  }

  /** create admits one request from ip, allocates its code, opens its object and answers the create answer. */
  create(ip, derived) {
    return guarded(async () => {
      const now = Date.now();
      this.#hit(`create:${ip}`, HOUR_MS, this.limits.linkCreatePerHour, now);
      this.#admitLive(ip, now);
      const record = { ...derived, code: this.#allocate(ip, now), requested_at: now, expires_at: now + this.limits.pairTtlMs };
      unwrap(await this.#requestOf(record.code).open(record));
      const { code, device, check, requested_at, expires_at } = record;
      return { code, device, check, requested_at, expires_at };
    });
  }

  /** decided says the request no longer counts as pending, and shortens its place in the table to what its object keeps it. */
  decided(code) {
    return guarded(() => {
      this.sql.exec('UPDATE live SET pending = 0, expires = ? WHERE code = ?', Date.now() + this.limits.linkDecidedKeepMs, code);
    });
  }

  /** admitRead charges one read to ip, and refuses it outright when ip has already missed too often. */
  admitRead(ip) {
    return guarded(() => {
      const now = Date.now();
      this.#hit(`read:${ip}`, MINUTE_MS, this.limits.linkReadPerMinute, now);
      const missed = this.counters.peek(`miss:${ip}`, now);
      if (missed.n >= this.limits.linkMissPerMinute) throw slowDown(missed.retryAfter);
    });
  }

  /** miss counts a read of a code that is not live. */
  miss(ip) {
    return guarded(() => void this.counters.hit(`miss:${ip}`, MINUTE_MS, Date.now()));
  }

  /** acquirePoll takes one of ip's open-poll slots and answers the token that gives it back. */
  acquirePoll(ip) {
    return guarded(() => {
      const now = Date.now();
      this.sql.exec('DELETE FROM polls WHERE expires <= ?', now);
      const { open } = this.sql.exec('SELECT COUNT(*) AS open FROM polls WHERE ip = ?', ip).one();
      if (open >= this.limits.linkConcurrentPolls) throw slowDown(1);
      const token = crypto.randomUUID();
      this.sql.exec('INSERT INTO polls VALUES (?,?,?)', token, ip, now + POLL_LEASE_MS);
      return token;
    });
  }

  releasePoll(token) {
    this.sql.exec('DELETE FROM polls WHERE token = ?', token);
  }

  #requestOf(code) {
    return this.env.LINK_REQUEST.get(this.env.LINK_REQUEST.idFromName(code));
  }

  #hit(key, windowMs, max, now) {
    const { n, retryAfter } = this.counters.hit(key, windowMs, now);
    if (n > max) throw slowDown(retryAfter);
  }

  // #admitLive turns away an ip with too many pending requests, and the relay when it holds all it will.
  #admitLive(ip, now) {
    this.sql.exec('DELETE FROM live WHERE expires <= ?', now);
    const { total, mine } = this.sql
      .exec('SELECT COUNT(*) AS total, COALESCE(SUM(pending = 1 AND ip = ?), 0) AS mine FROM live', ip)
      .one();
    if (mine >= this.limits.linkPendingPerIp) throw slowDown(1);
    if (total >= this.limits.linkMaxLive) throw relayFull();
  }

  // #allocate reserves a code no live request holds.
  #allocate(ip, now) {
    for (;;) {
      const code = newCode();
      if (this.sql.exec('SELECT 1 FROM live WHERE code = ?', code).toArray().length) continue;
      this.sql.exec('INSERT INTO live VALUES (?,?,1,?)', code, ip, now + this.limits.pairTtlMs);
      return code;
    }
  }
}
