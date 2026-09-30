// The frame store on R2. A frame is one immutable object named by its FrameID (the SHA-256
// of its bytes), so a put is idempotent. Nothing writes a pointer per object: a rid is found
// through an index derived from the frames' headers, saved as one snapshot object.
// The frames are the truth; deleting the snapshot loses nothing.
//
// One Durable Object serves one identity and is the only writer of its frames, so once the
// index is loaded it stays exact: puts add to it, and a miss means "not stored", with no
// re-listing. Loading is single-flight, so a burst of first requests builds it once.
import { decode, headerRefs } from './frame.js';
import { hex, sha256 } from './codec.js';
import { Lazy } from './lazy.js';
import { Serial } from './serial.js';

const FRONT = 64 << 10; // first read of a frame: enough for almost every header
// A snapshot is a write and a frame header is only a read, so a load re-reads a few headers
// rather than save the snapshot each time it finds one new frame.
const SNAPSHOT_AFTER = 32;

const frameIdOf = async (bytes) => hex(await sha256(bytes));

export class Conflict extends Error {}

const sameBytes = (a, b) => a?.length === b.length && a.every((x, i) => x === b[i]);

/** sealFrame validates a frame and names it: what the relay must know before it stores or counts one. */
export async function sealFrame(bytes) {
  const { base, objects } = decode(bytes);
  return { id: await frameIdOf(bytes), bytes, base, objects, size: bytes.length };
}

async function readRange(bucket, key, length) {
  const o = await bucket.get(key, { range: { offset: 0, length } });
  return new Uint8Array(await o.arrayBuffer());
}

/** headerOf reads one stored frame's object refs with one ranged read, or two for a huge header. */
async function headerOf(bucket, key) {
  const front = await readRange(bucket, key, FRONT);
  const seen = headerRefs(front);
  return seen.need ? headerRefs(await readRange(bucket, key, seen.need)) : seen;
}

/** Index maps rid -> [frame, offset, length] for the frames it holds. */
class Index {
  constructor(snapshot) {
    this.frames = new Set(snapshot?.frames ?? []);
    this.objs = new Map(Object.entries(snapshot?.objs ?? {}));
  }

  add(frame, base, refs) {
    this.frames.add(frame);
    for (const r of refs) this.objs.set(r.rid, [frame, base + r.off, r.len]);
  }

  toJSON() {
    return { frames: [...this.frames], objs: Object.fromEntries(this.objs) };
  }
}

export class R2Store {
  #index = new Lazy(() => this.#load());
  #writes = new Serial();

  constructor(bucket, identity) {
    this.bucket = bucket;
    this.prefix = `${identity}/f/`;
    this.snapshotKey = `${identity}/ix`;
  }

  /**
   * put stores a sealed frame and answers whether it was new. Puts run one at a time, so the
   * conflict check and the index update of one cannot be overtaken by another. A frame the
   * store already holds is a no-op; for a new one, admit() may still refuse it (a quota) before
   * anything is written. The frame is durable in R2 before the index learns of it.
   */
  put(frame, admit = () => {}) {
    return this.#writes.run(async () => {
      const index = await this.#index.get();
      if (index.frames.has(frame.id)) return false;
      await this.#refuseConflicts(index, frame.objects);
      admit();
      await this.bucket.put(this.prefix + frame.id, frame.bytes);
      index.add(frame.id, frame.base, frame.objects);
      return true;
    });
  }

  /** get answers one object's bytes, or null. */
  async get(rid) {
    const at = (await this.#index.get()).objs.get(rid);
    if (!at) return null;
    const [frame, offset, length] = at;
    const o = await this.bucket.get(this.prefix + frame, { range: { offset, length } });
    return o && new Uint8Array(await o.arrayBuffer());
  }

  /** has answers, in order, whether each rid is stored. */
  async has(rids) {
    const { objs } = await this.#index.get();
    return rids.map((r) => objs.has(r));
  }

  // A rid held with other bytes is a conflict, and a conflicting frame stores nothing of itself.
  async #refuseConflicts(index, objects) {
    for (const { rid, bytes } of objects) {
      if (index.objs.has(rid) && !sameBytes(await this.get(rid), bytes)) throw new Conflict();
    }
  }

  /** #load reads the snapshot, indexes the frames it has not seen, and saves the snapshot again once enough are new. */
  async #load() {
    const snap = await this.bucket.get(this.snapshotKey);
    const index = new Index(snap && JSON.parse(await snap.text()));
    let added = 0;
    for await (const key of this.#frameKeys()) {
      const frame = key.slice(this.prefix.length);
      if (index.frames.has(frame)) continue;
      const { base, refs } = await headerOf(this.bucket, key);
      index.add(frame, base, refs);
      added++;
    }
    if (added >= SNAPSHOT_AFTER) await this.bucket.put(this.snapshotKey, JSON.stringify(index));
    return index;
  }

  async *#frameKeys() {
    let cursor;
    do {
      const page = await this.bucket.list({ prefix: this.prefix, cursor });
      for (const o of page.objects) yield o.key;
      cursor = page.truncated ? page.cursor : undefined;
    } while (cursor);
  }
}
