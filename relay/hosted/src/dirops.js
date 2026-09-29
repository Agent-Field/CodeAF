// The directory's operations, independent of where records live. A store supplies
//   swap(id, fn(current|null, now) -> next)   move one cell atomically, answer {now, cell}
//   record(kind, id, fn(current|null) -> next) move one device or identity record atomically
//   readCell(id) -> cell|null,  list() -> Listing
// and this class supplies the rules.
import { STAGE1, makeRules, RuleError } from './rules.js';

export const refuse = (code) => new RuleError(code);
const present = (c) => {
  if (!c) throw refuse('not_found');
  return c;
};

export class DirectoryOps {
  constructor(identity, clock = Date.now, policy = STAGE1) {
    this.identity = identity;
    this.clock = clock;
    this.rules = makeRules(policy);
  }

  change(id, rule) {
    return this.swap(id, (c, now) => rule(present(c), now));
  }

  async cell(id) {
    return { now: this.clock(), cell: present(await this.readCell(id)) };
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
  async release(id, device, fence) {
    await this.change(id, (c) => this.rules.releaseOf(c, device, fence));
  }
  async archive(id) {
    await this.change(id, (c) => ({ ...c, archived: true }));
  }

  async putDevice(id, device) {
    await this.record('devices', id, () => device);
  }

  async setVault(old, next) {
    await this.record('identity', undefined, (rec) => {
      const cur = rec ?? { V: 1, identity: this.identity };
      if ((cur.vault ?? '') !== old) throw refuse('cas');
      return { ...cur, vault: next };
    });
  }

  /** assemble builds the Listing from [kind, id, doc] triples. */
  assemble(triples) {
    const out = { now: this.clock(), identity: { V: 1, identity: this.identity }, devices: {}, cells: {} };
    const into = { cells: out.cells, devices: out.devices };
    for (const [kind, id, doc] of triples) {
      if (kind === 'identity') out.identity = doc;
      else into[kind][id] = doc;
    }
    return out;
  }
}
