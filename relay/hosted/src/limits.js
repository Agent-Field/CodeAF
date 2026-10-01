// Every number the relay enforces, in one table, and the two small mechanisms that enforce them.
// Free forever needs ceilings: the money-bearing ones (bytes, objects, frames a day) are kept in
// the identity's Durable Object storage and survive eviction; the per-minute rates are kept in
// memory, because an eviction can only forgive a burst, never let a stored quota be exceeded.
import { full, rateLimited } from './wire.js';

const GiB = 1 << 30;
const MiB = 1 << 20;

export const DEFAULTS = {
  // One identity.
  storeBytes: 5 * GiB, // stored bytes; the cost ceiling
  storeObjects: 5_000_000, // the index is rows in the identity's SQLite, so this is a backstop; the byte cap rules
  framesPerDay: 5_000, // R2 writes, the dearest operation
  requestsPerMinute: 1_200, // all devices of one identity together
  requestsPerMinutePerDevice: 600,
  statsFlushMs: 5_000, // how long a count may wait in memory before it is written (see stats.js)
  concurrentPuts: 2, // a 16 MiB frame costs the isolate 2 to 3 times its size while it arrives
  // Rotation (contract 20.3): how long a retired identity stays readable before it is deleted.
  // The minimum and the page size are the test hooks, like the self-hosted relay's --min-grace.
  minGraceMs: 3_600_000,
  maxGraceMs: 30 * 86_400_000,
  defaultGraceMs: 7 * 86_400_000,
  maxWatchers: 1_000, // directory watch sockets one identity may hold (contract 21.8); the Go relay's directory.MaxWatchers
  maxHolds: 16, // cells one watch socket may vouch for (contract 21.11.1); the Go relay's directory.MaxHolds
  sweepPageSize: 1_000, // R2 objects deleted per alarm turn (R2's own limit per list and per delete)
  // The whole relay, by caller IP: how many identities one address may bring in (contract 6: no accounts).
  newIdentitiesPerIpPerDay: 20,
  // The pairing mailbox (contract 18.4), for callers that have no identity yet.
  pairTtlMs: 10 * 60_000,
  pairMaxMsg: 4 << 10,
  pairMaxMsgsPerSide: 4,
  pairCreatePerHour: 10,
  pairWritePerMinute: 30,
  pairConcurrentPolls: 4,
  pairMaxBoxes: 2_000, // at 32 KiB each this is also the 64 MiB ceiling in all
  // Link requests (docs/ux-pairing-contract.md 3.6), for a new device that has no identity yet. The
  // time to live is pairTtlMs, shared with the mailbox.
  linkCreatePerHour: 10,
  linkPendingPerIp: 3,
  linkReadPerMinute: 60,
  linkMissPerMinute: 20,
  linkConcurrentPolls: 4,
  linkMaxGrant: 4_096,
  linkMaxLive: 2_000,
  linkDecidedKeepMs: 2 * 60_000, // how long a decided request can still be read
};

/** limitsOf reads the table, with any number the deployment overrides in its CAF_LIMITS variable (JSON). */
export const limitsOf = (env) => ({ ...DEFAULTS, ...JSON.parse(env.CAF_LIMITS ?? '{}') });

/**
 * RateLimit counts hits per key in fixed windows and answers how long a refused caller should
 * wait. Every key's window rolls together, so the table never grows past one window's keys.
 */
export class RateLimit {
  #hits = new Map();
  #windowEnd = 0;

  constructor(max, windowMs = 60_000) {
    this.max = max;
    this.windowMs = windowMs;
  }

  /** admit counts one hit for key at now and throws rate_limited when the key is over its share. */
  admit(key, now) {
    if (now >= this.#windowEnd) {
      this.#hits.clear();
      this.#windowEnd = now + this.windowMs;
    }
    const n = (this.#hits.get(key) ?? 0) + 1;
    this.#hits.set(key, n);
    if (n > this.max) throw rateLimited((this.#windowEnd - now) / 1000);
  }
}

const DAY_MS = 86_400_000;

/**
 * Quota is one identity's stored totals, kept in its Durable Object's SQLite: bytes and objects
 * held, and the frames stored today. admit refuses a frame that would pass a ceiling, before
 * it costs anything; record counts a frame once R2 has confirmed it is new.
 */
export class Quota {
  constructor(sql, limits) {
    this.sql = sql;
    this.limits = limits;
    sql.exec('CREATE TABLE IF NOT EXISTS usage (one INTEGER PRIMARY KEY CHECK (one = 1), day INTEGER, frames INTEGER, bytes INTEGER, objects INTEGER)');
    sql.exec('INSERT OR IGNORE INTO usage VALUES (1, 0, 0, 0, 0)');
  }

  today(now) {
    const row = this.sql.exec('SELECT day, frames, bytes, objects FROM usage').one();
    return { ...row, frames: row.day === Math.floor(now / DAY_MS) ? row.frames : 0 };
  }

  /** admit throws full when storing `frame` ({size, objects}) would pass a stored ceiling, naming the byte ceiling when that is the one. */
  admit(frame, now) {
    const used = this.today(now);
    // Only the byte ceiling is worth telling the person: it is the one they can reason about, so it alone rides in the body.
    if (used.bytes + frame.size > this.limits.storeBytes) throw full(this.limits.storeBytes);
    const over = used.frames + 1 > this.limits.framesPerDay || used.objects + frame.objects > this.limits.storeObjects;
    if (over) throw full();
  }

  record(frame, now) {
    const used = this.today(now);
    this.sql.exec(
      'UPDATE usage SET day = ?, frames = ?, bytes = ?, objects = ?',
      Math.floor(now / DAY_MS), used.frames + 1, used.bytes + frame.size, used.objects + frame.objects,
    );
  }
}
