// Lease liveness from watch sockets (contract 21.11). A lease is live while its stored expiry lies
// ahead, or while a socket of the holder's device, which named this cell at this fence, has shown
// a sign of life within the lease TTL. Everything here is pure: the sockets are an input, so the
// lease rules stay free of any clock or store, and one function alone decides what a socket is worth.
import { Wire } from './wire.js';

const MAX_CELL_BYTES = 64; // keeps the attachment of a full hold list far below the platform's 2 KiB
const FENCE = /^[0-9]{1,16}$/;
const encoder = new TextEncoder();

/** hold parses one `hold` query value, `<cell>:<fence>`, split at the last colon; anything else is a bad request. */
function parseHold(value) {
  const at = value.lastIndexOf(':');
  const cell = at < 0 ? '' : value.slice(0, at);
  const digits = value.slice(at + 1);
  if (at < 0 || !cell || encoder.encode(cell).length > MAX_CELL_BYTES || !FENCE.test(digits)) throw new Wire('bad_request', 400);
  const fence = Number(digits);
  if (!Number.isSafeInteger(fence)) throw new Wire('bad_request', 400);
  return [cell, fence];
}

/** parseHolds reads the holds of a watch upgrade as distinct [cell, fence] pairs, at most `max` of them. */
export function parseHolds(values, max) {
  if (values.length > max) throw new Wire('bad_request', 400);
  const seen = new Map(values.map((v) => [v, parseHold(v)]));
  return [...seen.values()];
}

/**
 * vouchedUntil is the time until which the sockets vouch for the lease of `cell` at `fence`: the newest sign
 * of life among those that named it, plus the TTL, or 0 when none did. A socket is {holds, seenAt}; the
 * caller has already narrowed the sockets to the lease's device, which no client names for itself.
 */
export function vouchedUntil(sockets, cell, fence, ttlMs) {
  const seen = sockets.filter((s) => s.holds.some(([c, f]) => c === cell && f === fence)).map((s) => s.seenAt);
  return seen.length ? Math.max(...seen) + ttlMs : 0;
}

/**
 * lifted is the lease as every reader must see it: its expiry raised to the vouched time when that is later.
 * A released lease (expiry 0) stays free however many sockets still name it.
 */
export function lifted(c, until) {
  if (!c || c.lease.expires === 0 || until <= c.lease.expires) return c;
  return { ...c, lease: { ...c.lease, expires: until } };
}
