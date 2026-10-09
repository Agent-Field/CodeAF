// The view model of one task page: only what the engine said, nothing invented.
// Pure, so the state line, the log and the notes are tested without a DOM.

import type { EngineTaskRow } from '../chat/engine-client';
import { liveLine, noteLines, refusedCount, stepLines } from './tasks/logLines.ts';
import { durationText, sinceMs, spanText } from './tasks/taskClock.ts';
import type { LogLine, NoteLine, TaskPage } from './tasks/taskTypes.ts';
import { isEnded, rowKind, taskControls, taskMark, type TaskControls, type TaskKind, type TaskMark } from './taskState.ts';

export { durationText } from './tasks/taskClock.ts';

/** A child or waited-on task, drawn as one quiet row. */
export type TaskLink = { row: EngineTaskRow; depth: number };

export type TaskPageModel = {
  id: string;
  title: string;
  kind: TaskKind;
  mark: TaskMark;
  ended: boolean;
  /** The parts after the state word, each one known: step, elapsed, model tier, cost. */
  stateParts: string[];
  result: string;
  /** Why it ended and what the worker said last; for tasks that ended without a result. */
  endReason: string;
  lastWords: string;
  changed: string[];
  steps: LogLine[];
  live?: LogLine;
  refused: number;
  notes: NoteLine[];
  instructions: string;
  checks: string[];
  children: TaskLink[];
  waits: TaskLink[];
  controls: TaskControls;
};

/** True while the engine is still changing the page, so it is worth refreshing. */
export function isTaskRunning(page: TaskPage | undefined): boolean {
  return page ? !isEnded(rowKind(page.Row)) : false;
}

/** "deepseek/deepseek-v4.1-flash" reads "Deepseek v4.1 Flash". */
export function modelName(model?: string): string {
  const name = (model ?? '').split('/').pop() ?? '';
  const words = name.split(/[-_]/).filter(Boolean);
  return words.map(capital).join(' ');
}

function capital(word: string): string {
  return /^v?\d/.test(word) ? word : word.charAt(0).toUpperCase() + word.slice(1);
}

/** "deepseek/deepseek-v4.1-flash" reads "Flash": the tier a person tells models apart by; a name without one reads whole. */
export function modelTier(model?: string): string {
  const whole = modelName(model);
  const last = whole.split(' ').pop() ?? '';
  return whole.includes(' ') && /^[A-Z]/.test(last) ? last : whole;
}

/** "$0.06"; empty for an unknown or zero cost (the emptiness law). */
export function costPart(usd?: number): string {
  return usd && usd > 0 ? `$${usd.toFixed(2)}` : '';
}

function stepPart(page: TaskPage, kind: TaskKind): string {
  if (kind !== 'running') return '';
  const step = page.Live?.Step && page.Live.Step > 0 ? page.Live.Step : page.Row.Steps;
  return step ? `step ${step}` : '';
}

function elapsedPart(page: TaskPage, kind: TaskKind, now: number): string {
  const row = page.Row;
  if (kind === 'running') return spanText(sinceMs(row.Started, now));
  if (!isEnded(kind)) return '';
  return durationText(row.Started, row.Ended || page.Ended?.At);
}

/** Only the parts the engine knows; an unknown one leaves no gap. */
export function stateParts(page: TaskPage, kind: TaskKind, now: number): string[] {
  return [stepPart(page, kind), elapsedPart(page, kind, now), modelTier(page.Row.Model), costPart(page.Row.USD)].filter(Boolean);
}

function resultOf(page: TaskPage, ended: boolean): string {
  // A result belongs to a finished task; on an unfinished one it is left over from an earlier run.
  return ended ? (page.Result || page.Ended?.Result || '') : '';
}

function linksOf(rows: readonly EngineTaskRow[] | null | undefined): TaskLink[] {
  return (rows ?? []).map((row) => ({ row, depth: Math.max(0, (row.Depth ?? 1) - 1) }));
}

export function taskPageModel(page: TaskPage, now: number = Date.now()): TaskPageModel {
  const row = page.Row;
  const kind = rowKind(row);
  const ended = isEnded(kind);
  const steps = stepLines(page);
  return {
    id: row.ID,
    title: row.Title,
    kind,
    mark: taskMark(row.Status, { stopped: row.Stopped, interrupted: row.Interrupted, hold: row.Hold, paused: row.Paused }),
    ended,
    stateParts: stateParts(page, kind, now),
    result: resultOf(page, ended),
    endReason: ended ? (page.Ended?.Reason ?? '') : '',
    lastWords: ended ? (page.LastWords ?? '') : '',
    changed: [...(page.Changed ?? [])],
    steps,
    live: ended ? undefined : liveLine(page),
    refused: refusedCount(steps),
    notes: noteLines(page, !ended),
    instructions: page.Description ?? '',
    checks: [...(page.Checks ?? [])],
    children: linksOf(page.Children),
    waits: linksOf(page.WaitRows),
    controls: taskControls(kind, Boolean(row.Parent)),
  };
}
