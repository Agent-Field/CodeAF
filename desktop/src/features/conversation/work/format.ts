// Words for time and counts in the work rhythm. Pure, so tests can pin them.

import type { WorkBlock } from '../types';

export function duration(ms: number): string {
  if (ms < 100) return '<0.1s';
  if (ms < 10_000) return `${(Math.max(ms, 0) / 1000).toFixed(1)}s`;
  const seconds = Math.round(ms / 1000);
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${seconds % 60}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

/** Whole seconds up to a minute ("6s"), then the usual minutes form. */
export function spoken(seconds: number): string {
  return seconds < 60 ? `${Math.round(seconds)}s` : duration(seconds * 1000);
}

export type SummaryPart = { text: string; tone?: 'warning' };

const plural = (count: number, one: string, many: string) => `${count} ${count === 1 ? one : many}`;

/** "Worked 42s · thought 6s · 4 steps · 9 calls · 1 failed": only the parts that exist. */
export function summaryParts(summary: WorkBlock['summary'], live = false): SummaryPart[] {
  const verb = live ? 'Working' : 'Worked';
  const lead = summary.seconds === undefined ? verb : `${verb} ${spoken(summary.seconds)}`;
  const parts: SummaryPart[] = [{ text: lead }];
  if (summary.thoughtSeconds) parts.push({ text: `thought ${spoken(summary.thoughtSeconds)}` });
  if (summary.steps > 0) parts.push({ text: plural(summary.steps, 'step', 'steps') });
  if (summary.calls > 0) parts.push({ text: plural(summary.calls, 'call', 'calls') });
  if (summary.failed > 0) parts.push({ text: `${summary.failed} failed`, tone: 'warning' });
  return parts;
}

/** Elapsed time since a live start; undefined when the start is unknown. */
export function elapsed(startedAt: number | undefined, now: number): string | undefined {
  return startedAt === undefined ? undefined : duration(now - startedAt);
}
