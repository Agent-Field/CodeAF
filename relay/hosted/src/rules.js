// Port of internal/directory rules.go: pure, no clock, no store.
export const LEASE_TTL_MS = 30_000;

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

export function acquire(c, device, now) {
  if (c.lease.expires > now && c.lease.device !== device) throw refuse('lease_held');
  return { ...c, lease: { device, fence: c.lease.fence + 1, expires: now + LEASE_TTL_MS, pending: 0 } };
}

export function heartbeat(c, device, b, now) {
  holder(c, device, b.fence);
  return { ...c, lease: { ...c.lease, expires: now + LEASE_TTL_MS, pending: b.pending } };
}

export function publishTo(c, device, p, now) {
  holder(c, device, p.fence);
  if (p.old_head !== c.head) throw refuse('head_moved');
  const title = p.title === '' || p.title === undefined ? c.title : p.title;
  const next = { ...c, head: p.head, size: p.size, class: p.class, durable_at: now, lease: { ...c.lease, pending: p.pending } };
  return title === undefined ? next : { ...next, title };
}

export function releaseOf(c, device, fence) {
  holder(c, device, fence);
  return { ...c, lease: { ...c.lease, expires: 0, pending: 0 } };
}

export function created(init, device, now) {
  const c = { V: 1, head: init.head, durable_at: now, class: init.class, size: init.size, keys: init.keys,
    lease: { device, fence: 1, expires: now + LEASE_TTL_MS, pending: 0 } };
  return omitEmpty(c, { parent_cell: init.parent_cell, title: init.title, orphan_turns: init.orphan_turns });
}

const omitEmpty = (c, optional) =>
  Object.assign(c, Object.fromEntries(Object.entries(optional).filter(([, v]) => v !== undefined && v !== '' && v !== 0)));
