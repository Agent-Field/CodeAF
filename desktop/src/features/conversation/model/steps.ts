// Tool entries -> WorkSteps. One batch (the calls after one assistant entry)
// is one step; its title and category come from narration, caption or tools.

import type { ToolStep, WorkStep } from '../types.ts';
import type { RichEntry } from './entry.ts';
import { withoutHandoff } from './handoff.ts';
import { categoryOf, composedTitle, narrationTitle } from './titles.ts';

export type StepCtx = {
  sessionFile: string;
  running: boolean;
  lastUnanswered: number; // record index of the last call still awaiting its result
  waiting: ReadonlySet<string>; // CallIDs a question is waiting on
  decisions: ReadonlyMap<string, string>; // CallID -> the person's decision in the past tense
};

/** The calls that follow one assistant entry, with what the anchor said. */
export type Batch = {
  id: string;
  narration: string;
  anchor: RichEntry;
  calls: ToolStep[];
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

/** The engine's Took (ns) in milliseconds; undefined when it recorded none. */
function tookOf(entry: RichEntry): number | undefined {
  const ms = Math.round((entry.Took ?? 0) / 1e6);
  return ms > 0 ? ms : undefined;
}

function decisionOf(entry: RichEntry, ctx: StepCtx): Pick<ToolStep, 'decision'> {
  const decision = entry.CallID ? ctx.decisions.get(entry.CallID) : undefined;
  return decision ? { decision } : {};
}

export function toolStep(entry: RichEntry, index: number, ctx: StepCtx): ToolStep {
  const { output, covered } = withoutHandoff(entry.Output ?? '');
  return {
    id: entry.CallID || `${ctx.sessionFile}:${index}`,
    callId: entry.CallID || undefined,
    tool: entry.Tool ?? '',
    hint: entry.Hint ?? '',
    args: entry.Args ?? '',
    output,
    ...(covered ? { covered } : {}),
    ...(entry.OutputOmitted ? { outputOmitted: true } : {}),
    state: callState(entry, index, ctx),
    entryIndex: index,
    tookMs: tookOf(entry),
    ...decisionOf(entry, ctx),
  };
}

export function newBatch(id: string, narration: string, anchor: RichEntry): Batch {
  return { id, narration, anchor, calls: [] };
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

/**
 * The time a call spent working. An `ask` call parks inside its Execute until
 * the person answers, so its whole Took is waiting on them, not work.
 */
export const workedMs = (call: ToolStep): number => (call.tool === 'ask' ? 0 : (call.tookMs ?? 0));

/** A batch's calls run side by side, so the step took as long as its longest call. */
function stepTook(calls: ToolStep[]): number | undefined {
  const longest = Math.max(0, ...calls.map(workedMs));
  return longest > 0 ? longest : undefined;
}

export function finishStep(batch: Batch, ctx: StepCtx): WorkStep {
  const state = stateOf(batch.calls, ctx.waiting);
  return {
    id: batch.id,
    ...titleOf(batch, isSettled(state)),
    category: categoryOf(batch.calls[0]?.tool ?? '', batch.anchor.CaptionCategory),
    calls: batch.calls,
    tookMs: stepTook(batch.calls),
    state,
  };
}

/**
 * A failed call that a later call of the same tool at the same target
 * (hint, else args) completed is a retry that worked: it is not a failure.
 * `calls` are in record order.
 */
export function unresolvedFailures(calls: ToolStep[]): ToolStep[] {
  const target = (c: ToolStep) => `${c.tool}\0${c.hint || c.args}`;
  return calls.filter((call, at) => {
    if (call.state !== 'failed') return false;
    const key = target(call);
    return !calls.slice(at + 1).some((later) => later.state === 'done' && target(later) === key);
  });
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
    failed: unresolvedFailures(calls).length,
  };
}
