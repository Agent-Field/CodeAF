// NewcomerGate: the one Durable Object that counts, per caller IP, how many identities the relay
// has admitted from it. Identities cost nothing to make (there is no account and no proof of work),
// so the address is what a flood has to spread across. Only a first sight of an identity is counted:
// an existing identity is never affected, from any address.
import { DurableObject } from 'cloudflare:workers';
import { Counters } from './counters.js';
import { limitsOf } from './limits.js';
import { guarded, tooManyIdentities } from './wire.js';

const DAY_MS = 86_400_000;

export class NewcomerGate extends DurableObject {
  constructor(ctx, env) {
    super(ctx, env);
    this.limit = limitsOf(env).newIdentitiesPerIpPerDay;
    this.counters = new Counters(ctx.storage.sql);
  }

  /** admit counts one new identity from ip, and refuses it past the day's share. */
  admit(ip) {
    return guarded(() => {
      const { n, retryAfter } = this.counters.hit(`new:${ip}`, DAY_MS, Date.now());
      if (n > this.limit) throw tooManyIdentities(retryAfter);
    });
  }
}
