// LinkRequest: one Durable Object per pending request, like Mailbox is one per pairing. It holds the
// record of docs/ux-pairing-contract.md section 1 and moves its state once: pending to approved or
// denied. It stores the grant as opaque bytes and never reads it. An alarm deletes the record: at the
// expiry while pending, or a short while after a decision, so the new device can still read it.
import { DurableObject } from 'cloudflare:workers';
import { limitsOf } from '../limits.js';
import { guarded } from '../wire.js';
import { settled } from './state.js';
import { Serial } from '../serial.js';
import { boundLink, deliveredLink } from './effects.js';
import { alreadyDecided } from './refusals.js';
import { gone } from './refusals.js';

export class LinkRequest extends DurableObject {
  #record = null;
  #changes = new Serial();
  #waiting = new Set(); // wake functions of the long polls now waiting

  constructor(ctx, env) {
    super(ctx, env);
    this.limits = limitsOf(env);
    ctx.blockConcurrencyWhile(async () => {
      this.#record = (await ctx.storage.get('record')) ?? null;
    });
  }

  /** open stores the new record, pending, and arms its deletion at the expiry. */
  open(record) {
    return guarded(() => this.#changes.run(async () => {
      if (this.#record) throw alreadyDecided();
      const next = { ...record, state: 'pending', grant: null, decided_at: 0 };
      await this.#save(next, record.expires_at);
    }));
  }

  /** read answers the record, waiting up to waitMs for it to be decided; null when the wait ends first. */
  read(waitMs) {
    return guarded(async () => {
      if (this.#live().state === 'pending' && waitMs > 0) await this.#untilWoken(waitMs);
      const record = this.#live();
      return record.state === 'pending' && waitMs > 0 ? null : viewOf(record);
    });
  }

  /** decide moves a pending request to `state` once; see state.js for what a repeat does. */
  decide(state, grant) {
    return guarded(() => this.#changes.run(async () => {
      const record = this.#live();
      if (record.cluster) throw alreadyDecided();
      const next = settled(record, state, grant, Date.now());
      await this.#save(next, next.decided_at + this.limits.linkDecidedKeepMs);
      this.#wake();
    }));
  }

  /** bind reserves a request before its immutable dependency enters the log. */
  bind(cluster) {
    return guarded(() => this.#changes.run(async () => {
      const record = this.#live(), next = boundLink(record, cluster);
      if (next !== record) { await this.ctx.storage.put('record', next); this.#record = next; }
      return viewOf(next);
    }));
  }

  /** deliver accepts only an internal committed outbox response. */
  deliver(cluster, id, state, grant, device) {
    return guarded(() => this.#changes.run(async () => {
      const record = this.#live(), next = deliveredLink(record, cluster, id, state, grant, Date.now(), device);
      await this.#save(next, next.decided_at + this.limits.linkDecidedKeepMs);
      this.#wake();
    }));
  }

  // The record and its expiry share a transaction so a crash cannot retain
  // an undeliverable grant forever or delete one before its promised window.
  async #save(record, deadline) {
    await this.ctx.storage.transaction(async tx => {
      await tx.put('record', record);
      await tx.setAlarm(deadline);
    });
    this.#record = record;
  }

  async alarm() {
    return this.#changes.run(async () => {
      await this.ctx.storage.deleteAll();
      this.#record = null;
      this.#wake();
    });
  }

  // A pending record is dead at its expiry; a decided one lives until its alarm, so the new device can read it.
  #live() {
    const r = this.#record;
    if (!r || (r.state === 'pending' && Date.now() >= r.expires_at)) throw gone();
    return r;
  }

  #wake() {
    for (const wake of this.#waiting) wake();
  }

  #untilWoken(ms) {
    return new Promise((resolve) => {
      const done = () => (clearTimeout(timer), this.#waiting.delete(done), resolve());
      const timer = setTimeout(done, ms);
      this.#waiting.add(done);
    });
  }
}

/** viewOf is the record as the wire shows it: the stored fields except the relay's own bookkeeping. */
const viewOf = ({ decided_at: _, cluster: _cluster, delivery: _delivery, ...record }) => record;
