// The 12-hour idle rule for Open places (Places 10a, 6d), as pure functions of an injected clock. Nothing here reads
// the time itself, so a node test can stand exactly on the boundary. Pinned places never reach this rule.

/** How long an Open place may sit untouched before it leaves the rail (Places 10a). Pinned places are never on this clock. */
export const OPEN_IDLE_MS = 12 * 60 * 60 * 1000;

/**
 * True when an Open place has been untouched for the full twelve hours. 11h59 is still open; 12h00 is closed. An
 * unparseable instant is never idle, because guessing would close a place the person may be using.
 */
export function isIdle(touchedAt: string, now: string): boolean {
  const at = Date.parse(touchedAt);
  const clock = Date.parse(now);
  if (Number.isNaN(at) || Number.isNaN(clock)) return false;
  return clock - at >= OPEN_IDLE_MS;
}

/** Whether an Open place leaves the rail by itself. Work that is running or needs the person holds it open. */
export function autoCloses(touchedAt: string, now: string, busy: boolean): boolean {
  return !busy && isIdle(touchedAt, now);
}

/** Where a place lands when it is opened again, and whether its old tabs come back (Q-P4). */
export type Reopen = { land: 'home'; restoreTabs: boolean };

/**
 * A place the person closed gets its tabs back. A place that closed itself after idling lands on Home with nothing
 * restored: its idle tabs were archived to history, and it reopens quiet.
 */
export function reopen(closedBy: 'person' | 'idle'): Reopen {
  return { land: 'home', restoreTabs: closedBy === 'person' };
}
