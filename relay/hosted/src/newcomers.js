// NewcomerGate: the one Durable Object that counts, per caller network (see network.js), how many identities the relay
// has admitted from it. Identities cost nothing to make (there is no account and no proof of work),
// so the address is what a flood has to spread across. Only a first sight of an identity is counted:
// an existing identity is never affected, from any address.
import { DurableObject } from 'cloudflare:workers';
import { GlobalBudget } from './global.js';
import { limitsOf } from './limits.js';
import { guarded } from './wire.js';

export class NewcomerGate extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.budget = new GlobalBudget(ctx.storage.sql, env.FRAMES, limitsOf(env));
  }

  /** admit counts one new identity from ip, and refuses it past the day's share. */
  admit(ip) {
    return guarded(() => this.budget.admit(ip, Date.now()));
  }

  /** reserve bounds new frames across all identity objects before any R2 write. */
  reserve(identity, frame, bytes) {
    return guarded(() => this.budget.reserve(identity, frame, bytes));
  }

  /** forget returns capacity after an identity's erasure has completed. */
  forget(identity) {
    return guarded(() => this.budget.forget(identity));
  }
}
