// Who is online (docs/ux-pairing-contract.md 3.5, amended by contract 21.12): a device is online iff it holds a
// live watch socket, one whose sign of life is inside its window. The answer reads socket state only, so it costs no
// storage write; the rule itself lives with the sockets (liveness.js), and this file only shapes the answer.
import { PRESENCE } from './limits.js';
import { Wire } from './wire.js';

const PLAIN_SECONDS = /^[1-9][0-9]?$/;

/**
 * parseBeat reads the `beat` values of a watch upgrade (contract 21.12.3): none is the pre-presence default, one
 * value (repeated or not) is a plain decimal in 1 to maxBeat, and anything else is refused before the upgrade.
 */
export function parseBeat(values) {
  const distinct = [...new Set(values)];
  if (distinct.length === 0) return PRESENCE.defaultBeat;
  const beat = distinct.length === 1 && PLAIN_SECONDS.test(distinct[0]) ? Number(distinct[0]) : 0;
  if (beat < 1 || beat > PRESENCE.maxBeat) throw new Wire('bad_request', 400);
  return beat;
}

/** presenceOf answers {now, devices} for every device of the directory that is not revoked. */
export function presenceOf(devices, online, now) {
  const entries = Object.entries(devices)
    .filter(([, d]) => !d.revoked)
    .map(([id, d]) => [id, online.has(id) ? { online: true, last_seen: now } : { online: false, last_seen: d.last_seen ?? 0 }]);
  return { now, devices: Object.fromEntries(entries) };
}
