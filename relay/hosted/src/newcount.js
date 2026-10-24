// The newcomer count itself, apart from the Durable Object that keeps it, so it runs against any SQLite.
import { tooManyIdentities } from './wire.js';

/** The day a network's count lasts: it starts at that network's first new identity and ends a day later. */
export const DAY_MS = 86_400_000;

/**
 * countNewcomer counts one new identity from network at now, and throws too_many_identities past limit. The
 * refusal carries the seconds left in the network's own day, which is when the count starts over.
 */
export function countNewcomer(counters, limit, network, now) {
  const { n, retryAfter } = counters.hit(`new:${network}`, DAY_MS, now);
  if (n > limit) throw tooManyIdentities(retryAfter);
}
