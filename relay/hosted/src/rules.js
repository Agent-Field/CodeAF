// Port of internal/directory rules.go: pure, no clock, no store.
export const LEASE_TTL_MS = 30_000;

/** Stage 1 lease law, and the Stage 1H amendment: a publish is proof of life and the TTL is longer. */
export const STAGE1 = { ttlMs: LEASE_TTL_MS, renewOnPublish: false };
export const AMENDED = { ttlMs: 90_000, renewOnPublish: true };

export class RuleError extends Error {
  constructor(code) {
    super(code);
    this.code = code;
  }
}
const refuse = (code) => new RuleError(code);

function holder(c, device, fence) {
  if (c.lease.fence !== fence || c.lease.device !== device) throw refuse('fence_stale');
}

/** makeRules binds the pure lease rules to one policy. Nothing here reads a clock or a store. */
export function makeRules({ ttlMs, renewOnPublish }) {
  // force is the amendment: a person who chose "continue here" takes a live lease at once.
  // The fence still goes up, so the old holder's next write is refused as before.
  const acquire = (c, device, now, force = false) => {
    if (!force && c.lease.expires > now && c.lease.device !== device) throw refuse('lease_held');
    return { ...c, lease: { device, fence: c.lease.fence + 1, expires: now + ttlMs, pending: 0 } };
  };

  const heartbeat = (c, device, b, now) => {
    holder(c, device, b.fence);
    return { ...c, lease: { ...c.lease, expires: now + ttlMs, pending: b.pending } };
  };

  const publishTo = (c, device, p, now) => {
    holder(c, device, p.fence);
    if (p.old_head !== c.head) throw refuse('head_moved');
    const title = p.title === '' || p.title === undefined ? c.title : p.title;
    const expires = renewOnPublish ? now + ttlMs : c.lease.expires;
    const next = { ...c, head: p.head, size: p.size, class: p.class, durable_at: now, lease: { ...c.lease, expires, pending: p.pending } };
    return title === undefined ? next : { ...next, title };
  };

  const releaseOf = (c, device, fence) => {
    holder(c, device, fence);
    return { ...c, lease: { ...c.lease, expires: 0, pending: 0 } };
  };

  const created = (init, device, now) => {
    const c = { V: 1, head: init.head, durable_at: now, class: init.class, size: init.size, keys: init.keys,
      lease: { device, fence: 1, expires: now + ttlMs, pending: 0 } };
    return omitEmpty(c, { parent_cell: init.parent_cell, title: init.title, orphan_turns: init.orphan_turns });
  };

  return { acquire, heartbeat, publishTo, releaseOf, created };
}

const omitEmpty = (c, optional) =>
  Object.assign(c, Object.fromEntries(Object.entries(optional).filter(([, v]) => v !== undefined && v !== '' && v !== 0)));

export const { acquire, heartbeat, publishTo, releaseOf, created } = makeRules(STAGE1);
