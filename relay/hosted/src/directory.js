// The directory of one identity, in its Durable Object's SQLite. The object is single-threaded
// and every method here is one synchronous turn (no await between a read and the write that
// depends on it), so nothing can interleave inside a swap: that is the compare-and-swap the
// lease rules need. The rules themselves are pure and live in rules.js.
import { makeRules, RuleError } from './rules.js';

const refuse = (code) => new RuleError(code);
const present = (c) => {
  if (!c) throw refuse('not_found');
  return c;
};

export class Directory {
  constructor(sql, identity, clock, policy) {
    this.sql = sql;
    this.identity = identity;
    this.clock = clock;
    this.rules = makeRules(policy);
    sql.exec('CREATE TABLE IF NOT EXISTS dir (kind TEXT NOT NULL, id TEXT NOT NULL, doc TEXT NOT NULL, PRIMARY KEY (kind, id)) WITHOUT ROWID');
  }

  read(kind, id) {
    const row = this.sql.exec('SELECT doc FROM dir WHERE kind = ? AND id = ?', kind, id).toArray()[0];
    return row ? JSON.parse(row.doc) : null;
  }

  write(kind, id, doc) {
    this.sql.exec('INSERT OR REPLACE INTO dir VALUES (?,?,?)', kind, id, JSON.stringify(doc));
    return doc;
  }

  /** swap moves one cell through a rule and answers {now, cell}; a refusing rule leaves it untouched. */
  swap(id, rule) {
    const now = this.clock();
    return { now, cell: this.write('cells', id, rule(this.read('cells', id), now)) };
  }

  change(id, rule) {
    return this.swap(id, (c, now) => rule(present(c), now));
  }

  cell(id) {
    return { now: this.clock(), cell: present(this.read('cells', id)) };
  }

  create(id, init, device) {
    return this.swap(id, (c, now) => {
      if (c) throw refuse('exists');
      return this.rules.created(init, device, now);
    });
  }

  acquire(id, device, force = false) {
    return this.change(id, (c, now) => this.rules.acquire(c, device, now, force));
  }

  heartbeat(id, device, beat) {
    return this.change(id, (c, now) => this.rules.heartbeat(c, device, beat, now));
  }

  publish(id, device, p) {
    return this.change(id, (c, now) => this.rules.publishTo(c, device, p, now));
  }

  release(id, device, fence) {
    this.change(id, (c) => this.rules.releaseOf(c, device, fence));
  }

  archive(id) {
    this.change(id, (c) => ({ ...c, archived: true }));
  }

  // A device's record says whether it is revoked, and the device cannot change that: only revoke can.
  putDevice(id, device) {
    this.write('devices', id, { ...device, revoked: this.revoked(id) });
  }

  /**
   * revoke stops the device `id` on behalf of `caller`. A device cannot stop itself, which is nearly
   * always a slip and a stopped device cannot say so, and revoking twice changes nothing.
   */
  revoke(id, caller) {
    const record = this.read('devices', id);
    if (!record) throw refuse('not_found');
    if (id === caller) throw refuse('self_revoke');
    this.write('devices', id, { ...record, revoked: true });
  }

  /** revoked says whether the identity's own record of this device has turned it away. */
  revoked(device) {
    return this.read('devices', device)?.revoked === true;
  }

  #identityRec() {
    return this.read('identity', '') ?? { V: 1, identity: this.identity };
  }

  setVault(old, next) {
    const cur = this.#identityRec();
    if ((cur.vault ?? '') !== old) throw refuse('cas');
    this.write('identity', '', { ...cur, vault: next });
  }

  /** rotation answers what the identity's record says about its replacement; undefined while it is live. */
  rotation() {
    return this.#identityRec().rotation;
  }

  setRotation(rotation) {
    this.write('identity', '', { ...this.#identityRec(), rotation });
  }

  /** list answers the whole Listing: the identity record, every device and every cell. */
  list() {
    const out = { now: this.clock(), identity: { V: 1, identity: this.identity }, devices: {}, cells: {} };
    for (const { kind, id, doc } of this.sql.exec('SELECT kind, id, doc FROM dir').toArray()) {
      if (kind === 'identity') out.identity = JSON.parse(doc);
      else out[kind][id] = JSON.parse(doc);
    }
    return out;
  }
}
