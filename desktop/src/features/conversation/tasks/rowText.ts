// The words and numbers on one tree row. Pure, so each rule is a unit test.

import type { EngineTaskRow } from '../../chat/engine-client';
import { isEnded, type TaskKind } from '../taskState.ts';
import { displayCommand } from './displayCommand.ts';
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

export type LiveStep = { command: string; clock: string };

/** The running row's second line: the live command, cut for display, with its own clock. */
export function liveStep(row: EngineTaskRow, kind: TaskKind, now: number): LiveStep | undefined {
  const command = kind === 'running' ? displayCommand(row.Live?.Command ?? '', row.LiveParts) : '';
  return command ? { command, clock: shortAge(sinceMs(row.Live?.Since, now)) } : undefined;
}

/** "waits on Parse config" for a queued row; empty when nothing it waits on is unfinished. */
export function waitsText(kind: TaskKind, titles: readonly string[]): string {
  return kind === 'queued' && titles.length > 0 ? `waits on ${titles.join(', ')}` : '';
}
