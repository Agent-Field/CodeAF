// Who is online (docs/ux-pairing-contract.md 3.5): a device is online iff it holds at least one open
// watch socket. The answer reads socket state only, so it costs no storage write.

/** onlineDevices is the set of device ids that hold an open watch socket; each socket is tagged with its device. */
export function onlineDevices(ctx) {
  return new Set(ctx.getWebSockets().flatMap((ws) => ctx.getTags(ws)));
}

/** presenceOf answers {now, devices} for every device of the directory that is not revoked. */
export function presenceOf(devices, online, now) {
  const entries = Object.entries(devices)
    .filter(([, d]) => !d.revoked)
    .map(([id, d]) => [id, online.has(id) ? { online: true, last_seen: now } : { online: false, last_seen: d.last_seen ?? 0 }]);
  return { now, devices: Object.fromEntries(entries) };
}
