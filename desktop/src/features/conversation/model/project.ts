// Canonical engine records -> TurnV2[] (ELEMENTS.md). Pure: no React.
// The flags decide what an assistant entry is, never its position:
//   Answer && !Addressed -> answer      Addressed -> update (cut if Interrupted)
//   otherwise            -> narration, which titles the step that follows it.

import type { EngineSnapshot } from '../../chat/engine-client.ts';
import type { ToolStep, TurnBlock, TurnItem, TurnV2, WorkBlock } from '../types.ts';
import { noteItem } from '../aside-kind.ts';
import { digestOf } from '../transcript-parse.ts';
import { attachmentsOf } from './attachments.ts';
import { placeDeliverables } from './deliverables.ts';
import { asRich, questionsOf, type RichEntry } from './entry.ts';
import { receiptOutcomes } from './outcomes.ts';
import { addReceipts, type ReceiptPlaces } from './receipts.ts';
import { finishStep, lastUnansweredCall, newBatch, summarize, toolStep, type Batch, type StepCtx } from './steps.ts';
import { asideItem } from './tasks.ts';
import type { LiveOverlayV2 } from './live.ts';
import { applyLive } from './live-apply.ts';

type WorkAcc = { id: string; batches: Batch[]; notes: TurnItem[] };
type Acc = {
  turn: TurnV2;
  ctx: StepCtx;
  calls: ToolStep[];
  byCall: Map<string, ToolStep>;
  work?: WorkAcc;
  fresh: boolean; // the next tool call opens a new batch
  narration: string; // the last narration, waiting for the batch it titles
  stopped: boolean;
};
type Input = { entry: RichEntry; index: number; snapshot: EngineSnapshot; ctx: StepCtx };

function newAcc({ entry, index, snapshot, ctx }: Input): Acc {
  const { attachments, user } = attachmentsOf(entry);
  const id = `${snapshot.sessionFile}:${index}`;
  const turn: TurnV2 = { id, user, attachments, steer: [], blocks: [], state: 'done', digest: '' };
  return { turn, ctx, calls: [], byCall: new Map(), fresh: true, narration: '', stopped: false };
}

function openWork(acc: Acc, index: number): WorkAcc {
  acc.work ??= { id: `${acc.turn.id}:w${index}`, batches: [], notes: [] };
  return acc.work;
}

/** Narration that no tool call followed is drawn as a quiet work line. */
function settleNarration(acc: Acc, index: number) {
  if (acc.narration.trim()) openWork(acc, index).notes.push(noteItem(`${acc.turn.id}:n${index}`, acc.narration, false));
  acc.narration = '';
}

function flushWork(acc: Acc, index: number) {
  settleNarration(acc, index);
  const work = acc.work;
  if (!work) return;
  acc.work = undefined;
  acc.fresh = true;
  const steps = work.batches.map((b) => finishStep(b, acc.ctx));
  const block: WorkBlock = { kind: 'work', id: work.id, steps, notes: work.notes, live: false, summary: summarize(steps) };
  acc.turn.blocks.push(block);
}

function push(acc: Acc, index: number, block: TurnBlock) {
  flushWork(acc, index);
  acc.turn.blocks.push(block);
}

function addSteer({ entry, index }: Input, acc: Acc) {
  const id = `${acc.turn.id}:${index}`;
  const steer = entry.Steer;
  const row = { id, text: entry.Text, landing: steer?.Landing || undefined, consumed: steer?.Consumed !== false };
  acc.turn.steer.push(row);
  if (acc.work) acc.work.notes.push({ kind: 'steer', ...row });
}

function addAssistant({ entry, index }: Input, acc: Acc) {
  settleNarration(acc, index);
  acc.fresh = true;
  const id = `${acc.turn.id}:${index}`;
  const hasText = Boolean(entry.Text.trim());
  if (entry.Addressed) {
    if (hasText) push(acc, index, { kind: 'update', id, text: entry.Text, cut: Boolean(entry.Interrupted), streaming: false });
  } else if (entry.Answer) {
    if (hasText) push(acc, index, { kind: 'answer', id, text: entry.Text, streaming: false });
  } else {
    acc.narration = entry.Text;
  }
}

function addTool(input: Input, acc: Acc) {
  const { entry, index, ctx } = input;
  if (!entry.Tool) return;
  const step = toolStep(entry, index, ctx);
  const known = step.callId ? acc.byCall.get(step.callId) : undefined;
  if (known) {
    Object.assign(known, step, { hint: step.hint || known.hint, args: step.args || known.args, tookMs: step.tookMs ?? known.tookMs });
    return;
  }
  if (step.callId) acc.byCall.set(step.callId, step);
  acc.calls.push(step);
  const work = openWork(acc, index);
  if (acc.fresh || work.batches.length === 0) {
    work.batches.push(newBatch(`${acc.turn.id}:s${index}`, acc.narration, entry));
    acc.fresh = false;
    acc.narration = '';
  }
  const batch = work.batches[work.batches.length - 1];
  batch.calls.push(step);
}

function addAside(input: Input, acc: Acc) {
  const { entry, index, snapshot } = input;
  if (!entry.Text.trim()) return;
  const item = asideItem(entry, `${acc.turn.id}:${index}`, snapshot);
  if (item.kind === 'task') push(acc, index, { ...item, kind: 'task' });
  else openWork(acc, index).notes.push(item);
}

function addNote({ entry, index }: Input, acc: Acc) {
  if (!entry.Text.trim()) return;
  openWork(acc, index).notes.push({ kind: 'note', id: `${acc.turn.id}:${index}`, text: entry.Text, tone: 'compaction' });
}

function consume(input: Input, acc: Acc) {
  const { entry } = input;
  if (entry.Interrupted) acc.stopped = true;
  if (entry.Role === 'tool') addTool(input, acc);
  else if (entry.Role === 'assistant') addAssistant(input, acc);
  else if (entry.Role === 'note') addNote(input, acc);
  else if (entry.Role === 'aside') addAside(input, acc);
}

function prefaceItem({ entry, index, snapshot }: Input): TurnItem | undefined {
  if (!entry.Text.trim()) return undefined;
  const id = `preface:${index}`;
  if (entry.Role === 'assistant') return { kind: 'text', id, text: entry.Text, streaming: false };
  if (entry.Role === 'note') return { kind: 'note', id, text: entry.Text, tone: 'compaction' };
  return entry.Role === 'aside' ? asideItem(entry, id, snapshot) : undefined;
}

function finishTurn(acc: Acc, index: number): TurnV2 {
  flushWork(acc, index);
  const turn = acc.turn;
  turn.blocks = placeDeliverables(turn.blocks, turn.id, acc.calls);
  const answers = turn.blocks.filter((b) => b.kind === 'answer');
  const last = answers[answers.length - 1];
  turn.digest = last?.kind === 'answer' ? digestOf(last.text) : '';
  turn.state = acc.stopped ? 'stopped' : 'done';
  return turn;
}

function stepContext(snapshot: EngineSnapshot, entries: RichEntry[]): StepCtx {
  const waiting = new Set(questionsOf(snapshot).flatMap((q) => (q.subject?.callId ? [q.subject.callId] : [])));
  return { sessionFile: snapshot.sessionFile, running: snapshot.running, lastUnanswered: lastUnansweredCall(entries), waiting };
}

/** `places` remembers where each question's receipt was drawn, so its outcome lands there too. */
export function projectTurnsV2(snapshot: EngineSnapshot, live?: LiveOverlayV2, places?: ReceiptPlaces): { turns: TurnV2[]; preface: TurnItem[] } {
  const entries = snapshot.entries.map(asRich);
  const ctx = stepContext(snapshot, entries);
  const accs: Acc[] = [];
  const preface: TurnItem[] = [];
  entries.forEach((entry, index) => {
    const input: Input = { entry, index, snapshot, ctx };
    const current = accs[accs.length - 1];
    if (entry.Role === 'user' && entry.Steer && current) return addSteer(input, current);
    if (entry.Role === 'user') {
      return void accs.push(newAcc(input));
    }
    if (current) return consume(input, current);
    const item = prefaceItem(input);
    if (item) preface.push(item);
  });
  const turns = accs.map((acc) => finishTurn(acc, entries.length));
  const last = turns[turns.length - 1];
  if (snapshot.running && last) applyLive(last, snapshot, live);
  addReceipts(turns, receiptOutcomes(snapshot), places);
  return { turns, preface };
}
