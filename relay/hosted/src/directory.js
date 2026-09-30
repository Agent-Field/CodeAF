// The directory of one identity, in its Durable Object's SQLite. The object is single-threaded
// and every method here is one synchronous turn (no await between a read and the write that
// depends on it), so nothing can interleave inside a swap: that is the compare-and-swap the
// lease rules need. The rules themselves are pure and live in rules.js.
import { makeRules, RuleError } from './rules.js';
import { CLOSE_REVOKED, CLOSE_ROTATED, NOBODY } from './watch.js';

const refuse = (code) => new RuleError(code);
const present = (c) => {
  if (!c) throw refuse('not_found');
  return c;
};

// Key order must not decide whether two records are equal, so the comparison is over sorted keys.
const sorted = (_, x) => (x && typeof x === 'object' && !Array.isArray(x) ? Object.fromEntries(Object.entries(x).sort(([a], [b]) => (a < b ? -1 : 1))) : x);
const canonical = (v) => JSON.stringify(v, sorted);

export class Directory {
  /** `watchers` is told of every visible change and of the closes a revoke or a rotation asks for (contract 21). */
  constructor(sql, identity, clock, policy, watchers = NOBODY) {
    this.sql = sql;
    this.identity = identity;
    this.clock = clock;
    this.rules = makeRules(policy);
    this.watchers = watchers;
    sql.exec('CREATE TABLE IF NOT EXISTS dir (kind TEXT NOT NULL, id TEXT NOT NULL, doc TEXT NOT NULL, PRIMARY KEY (kind, id)) WITHOUT ROWID');
    sql.exec('CREATE TABLE IF NOT EXISTS dirver (one INTEGER PRIMARY KEY CHECK (one = 1), n INTEGER NOT NULL)');
    sql.exec('INSERT OR IGNORE INTO dirver VALUES (1, 0)');
    this.version = sql.exec('SELECT n FROM dirver').one().n;
  }

  read(kind, id) {
    const row = this.sql.exec('SELECT doc FROM dir WHERE kind = ? AND id = ?', kind, id).toArray()[0];
    return row ? JSON.parse(row.doc) : null;
  }

  /** write stores a record, and counts a change of it that a person could see (see #visible) as a new version. */
  write(kind, id, doc) {
    const before = this.read(kind, id);
    this.sql.exec('INSERT OR REPLACE INTO dir VALUES (?,?,?)', kind, id, JSON.stringify(doc));
    if (!before || this.#visible(kind, before) !== this.#visible(kind, doc)) this.#bump();
    return doc;
  }

  // What the home list shows of a record: all of it, except that a lease shows only whether it is held,
  // never when it runs out, so a heartbeat that moves the expiry alone is not a change (contract 21.4).
  #visible(kind, doc) {
    if (kind !== 'cells') return canonical(doc);
    return canonical({ ...doc, lease: { ...doc.lease, expires: doc.lease.expires > this.clock() } });
  }

  /** #bump moves the durable version up by one in the turn that made the change, then tells every socket. */
  #bump() {
    this.version += 1;
    this.sql.exec('UPDATE dirver SET n = ?', this.version);
    this.watchers.broadcast(this.version);
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
    this.watchers.closeDevice(id, CLOSE_REVOKED, 'revoked');
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
    if (rotation) this.watchers.closeAll(CLOSE_ROTATED, 'rotated');
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
