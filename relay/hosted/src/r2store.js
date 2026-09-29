// The frame store on R2 alone. A frame is one immutable object named by its FrameID.
// Nothing writes a pointer per object: a rid is found through a derived index, built
// from frame headers on a miss and saved as one snapshot object. The frames are the
// truth; deleting the snapshot loses nothing.
import { decode, headerRefs } from './frame.js';

const FRONT = 64 << 10; // first read of a frame: enough for almost every header

const hex = (buf) => [...new Uint8Array(buf)].map((b) => b.toString(16).padStart(2, '0')).join('');
export const frameIdOf = async (bytes) => hex(await crypto.subtle.digest('SHA-256', bytes));

/** headerOf reads one frame's object refs with one ranged read, or two for a huge header. */
async function headerOf(bucket, key) {
  let front = await readRange(bucket, key, FRONT);
  let seen = headerRefs(front);
  if (seen.need) seen = headerRefs((front = await readRange(bucket, key, seen.need)));
  return seen;
}

async function readRange(bucket, key, length) {
  const o = await bucket.get(key, { range: { offset: 0, length } });
  return new Uint8Array(await o.arrayBuffer());
}

/** Index maps rid -> [frame, offset, length] for the frames it has read. */
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

const cache = new Map(); // identity -> Index; only a hint, every miss re-lists

export class R2Store {
  constructor(bucket, identity) {
    this.bucket = bucket;
    this.identity = identity;
  }

  frameKey(id) {
    return `${this.identity}/f/${id}`;
  }
  get snapshotKey() {
    return `${this.identity}/ix`;
  }

  /** put validates the frame, then stores it. A frame durable in R2 is the whole write. */
  async put(bytes) {
    const { objects } = decode(bytes);
    const frame = await frameIdOf(bytes);
    await this.bucket.put(this.frameKey(frame), bytes);
    return { frame, objects: objects.length };
  }

  async find(rid) {
    return (await this.index()).objs.get(rid) ?? (await this.refresh()).objs.get(rid);
  }

  /** get answers one object's bytes, or null. */
  async get(rid) {
    const at = await this.find(rid);
    if (!at) return null;
    const [frame, offset, length] = at;
    const o = await this.bucket.get(this.frameKey(frame), { range: { offset, length } });
    return o && new Uint8Array(await o.arrayBuffer());
  }

  /** has answers in order. A miss triggers one refresh, so "no" always means "not in R2 now". */
  async has(rids) {
    let index = await this.index();
    if (!rids.every((r) => index.objs.has(r))) index = await this.refresh();
    return rids.map((r) => index.objs.has(r));
  }

  async index() {
    if (cache.has(this.identity)) return cache.get(this.identity);
    const snap = await this.bucket.get(this.snapshotKey);
    return this.remember(new Index(snap && JSON.parse(await snap.text())));
  }

  remember(index) {
    cache.set(this.identity, index);
    return index;
  }

  /** refresh reads the header of every frame the index has not seen and saves the snapshot. */
  async refresh() {
    const index = await this.index();
    let added = 0;
    for await (const key of this.frameKeys()) {
      const frame = key.slice(`${this.identity}/f/`.length);
      if (index.frames.has(frame)) continue;
      const { base, refs } = await headerOf(this.bucket, key);
      index.add(frame, base, refs);
      added++;
    }
    if (added) await this.bucket.put(this.snapshotKey, JSON.stringify(index));
    return index;
  }

  async *frameKeys() {
    let cursor;
    do {
      const page = await this.bucket.list({ prefix: `${this.identity}/f/`, cursor });
      for (const o of page.objects) yield o.key;
      cursor = page.truncated ? page.cursor : undefined;
    } while (cursor);
  }
}

export const forgetIndexes = () => cache.clear();
