// Projects canonical engine records into the conversation view model.
// Pure: no React, no CSS. Everything shown here comes from the snapshot,
// plus the live overlay for the part of a running turn not yet recorded.

import type { EngineEntry, EngineSnapshot } from '../chat/engine-client.ts';
import type { ConversationModel, ToolStep, Turn, TurnItem } from './types.ts';
import type { LiveOverlay } from './live-overlay.ts';
import { digestOf, firstSentence, parseTaskAside } from './transcript-parse.ts';

export { digestOf } from './transcript-parse.ts';
export { reduceLiveEvent, emptyOverlay } from './live-overlay.ts';
export type { LiveOverlay } from './live-overlay.ts';

// The record may carry these on an aside; the Go side is adding them.
type AsideFields = { TaskIDs?: string[]; TaskStatus?: string };

type Ctx = { snapshot: EngineSnapshot; lastUnanswered: number };
type Builder = { turn: Turn; byCall: Map<string, ToolStep> };

function lastUnansweredTool(entries: EngineEntry[]): number {
  for (let i = entries.length - 1; i >= 0; i--) {
    const e = entries[i];
    if (e.Role === 'tool' && e.Tool && !e.Answered) return i;
  }
  return -1;
}

function stepState(entry: EngineEntry, index: number, ctx: Ctx): ToolStep['state'] {
  if (entry.Failed) return 'failed';
  if (entry.Answered) return 'done';
  return ctx.snapshot.running && index === ctx.lastUnanswered ? 'running' : 'done';
}

function toStep(entry: EngineEntry, index: number, ctx: Ctx): ToolStep {
  return {
    id: entry.CallID || `${ctx.snapshot.sessionFile}:${index}`,
    callId: entry.CallID || undefined,
    tool: entry.Tool ?? '',
    hint: entry.Hint ?? '',
    args: entry.Args ?? '',
    output: entry.Output ?? '',
    state: stepState(entry, index, ctx),
  };
}

function addTool(b: Builder, entry: EngineEntry, index: number, ctx: Ctx) {
  const step = toStep(entry, index, ctx);
  const known = step.callId ? b.byCall.get(step.callId) : undefined;
  if (known) {
    Object.assign(known, step, {
      hint: step.hint || known.hint,
      args: step.args || known.args,
    });
    return;
  }
  if (step.callId) b.byCall.set(step.callId, step);
  const last = b.turn.items[b.turn.items.length - 1];
  if (last?.kind === 'tools') last.steps.push(step);
  else b.turn.items.push({ kind: 'tools', id: `${b.turn.id}:t${index}`, steps: [step] });
}

type TaskRowLike = { ID?: unknown; Title?: string; Status?: string; Note?: string };

// Rows arrive oldest run first, so the last match is the newest record.
function lastRow(ctx: Ctx, matches: (row: TaskRowLike) => boolean): TaskRowLike | undefined {
  return [...(ctx.snapshot.tasks as TaskRowLike[])].reverse().find(matches);
}

function rowById(ctx: Ctx, id: string | undefined): TaskRowLike | undefined {
  if (!id) return undefined;
  return lastRow(ctx, (t) => String(t.ID) === id);
}

/** The task whose own recorded report line the note carries. */
function rowByNote(ctx: Ctx, text: string): TaskRowLike | undefined {
  return lastRow(ctx, (t) => Boolean(t.Note?.trim()) && text.includes(t.Note as string));
}

function rowByTitle(ctx: Ctx, title: string): TaskRowLike | undefined {
  if (!title) return undefined;
  return lastRow(ctx, (t) => t.Title === title);
}

function asideItem(entry: EngineEntry, id: string, ctx: Ctx): TurnItem {
  const parsed = parseTaskAside(entry.Text);
  const fields = entry as EngineEntry & AsideFields;
  const given = fields.TaskIDs?.[0];
  if (!parsed && !given) return { kind: 'note', id, text: entry.Text };
  // The canonical row outranks words parsed out of the note.
  const byId = rowById(ctx, given);
  const row = byId ?? rowByNote(ctx, entry.Text) ?? rowByTitle(ctx, parsed?.title ?? '');
  // A notice with no name cannot be read or opened; it is only a note.
  const title = row?.Title || parsed?.title || '';
  if (!title) return { kind: 'note', id, text: entry.Text };
  const taskId = given ?? (row?.ID ? String(row.ID) : undefined);
  return {
    kind: 'task',
    id,
    taskId,
    title,
    status: fields.TaskStatus ?? byId?.Status ?? parsed?.status ?? row?.Status ?? '',
    summary: parsed?.summary ?? firstSentence(entry.Text),
    body: entry.Text,
  };
}

function itemFor(entry: EngineEntry, id: string, ctx: Ctx): TurnItem | undefined {
  // A blank record draws nothing, so it must not split one group of steps in two.
  if (!entry.Text.trim()) return undefined;
  if (entry.Role === 'assistant') return { kind: 'text', id, text: entry.Text, streaming: false };
  if (entry.Role === 'note') return { kind: 'note', id, text: entry.Text };
  if (entry.Role === 'aside') return asideItem(entry, id, ctx);
  return undefined;
}

function newTurn(snapshot: EngineSnapshot, entry: EngineEntry, index: number): Builder {
  const id = `${snapshot.sessionFile}:${index}`;
  const turn: Turn = { id, user: entry.Text, items: [], state: 'done', digest: '' };
  return { turn, byCall: new Map() };
}

function lastTextOf(turn: Turn): string {
  for (let i = turn.items.length - 1; i >= 0; i--) {
    const item = turn.items[i];
    if (item.kind === 'text') return item.text;
  }
  return '';
}

function project(snapshot: EngineSnapshot): ConversationModel {
  const ctx: Ctx = { snapshot, lastUnanswered: lastUnansweredTool(snapshot.entries) };
  const builders: Builder[] = [];
  const preface: TurnItem[] = [];
  const stopped = new Set<Turn>();
  snapshot.entries.forEach((entry, index) => {
    const current = builders[builders.length - 1];
    if (entry.Role === 'user' && entry.Steer && current) {
      current.turn.items.push({ kind: 'steer', id: `${current.turn.id}:${index}`, text: entry.Text });
      return;
    }
    if (entry.Role === 'user') {
      builders.push(newTurn(snapshot, entry, index));
      return;
    }
    if (entry.Interrupted && current) stopped.add(current.turn);
    if (entry.Role === 'tool') {
      if (entry.Tool && current) addTool(current, entry, index, ctx);
      return;
    }
    const item = itemFor(entry, `${current?.turn.id ?? 'preface'}:${index}`, ctx);
    if (item) (current ? current.turn.items : preface).push(item);
  });
  const turns = builders.map((b) => b.turn);
  for (const turn of turns) {
    turn.state = stopped.has(turn) ? 'stopped' : 'done';
    turn.digest = digestOf(lastTextOf(turn));
  }
  return {
    title: snapshot.title,
    turns,
    preface,
    running: snapshot.running,
    questions: snapshot.questions ?? [],
    tasks: snapshot.tasks,
    planError: snapshot.planError,
  };
}

function overlayItems(turn: Turn, snapshot: EngineSnapshot, live: LiveOverlay): TurnItem[] {
  const recorded = snapshot.entries
    .slice(live.turnBaseEntries)
    .filter((e) => e.Role === 'assistant')
    .pop();
  const out: TurnItem[] = [];
  if (live.thinking) {
    // Thinking ends where the answer's words begin.
    out.push({ kind: 'thinking', id: `${turn.id}:live-thinking`, text: live.thinking, streaming: !live.text });
  }
  const covered = recorded && live.text && recorded.Text.startsWith(live.text);
  if (live.text && !covered) {
    out.push({ kind: 'text', id: `${turn.id}:live-text`, text: live.text, streaming: true });
  }
  if (live.error) out.push({ kind: 'error', id: `${turn.id}:live-error`, text: live.error });
  return out;
}

function liveStep(live: LiveOverlay, turn: Turn): ToolStep | undefined {
  if (!live.activeTool) return undefined;
  // A recorded call still waiting on its result is this live step already drawn.
  const recordedRunning = turn.items.some((item) => item.kind === 'tools' && item.steps.some((s) => s.state === 'running'));
  if (recordedRunning) return undefined;
  const { tool, hint } = live.activeTool;
  return { id: `${turn.id}:live-tool`, tool, hint, args: '', output: '', state: 'running' };
}

function addLiveStep(turn: Turn, step: ToolStep) {
  const last = turn.items[turn.items.length - 1];
  if (last?.kind === 'tools') last.steps.push(step);
  else turn.items.push({ kind: 'tools', id: `${turn.id}:live-tools`, steps: [step] });
}

function applyOverlay(turn: Turn, snapshot: EngineSnapshot, live: LiveOverlay) {
  const extra = overlayItems(turn, snapshot, live);
  turn.items.push(...extra);
  const step = liveStep(live, turn);
  if (step) addLiveStep(turn, step);
  if (extra.some((i) => i.kind === 'text')) turn.state = 'streaming';
}

export function projectConversation(snapshot: EngineSnapshot, live?: LiveOverlay): ConversationModel {
  const model = project(snapshot);
  const last = model.turns[model.turns.length - 1];
  if (!last || !snapshot.running) return model;
  last.state = 'working';
  if (live) applyOverlay(last, snapshot, live);
  return model;
}
