// NewcomerGate: the one Durable Object that counts, per caller network (see network.js), how many identities the relay
// has admitted from it. Identities cost nothing to make (there is no account and no proof of work),
// so the address is what a flood has to spread across. Only a first sight of an identity is counted:
// an existing identity is never affected, from any address.
import { DurableObject } from 'cloudflare:workers';
import { Counters } from './counters.js';
import { limitsOf } from './limits.js';
import { countNewcomer } from './newcount.js';
import { guarded } from './wire.js';

export class NewcomerGate extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.limit = limitsOf(env).newIdentitiesPerIpPerDay;
    this.counters = new Counters(ctx.storage.sql);
  }

  /** admit counts one new identity from ip, and refuses it past the day's share. */
  admit(ip) {
    return guarded(() => countNewcomer(this.counters, this.limit, ip, Date.now()));
  }
}
