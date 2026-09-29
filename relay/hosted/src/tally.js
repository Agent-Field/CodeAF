// Per-identity counts kept inside the Worker: requests by route, and R2 operations by
// class and kind. Read at /_tally when the dev binding DEV_TALLY is set. R2 prices
// PUT and LIST as Class A, GET and HEAD as Class B, and delete as free.
const CLASS = { put: 'A', list: 'A', get: 'B', head: 'B', delete: 'free' };

const tallies = new Map();

export function tallyOf(identity) {
  if (!tallies.has(identity)) tallies.set(identity, { requests: {}, r2: {}, bytes_in: 0, bytes_out: 0 });
  return tallies.get(identity);
}
const bump = (map, key, n = 1) => (map[key] = (map[key] ?? 0) + n);

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

export const countRequest = (tally, route) => bump(tally.requests, route);
export const snapshot = () => Object.fromEntries(tallies);
export const reset = () => tallies.clear();
