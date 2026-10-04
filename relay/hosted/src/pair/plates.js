// Plates: the nameplates of the pairing mailboxes, and the one rule about reusing them. A nameplate is a
// short number a person types, so it is drawn at random from a small space and must be handed out again
// some day. When a mailbox ends, though, a device that still holds its code may come back to the name up to
// the end of the code's life; if the name had gone to a new mailbox by then, that device would find a live
// mailbox under it and wait there for an answer that cannot come. So an ended plate is quarantined: it is
// not drawn again until `plateQuarantineMs` after the mailbox ended, which is at least as long as any
// client's code lives. Quarantined plates count as occupied when the length is chosen, so they never fill
// the space. Pure over a Durable Object's sql, so every rule is tested without a relay.
import { relayFull } from './refusals.js';

const DRAWS = 64; // tries at a free plate before the space is called full; a space under a tenth taken needs one or two

/** digitsFor is the smallest nameplate length, 2 to 4, that keeps the occupied plates under a tenth of the space. */
export function digitsFor(occupied) {
  let k = 2;
  while (k < 4 && occupied * 10 >= 10 ** k) k++;
  return k;
}

const draw = (k) => crypto.getRandomValues(new Uint32Array(1))[0] % 10 ** k;

export class Plates {
  /** @param limits the relay's numbers; @param pick draws a number below 10 ** k, which a test replaces. */
  constructor(sql, limits, pick = draw) {
    this.sql = sql;
    this.limits = limits;
    this.pick = pick;
    // ends is when the mailbox ends; free is when the plate may be drawn again.
    sql.exec('CREATE TABLE IF NOT EXISTS held (np TEXT PRIMARY KEY, ends INTEGER NOT NULL, free INTEGER NOT NULL) WITHOUT ROWID');
    this.#adoptOldTable();
  }

  /** allocate reserves a plate that is neither live nor quarantined, or throws full. */
  allocate(now) {
    this.sql.exec('DELETE FROM held WHERE free <= ?', now);
    const { occupied, live } = this.sql.exec('SELECT COUNT(*) AS occupied, COALESCE(SUM(ends > ?), 0) AS live FROM held', now).one();
    if (live >= this.limits.pairMaxBoxes) throw relayFull();
    const digits = digitsFor(occupied);
    for (let i = 0; i < DRAWS; i++) {
      const np = String(this.pick(digits)).padStart(digits, '0');
      if (this.sql.exec('SELECT 1 FROM held WHERE np = ?', np).toArray().length) continue;
      const ends = now + this.limits.pairTtlMs;
      this.sql.exec('INSERT INTO held VALUES (?,?,?)', np, ends, ends + this.limits.plateQuarantineMs);
      return np;
    }
    throw relayFull();
  }

  /** release ends the mailbox under np now; the plate is free again one quarantine later. */
  release(np, now) {
    this.sql.exec('UPDATE held SET ends = ?, free = ? WHERE np = ? AND ends > ?', now, now + this.limits.plateQuarantineMs, np, now);
  }

  // #adoptOldTable carries over the plates of a relay that predates quarantine, so a deploy does not forget a live mailbox.
  #adoptOldTable() {
    const old = this.sql.exec("SELECT 1 FROM sqlite_master WHERE type = 'table' AND name = 'plates'").toArray().length;
    if (!old) return;
    this.sql.exec('INSERT OR IGNORE INTO held SELECT np, expires, expires + ? FROM plates', this.limits.plateQuarantineMs);
    this.sql.exec('DROP TABLE plates');
  }
}
