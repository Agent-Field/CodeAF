// The relay's own count of what an identity asked of the store (contract 3, /v1/store/stats).
// Counts change on every request, and a row written per request would be the dearest line of the
// bill, so they are kept in memory and written in batches: the first change after a write asks for
// a flush a few seconds later (an alarm, which outlives the object's memory), and that flush
// writes everything counted since. What a platform crash can lose is those few seconds.
const ZERO = { puts: 0, gets: 0, has: 0, bytes_in: 0, bytes_out: 0 };
const KEY = 'stats';

export class Stats {
  #counts;
  #dirty = false;

  /** schedule asks the platform to call flush() in a few seconds. */
  constructor(meta, schedule) {
    this.meta = meta;
    this.schedule = schedule;
    this.#counts = { ...ZERO, ...meta.get(KEY) };
  }

  /** add folds in a change, e.g. add({puts: 1, bytes_in: 300}). */
  add(change) {
    for (const [name, n] of Object.entries(change)) this.#counts[name] += n;
    if (!this.#dirty) {
      this.#dirty = true;
      this.schedule();
    }
  }

  snapshot() {
    return { ...this.#counts };
  }

  /** flush writes the counts if any changed since the last write. */
  flush() {
    if (!this.#dirty) return;
    this.#dirty = false;
    this.meta.set(KEY, this.#counts);
  }
}
