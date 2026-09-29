// The directory on R2 alone: one small object per record, moved by conditional put.
import { DirectoryOps, refuse } from './dirops.js';

const RETRIES = 8;
const META_LIMIT = 8192; // R2 custom metadata limit; a record over it is refused as too_large

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
export class R2Directory extends DirectoryOps {
  constructor(bucket, identity, clock, policy) {
    super(identity, clock, policy);
    this.bucket = bucket;
  }

  get prefix() {
    return `${this.identity}/dir/`;
  }

  key(kind, id) {
    return id === undefined ? this.prefix + kind : `${this.prefix}${kind}/${id}`;
  }

  async swap(id, fn) {
    let now;
    const cell = await cas(this.bucket, this.key('cells', id), (c) => fn(c, (now = this.clock())));
    return { now, cell };
  }

  record(kind, id, fn) {
    return cas(this.bucket, this.key(kind, id), fn);
  }

  async readCell(id) {
    const held = await this.bucket.get(this.key('cells', id));
    return held && JSON.parse(await held.text());
  }

  /** list answers the whole directory from each record's metadata copy: no body read. */
  async list() {
    const triples = [];
    for await (const o of this.records()) {
      const [kind, id] = o.key.slice(this.prefix.length).split('/');
      triples.push([kind, id, JSON.parse(o.customMetadata.doc)]);
    }
    return this.assemble(triples);
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
