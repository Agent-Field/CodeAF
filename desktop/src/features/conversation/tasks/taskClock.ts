// Clocks for tasks. Times arrive as ISO strings from the engine; Go's zero time
// ("0001-01-01…") means "never", so anything before 2000 is treated as absent.

const SECOND = 1000;
const MINUTE = 60;
const HOUR = 60;
const DAY = 24;
const EARLIEST = Date.UTC(2000, 0, 1);

/** Epoch milliseconds, or NaN when the engine gave no real time. */
export function parseTime(value?: string): number {
  const time = Date.parse(value ?? '');
  return time >= EARLIEST ? time : Number.NaN;
}

/** "40s", "2m", "3h", "2d": the coarse age a tree row shows. Empty when unreadable. */
export function shortAge(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '';
  const seconds = Math.floor(ms / SECOND);
  if (seconds < MINUTE) return `${seconds}s`;
  const minutes = Math.floor(seconds / MINUTE);
  if (minutes < HOUR) return `${minutes}m`;
  const hours = Math.floor(minutes / HOUR);
  return hours < DAY ? `${hours}h` : `${Math.floor(hours / DAY)}d`;
}

/** "12s", "3m 4s", "1h 5m": the finer span the task view shows. */
export function spanText(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '';
  const seconds = Math.round(ms / SECOND);
  if (seconds < MINUTE) return `${seconds}s`;
  const minutes = Math.floor(seconds / MINUTE);
  if (minutes < HOUR) return `${minutes}m ${seconds % MINUTE}s`;
  return `${Math.floor(minutes / HOUR)}h ${minutes % HOUR}m`;
}

/** "12s", "3m 4s", "1h 5m"; empty when either end is missing or unreadable. */
export function durationText(started?: string, ended?: string): string {
  return spanText(parseTime(ended) - parseTime(started));
}

/** Milliseconds from `from` to `now`; NaN while `from` is unknown. */
export function sinceMs(from: string | undefined, now: number): number {
  return now - parseTime(from);
}
