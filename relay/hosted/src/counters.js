// Counters keep fixed-window event counts by key in a Durable Object's SQLite, so a caller cannot
// reset its count by waiting for the object to be evicted. Expired windows are deleted as they are met.
export class Counters {
  constructor(sql) {
    this.sql = sql;
    sql.exec('CREATE TABLE IF NOT EXISTS hits (key TEXT PRIMARY KEY, until INTEGER NOT NULL, n INTEGER NOT NULL) WITHOUT ROWID');
  }

  /** hit counts one event for key at now and answers {n, retryAfter}: the count in its window, and seconds until it ends. */
  hit(key, windowMs, now) {
    this.sql.exec('DELETE FROM hits WHERE until <= ?', now);
    const row = this.sql.exec('SELECT until, n FROM hits WHERE key = ?', key).toArray()[0];
    const until = row?.until ?? now + windowMs;
    const n = (row?.n ?? 0) + 1;
    this.sql.exec('INSERT OR REPLACE INTO hits VALUES (?,?,?)', key, until, n);
    return { n, retryAfter: (until - now) / 1000 };
  }

  /** peek answers the count key holds now, without counting an event. */
  peek(key, now) {
    const row = this.sql.exec('SELECT until, n FROM hits WHERE key = ?', key).toArray()[0];
    return row && row.until > now ? { n: row.n, retryAfter: (row.until - now) / 1000 } : { n: 0, retryAfter: 0 };
  }
}
