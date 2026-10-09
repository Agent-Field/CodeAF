// Lays the live overlay over the last recorded turn. It only adds to or
// updates that turn: a recorded item is never dropped, and a call the record
// already holds (matched by CallID) is never drawn twice.

import type { EngineSnapshot } from '../../chat/engine-client.ts';
import type { ToolStep, TurnV2, WorkBlock, WorkStep } from '../types.ts';
import { asRich, questionsOf, type RichEntry } from './entry.ts';
import type { LiveCall, LiveOverlayV2 } from './live.ts';
import { finishStep, newBatch, summarize, type Batch } from './steps.ts';

const PREPARING = ['forming', 'announced'];

function callState(call: LiveCall): ToolStep['state'] {
  if (call.phase === 'failed') return 'failed';
  return call.phase === 'done' || call.phase === 'finished' ? 'done' : 'running';
}

function liveToolStep(call: LiveCall): ToolStep {
  const { id, tool, hint, args, output, tookMs } = call;
  return { id, callId: id.startsWith('live:') ? undefined : id, tool, hint, args, output, tookMs, state: callState(call) };
}

function recordedCalls(turn: TurnV2): ToolStep[] {
  return turn.blocks.flatMap((b) => (b.kind === 'work' ? b.steps.flatMap((s) => s.calls) : []));
}

/** The record is canonical; the overlay only moves an in-flight call forward. */
function reconcile(recorded: ToolStep[], live: LiveOverlayV2): Set<string> {
  const seen = new Set<string>();
  for (const call of live.calls) {
    const twin = recorded.find((r) => matches(r, call, live.baseEntries));
    if (!twin) continue;
    seen.add(call.id);
    twin.tookMs ??= call.tookMs;
    if (twin.state === 'running' && callState(call) !== 'running') {
      twin.state = callState(call);
      twin.output = twin.output || call.output;
    }
  }
  return seen;
}

function matches(recorded: ToolStep, call: LiveCall, base: number): boolean {
  if (!call.id.startsWith('live:')) return recorded.callId === call.id;
  return recorded.tool === call.tool && recorded.hint === call.hint && (recorded.entryIndex ?? -1) >= base;
}

function liveSteps(calls: LiveCall[], waiting: ReadonlySet<string>): WorkStep[] {
  const batches = new Map<number, LiveCall[]>();
  for (const call of calls) batches.set(call.batch, [...(batches.get(call.batch) ?? []), call]);
  return [...batches].map(([index, group]) => liveStep(index, group, waiting));
}

function liveStep(index: number, group: LiveCall[], waiting: ReadonlySet<string>): WorkStep {
  const captioned = group.find((c) => c.caption);
  const anchor = { Role: 'tool', Text: '', Caption: captioned?.caption, CaptionCategory: captioned?.category } as RichEntry;
  const batch: Batch = { ...newBatch(`live-s${index}`, group[0].narration ?? '', anchor), calls: group.map(liveToolStep) };
  const step = finishStep(batch, { sessionFile: '', running: true, lastUnanswered: -1, waiting });
  const preparing = group.every((c) => PREPARING.includes(c.phase));
  return preparing ? { ...step, state: 'preparing' } : step;
}

function tailWork(turn: TurnV2): WorkBlock {
  const last = turn.blocks[turn.blocks.length - 1];
  if (last?.kind === 'work') return last;
  const block: WorkBlock = { kind: 'work', id: `${turn.id}:w-live`, steps: [], notes: [], live: true, summary: summarize([]) };
  turn.blocks.push(block);
  return block;
}

function addWork(turn: TurnV2, live: LiveOverlayV2, fresh: LiveCall[], waiting: ReadonlySet<string>) {
  const thinking = live.thinking;
  const thought = thinking.text.length > 0 || thinking.startedAt !== undefined;
  if (!fresh.length && !thought && !live.retry) return;
  const block = tailWork(turn);
  block.steps.push(...liveSteps(fresh, waiting));
  if (thought) block.thinking = { text: thinking.text, streaming: thinking.seconds === undefined, seconds: thinking.seconds };
  if (live.retry) block.notes.push({ kind: 'note', id: `${turn.id}:retry`, text: `Retrying — ${live.retry}` });
  block.summary = summarize(block.steps, thinking.seconds);
}

/** True when the record already holds these words as an assistant entry. */
function recordedText(entries: RichEntry[], live: LiveOverlayV2): boolean {
  const last = entries.slice(live.baseEntries).filter((e) => e.Role === 'assistant').pop();
  return Boolean(last && last.Text.trim().startsWith(live.text.trim()));
}

function addSteers(turn: TurnV2, live: LiveOverlayV2) {
  const known = new Set(turn.steer.map((s) => s.text));
  const fresh = live.steers.filter((s) => !known.has(s.text));
  turn.steer.push(...fresh);
  const work = turn.blocks[turn.blocks.length - 1];
  if (work?.kind === 'work') work.notes.push(...fresh.map((s) => ({ kind: 'steer' as const, id: s.id, text: s.text })));
}

export function applyLive(turn: TurnV2, snapshot: EngineSnapshot, live?: LiveOverlayV2) {
  turn.state = 'working';
  if (!live) return;
  const waiting = new Set(questionsOf(snapshot).flatMap((q) => (q.subject?.callId ? [q.subject.callId] : [])));
  const seen = reconcile(recordedCalls(turn), live);
  addWork(turn, live, live.calls.filter((c) => !seen.has(c.id)), waiting);
  addSteers(turn, live);
  const text = live.text.trim() && !recordedText(snapshot.entries.map(asRich), live) ? live.text : '';
  if (text) turn.blocks.push({ kind: 'answer', id: `${turn.id}:live-text`, text, streaming: !live.textDone });
  if (text && !live.textDone) turn.state = 'streaming';
  if (live.error) turn.blocks.push({ kind: 'error', id: `${turn.id}:live-error`, text: live.error });
  settleLiveFlag(turn);
}

/** The work block stays open until the answer starts streaming. */
function settleLiveFlag(turn: TurnV2) {
  const streaming = turn.state === 'streaming';
  const last = [...turn.blocks].reverse().find((b) => b.kind === 'work');
  if (last?.kind === 'work') last.live = !streaming && turn.blocks[turn.blocks.length - 1] === last;
}
