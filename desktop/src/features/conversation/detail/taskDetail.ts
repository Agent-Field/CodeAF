// What the task detail pane says about one task. Pure, so each rule is a unit
// test. A fact is a row only when the engine provided it (the emptiness law).

import type { EngineTaskRow } from '../../chat/engine-client.ts';
import { rowAge, tokensText } from '../tasks/rowText.ts';
import { parseTime } from '../tasks/taskClock.ts';
import type { TaskKind } from '../taskState.ts';

export type Fact = { label: string; value: string };

const COST_DIGITS = 2;

/** The step the task is on: the live step while it runs, else the steps it took. */
export function stepOf(row: EngineTaskRow): number {
  return row.Live?.Step || row.Steps || 0;
}

/** "step 12 · 2m": only the parts the engine knows. */
export function progressText(row: EngineTaskRow, kind: TaskKind, now: number): string {
  const step = stepOf(row);
  return [step > 0 ? `step ${step}` : '', rowAge(row, kind, now)].filter(Boolean).join(' · ');
}

/** "$0.14 · 41k tokens": cost and tokens, each only when above zero. */
export function costText(row: EngineTaskRow): string {
  const cost = row.USD && row.USD > 0 ? `$${row.USD.toFixed(COST_DIGITS)}` : '';
  const tokens = tokensText(row.Tokens ?? 0);
  return [cost, tokens && `${tokens} tokens`].filter(Boolean).join(' · ');
}

const pad = (value: number) => String(value).padStart(2, '0');

/** "14:02": the local clock time the task began; empty when the engine gave none. */
export function startedText(started?: string): string {
  const time = parseTime(started);
  if (Number.isNaN(time)) return '';
  const date = new Date(time);
  return `${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/** The Model / Steps / Cost / Started facts, in the designer's order, absent ones left out. */
export function factsOf(row: EngineTaskRow): Fact[] {
  const steps = stepOf(row);
  const facts: Fact[] = [
    { label: 'Model', value: row.Model ?? '' },
    { label: 'Steps', value: steps > 0 ? String(steps) : '' },
    { label: 'Cost', value: costText(row) },
    { label: 'Started', value: startedText(row.Started) },
  ];
  return facts.filter((fact) => fact.value);
}
