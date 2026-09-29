// The directory on R2 alone: one small object per record, moved by conditional put.
// The pure rules (rules.js) decide; this file only reads, applies and swaps.
import * as rules from './rules.js';

const RETRIES = 8;
const META_LIMIT = 8192; // R2 custom metadata limit; a record over it is refused as too_large

const refuse = (code) => new rules.RuleError(code);
const present = (c) => {
  if (!c) throw refuse('not_found');
  return c;
};

/** cas moves one record: read it with its etag, apply fn, put only if nobody moved it. */
async function cas(bucket, key, fn) {
  for (let attempt = 0; attempt < RETRIES; attempt++) {
    const held = await bucket.get(key);
    const next = fn(held ? JSON.parse(await held.text()) : null);
    const doc = JSON.stringify(next);
    if (doc.length > META_LIMIT) throw refuse('too_large');
    const onlyIf = held ? { etagMatches: held.etag } : { etagDoesNotMatch: '*' };
    if (await bucket.put(key, doc, { onlyIf, customMetadata: { doc } })) return next;
  }
  throw refuse('cas');
}

/** R2Directory is one identity's directory. clock answers directory milliseconds. */
export class R2Directory {
  constructor(bucket, identity, clock = Date.now) {
    this.bucket = bucket;
    this.identity = identity;
    this.clock = clock;
  }

  get prefix() {
    return `${this.identity}/dir/`;
  }

  key(kind, id) {
    return id === undefined ? this.prefix + kind : `${this.prefix}${kind}/${id}`;
  }

  /** swap moves a cell by fn(current or null) and answers the CellView it wrote. */
  async swap(id, fn) {
    let now;
    const cell = await cas(this.bucket, this.key('cells', id), (c) => fn(c, (now = this.clock())));
    return { now, cell };
  }

  change(id, rule) {
    return this.swap(id, (c, now) => rule(present(c), now));
  }

  async cell(id) {
    const held = await this.bucket.get(this.key('cells', id));
    return { now: this.clock(), cell: JSON.parse(await present(held).text()) };
  }

  create(id, init, device) {
    return this.swap(id, (c, now) => {
      if (c) throw refuse('exists');
      return rules.created(init, device, now);
    });
  }

  acquire(id, device, force = false) {
    return this.change(id, (c, now) => rules.acquire(c, device, now, force));
  }
  heartbeat(id, device, beat) {
    return this.change(id, (c, now) => rules.heartbeat(c, device, beat, now));
  }
  publish(id, device, p) {
    return this.change(id, (c, now) => rules.publishTo(c, device, p, now));
  }
  async release(id, device, fence) {
    await this.change(id, (c) => rules.releaseOf(c, device, fence));
  }
  async archive(id) {
    await this.change(id, (c) => ({ ...c, archived: true }));
  }

  async putDevice(id, device) {
    await cas(this.bucket, this.key('devices', id), () => device);
  }

  async setVault(old, next) {
    await cas(this.bucket, this.key('identity'), (rec) => {
      const cur = rec ?? { V: 1, identity: this.identity };
      if ((cur.vault ?? '') !== old) throw refuse('cas');
      return { ...cur, vault: next };
    });
  }

  /** list answers the whole directory: every record's metadata copy, no body read. */
  async list() {
    const out = { now: this.clock(), identity: { V: 1, identity: this.identity }, devices: {}, cells: {} };
    const into = { cells: out.cells, devices: out.devices };
    for await (const o of this.records()) {
      const doc = JSON.parse(o.customMetadata.doc);
      const [kind, id] = o.key.slice(this.prefix.length).split('/');
      if (kind === 'identity') out.identity = doc;
      else into[kind][id] = doc;
    }
    return out;
  }

  async *records() {
    let cursor;
    do {
      const page = await this.bucket.list({ prefix: this.prefix, cursor, include: ['customMetadata'] });
      yield* page.objects;
      cursor = page.truncated ? page.cursor : undefined;
    } while (cursor);
  }
}
