// Counts kept per request and logged as one line, so they survive isolates and Durable Object
// eviction: R2 operations by class and kind, request route, bytes. `wrangler tail` collects the lines.
// R2 prices PUT and LIST as Class A, GET and HEAD as Class B, and delete as free.
const CLASS = { put: 'A', list: 'A', get: 'B', head: 'B', delete: 'free' };

const totals = new Map(); // dev only: identity -> running sum, read at /_tally
const bump = (map, key, n = 1) => (map[key] = (map[key] ?? 0) + n);

export const newTally = () => ({ r2: {}, bytes_in: 0, bytes_out: 0 });

/** counted wraps an R2 bucket so every call lands in tally.r2 as "<kind>:<class>". */
export function counted(bucket, tally) {
  return new Proxy(bucket, {
    get(target, kind) {
      const fn = target[kind];
      if (typeof fn !== 'function' || !(kind in CLASS)) return fn?.bind(target);
      return (...args) => (bump(tally.r2, `${kind}:${CLASS[kind]}`), fn.apply(target, args));
    },
  });
}

/** record folds one finished request into the dev totals and answers its log line. */
export function record(identity, route, tally, where) {
  const sum = totals.get(identity) ?? { requests: {}, r2: {}, bytes_in: 0, bytes_out: 0 };
  totals.set(identity, sum);
  bump(sum.requests, route);
  for (const [k, n] of Object.entries(tally.r2)) bump(sum.r2, k, n);
  sum.bytes_in += tally.bytes_in;
  sum.bytes_out += tally.bytes_out;
  return JSON.stringify({ t: 'caf', where, id: identity, route, r2: tally.r2, in: tally.bytes_in, out: tally.bytes_out });
}

export const totalsOf = (identity) => totals.get(identity) ?? { requests: {}, r2: {}, bytes_in: 0, bytes_out: 0 };
export const snapshot = () => Object.fromEntries(totals);
export const reset = () => totals.clear();
