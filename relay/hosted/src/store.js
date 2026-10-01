// The frame store. A frame is one immutable R2 object named by its FrameID (the SHA-256 of its
// bytes), so a put is idempotent. The index that finds an object inside its frame lives in the
// identity's Durable Object SQLite, not in memory: rid -> (frame, offset, length), one row per
// object, so how many objects an identity holds is bounded by its byte quota and never by an
// isolate's 128 MB.
//
// One Durable Object serves one identity and is its only writer, so the index is exact. A frame
// is durable in R2 before the index learns of it; a crash between the two leaves an unindexed
// frame, and the next put of that same frame (which a client resends) indexes it.
import { decode } from './frame.js';
import { hex, sha256 } from './codec.js';
import { Serial } from './serial.js';

export class Conflict extends Error {}
export class Damaged extends Error {}
/** Unsatisfiable is a ranged get whose offset begins past the frame: the caller turns it into a refusal. */
export class Unsatisfiable extends Error {}

export const TARGET_FRAME = 1 << 20; // the size writers close a frame at, and a batch get's answer stops at

/** spansOf groups located objects by frame: the byte range to read and the objects inside it. */
function spansOf(found) {
  const spans = new Map();
  for (const at of found) {
    const span = spans.get(at.frame) ?? { from: Infinity, to: 0, refs: [] };
    span.from = Math.min(span.from, at.off);
    span.to = Math.max(span.to, at.off + at.len);
    span.refs.push(at);
    spans.set(at.frame, span);
  }
  return spans;
}

const sameBytes = (a, b) => a?.length === b.length && a.every((x, i) => x === b[i]);
const frameIdOf = async (bytes) => hex(await sha256(bytes));

/** sealFrame validates a frame and names it: what the relay must know before it stores or counts one. */
export async function sealFrame(bytes) {
  const { base, objects } = decode(bytes);
  return { id: await frameIdOf(bytes), bytes, base, objects, size: bytes.length };
}

export class R2Store {
  #writes = new Serial();

  constructor(bucket, sql, identity) {
    this.bucket = bucket;
    this.sql = sql;
    this.prefix = `${identity}/f/`;
    // WITHOUT ROWID keeps each row one row written, not a row and an index entry.
    sql.exec('CREATE TABLE IF NOT EXISTS frames (frame TEXT PRIMARY KEY) WITHOUT ROWID');
    sql.exec('CREATE TABLE IF NOT EXISTS objs (rid TEXT PRIMARY KEY, frame TEXT NOT NULL, off INTEGER NOT NULL, len INTEGER NOT NULL) WITHOUT ROWID');
  }

  /**
   * put stores a sealed frame and answers whether it was new. Puts run one at a time, so the
   * conflict check of one cannot be overtaken by the index update of another. A frame already
   * indexed is a no-op; for a new one, admit() may still refuse it (a quota) before anything is written.
   */
  put(frame, admit = () => {}) {
    return this.#writes.run(async () => {
      if (this.#indexed(frame.id)) return false;
      await this.#refuseConflicts(frame.objects);
      admit();
      await this.bucket.put(this.prefix + frame.id, frame.bytes);
      this.#index(frame);
      return true;
    });
  }

  /** get answers one object's bytes, or null. */
  async get(rid) {
    const at = this.#locate(rid);
    if (!at) return null;
    const o = await this.bucket.get(this.prefix + at.frame, { range: { offset: at.off, length: at.len } });
    return o && new Uint8Array(await o.arrayBuffer());
  }

  /**
   * getMany answers the objects of the longest prefix of rids that fits in one frame's worth of
   * bytes, in request order, stopping short at the first rid the index does not hold. It reads each
   * frame it touches once, as one ranged read from the first wanted byte to the last, so a request
   * costs one R2 read per frame and not one per object. An indexed object R2 no longer holds is damage.
   */
  async getMany(rids) {
    const found = this.#prefix(rids);
    const spans = spansOf(found);
    const reads = await Promise.all([...spans].map(([frame, span]) => this.#read(frame, span)));
    const bytes = new Map(reads.flat());
    return found.map(({ rid }) => ({ rid, bytes: bytes.get(rid) }));
  }

  /**
   * getFrame answers one frame as R2 holds it: the body is R2's own ReadableStream, so a frame is
   * piped to the client and never held whole in the isolate's memory. Absent is null. A range
   * asked is pre-checked against the frame's own end, so an unsatisfiable range is refused by
   * the index rather than classified from the wording of an R2 error, and R2 is only ever
   * asked ranges that begin inside the frame.
   */
  async getFrame(id, range) {
    if (range && this.frameEnd(id) <= range.offset) throw new Unsatisfiable();
    const o = await this.bucket.get(this.prefix + id, range && { range });
    if (!o) return null;
    return { body: o.body, size: o.size };
  }

  /**
   * frameEnd answers the last byte offset the index holds in a frame, or 0 for a frame the index
   * does not know: a frame's objects are laid end to end, so the largest end is the frame's own
   * end, and a range that begins at or past it cannot be served.
   */
  frameEnd(id) {
    return this.sql.exec('SELECT MAX(off + len) AS end FROM objs WHERE frame = ?', id).one()?.end ?? 0;
  }

  /**
   * locate answers, for each rid the index holds, where its bytes live: the frame, offset and length
   * the index recorded at put time. It is an index read only, with no freshness rule.
   */
  locate(rids) {
    const at = {};
    for (const rid of rids) {
      const row = this.#locate(rid);
      if (row) at[rid] = row;
    }
    return at;
  }

  // #prefix locates rids in order until a frame's worth of bytes is in hand or a rid is absent.
  #prefix(rids) {
    const found = [];
    let size = 0;
    for (const rid of rids) {
      const at = size < TARGET_FRAME && this.#locate(rid);
      if (!at) break;
      found.push({ rid, ...at });
      size += at.len;
    }
    return found;
  }

  // #read fetches one span of one frame and cuts the wanted objects out of it.
  async #read(frame, { from, to, refs }) {
    const o = await this.bucket.get(this.prefix + frame, { range: { offset: from, length: to - from } });
    if (!o) throw new Damaged();
    const span = new Uint8Array(await o.arrayBuffer());
    return refs.map(({ rid, off, len }) => [rid, span.subarray(off - from, off - from + len)]);
  }

  /** has answers, in order, whether each rid is stored. */
  has(rids) {
    return rids.map((rid) => this.#locate(rid) !== null);
  }

  #locate(rid) {
    return this.sql.exec('SELECT frame, off, len FROM objs WHERE rid = ?', rid).toArray()[0] ?? null;
  }

  #indexed(id) {
    return this.sql.exec('SELECT 1 FROM frames WHERE frame = ?', id).toArray().length > 0;
  }

  // A rid held with other bytes is a conflict, and a conflicting frame stores nothing of itself.
  async #refuseConflicts(objects) {
    for (const { rid, bytes } of objects) {
      if (this.#locate(rid) && !sameBytes(await this.get(rid), bytes)) throw new Conflict();
    }
  }

  // #index is one synchronous turn, so no request sees a frame half indexed. An object already held
  // (the same rid in an earlier frame, with the same bytes) keeps its first place.
  #index({ id, base, objects }) {
    this.sql.exec('INSERT OR IGNORE INTO frames VALUES (?)', id);
    for (const { rid, off, len } of objects) this.sql.exec('INSERT OR IGNORE INTO objs VALUES (?,?,?,?)', rid, id, base + off, len);
  }
}
