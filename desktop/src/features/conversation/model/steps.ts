// Tool entries -> WorkSteps. One batch (the calls after one assistant entry)
// is one step; its title and category come from narration, caption or tools.

import type { ToolStep, WorkStep } from '../types.ts';
import type { RichEntry } from './entry.ts';
import { categoryOf, composedTitle, narrationTitle } from './titles.ts';

export type StepCtx = {
  sessionFile: string;
  running: boolean;
  lastUnanswered: number; // record index of the last call still awaiting its result
  waiting: ReadonlySet<string>; // CallIDs a question is waiting on
};

/** The calls that follow one assistant entry, with what the anchor said. */
export type Batch = {
  id: string;
  narration: string;
  anchor: RichEntry;
  calls: ToolStep[];
  tookNs: number;
};

export function lastUnansweredCall(entries: RichEntry[]): number {
  for (let i = entries.length - 1; i >= 0; i--) {
    if (entries[i].Role === 'tool' && entries[i].Tool && !entries[i].Answered) return i;
  }
  return -1;
}

function callState(entry: RichEntry, index: number, ctx: StepCtx): ToolStep['state'] {
  if (entry.Interrupted && (entry.Failed || !entry.Answered)) return 'stopped';
  if (entry.Failed) return 'failed';
  if (entry.Answered) return 'done';
  return ctx.running && index === ctx.lastUnanswered ? 'running' : 'done';
}

export function toolStep(entry: RichEntry, index: number, ctx: StepCtx): ToolStep {
  return {
    id: entry.CallID || `${ctx.sessionFile}:${index}`,
    callId: entry.CallID || undefined,
    tool: entry.Tool ?? '',
    hint: entry.Hint ?? '',
    args: entry.Args ?? '',
    output: entry.Output ?? '',
    state: callState(entry, index, ctx),
    entryIndex: index,
  };
}

export function newBatch(id: string, narration: string, anchor: RichEntry): Batch {
  return { id, narration, anchor, calls: [], tookNs: 0 };
}

function stateOf(calls: ToolStep[], waiting: ReadonlySet<string>): WorkStep['state'] {
  const running = calls.filter((c) => c.state === 'running');
  if (running.length) return running.some((c) => c.callId && waiting.has(c.callId)) ? 'waiting' : 'running';
  if (calls.some((c) => c.state === 'stopped')) return 'stopped';
  return calls.some((c) => c.state === 'failed') ? 'failed' : 'done';
}

const isSettled = (state: WorkStep['state']) => state === 'done' || state === 'failed' || state === 'stopped';

function titleOf(batch: Batch, done: boolean): Pick<WorkStep, 'title' | 'titleSource'> {
  const narration = narrationTitle(batch.narration);
  if (narration) return { title: narration, titleSource: 'narration' };
  const caption = batch.anchor.Caption?.trim();
  if (caption) return { title: caption, titleSource: 'caption' };
  return { title: composedTitle(batch.calls, done), titleSource: 'composed' };
}

export function finishStep(batch: Batch, ctx: StepCtx): WorkStep {
  const state = stateOf(batch.calls, ctx.waiting);
  const tookMs = Math.round(batch.tookNs / 1e6);
  return {
    id: batch.id,
    ...titleOf(batch, isSettled(state)),
    category: categoryOf(batch.calls[0]?.tool ?? '', batch.anchor.CaptionCategory),
    calls: batch.calls,
    tookMs: tookMs > 0 ? tookMs : undefined,
    state,
  };
}

export function summarize(steps: WorkStep[], thoughtSeconds?: number): {
  seconds?: number;
  thoughtSeconds?: number;
  steps: number;
  calls: number;
  failed: number;
} {
  const ms = steps.reduce((sum, s) => sum + (s.tookMs ?? 0), 0);
  const calls = steps.flatMap((s) => s.calls);
  return {
    seconds: ms > 0 ? Math.max(1, Math.round(ms / 1000)) : undefined,
    thoughtSeconds,
    steps: steps.length,
    calls: calls.length,
    failed: calls.filter((c) => c.state === 'failed').length,
  };
}
