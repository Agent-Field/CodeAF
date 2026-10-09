// The words and numbers on one tree row. Pure, so each rule is a unit test.

import type { EngineTaskRow } from '../../chat/engine-client';
import { isEnded, type TaskKind } from '../taskState.ts';
import { parseTime, shortAge, sinceMs } from './taskClock.ts';

/** Compact token count: 940, 9.9k, 1.2M. Empty when unknown. */
export function tokensText(tokens: number): string {
  const scaled = (value: number, unit: string) => `${value.toFixed(1).replace(/\.0$/, '')}${unit}`;
  if (tokens <= 0) return '';
  if (tokens < 1000) return `${tokens}`;
  return tokens < 1_000_000 ? scaled(tokens / 1000, 'k') : scaled(tokens / 1_000_000, 'M');
}

/** Running: time so far. Ended: how long it took. Queued, paused, your call: blank. */
export function rowAge(row: EngineTaskRow, kind: TaskKind, now: number): string {
  if (kind === 'running') return shortAge(sinceMs(row.Started, now));
  return isEnded(kind) ? shortAge(parseTime(row.Ended) - parseTime(row.Started)) : '';
}

/** What a queued row waits on, as the dimmed lead of its title ("Split by loader /"); empty otherwise. */
export function waitPrefix(kind: TaskKind, titles: readonly string[]): string {
  return kind === 'queued' && titles.length > 0 ? `${titles.join(', ')} /` : '';
}
