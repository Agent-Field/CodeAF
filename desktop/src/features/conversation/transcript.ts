// Projects canonical engine records into the conversation view model.
// Pure: no React, no CSS. Everything shown here comes from the snapshot,
// plus the live overlay for the part of a running turn not yet recorded.

import type { EngineEntry, EngineSnapshot } from '../chat/engine-client.ts';
import type { ConversationModel, ToolStep, Turn, TurnItem } from './types.ts';
import type { LiveOverlay } from './live-overlay.ts';
import { digestOf, parseTaskAside } from './transcript-parse.ts';

export { digestOf } from './transcript-parse.ts';
export { reduceLiveEvent, emptyOverlay } from './live-overlay.ts';
export type { LiveOverlay } from './live-overlay.ts';

// The record may carry these on an aside; the Go side is adding them.
type AsideFields = { TaskIDs?: string[]; TaskStatus?: string };
// DisplayEntry has no failure flag today; honour one if it appears.
type ToolFields = { Failed?: boolean };

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
  if ((entry as ToolFields).Failed) return 'failed';
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

function taskIdFor(entry: EngineEntry, title: string, ctx: Ctx): string | undefined {
  const given = (entry as EngineEntry & AsideFields).TaskIDs?.[0];
  if (given) return given;
  const row = ctx.snapshot.tasks.find((t) => (t as { Title?: string }).Title === title);
  const id = (row as { ID?: unknown } | undefined)?.ID;
  return id ? String(id) : undefined;
}

function asideItem(entry: EngineEntry, id: string, ctx: Ctx): TurnItem {
  const parsed = parseTaskAside(entry.Text);
  const fields = entry as EngineEntry & AsideFields;
  if (!parsed && !fields.TaskIDs?.length) return { kind: 'note', id, text: entry.Text };
  const title = parsed?.title ?? '';
  return {
    kind: 'task',
    id,
    taskId: taskIdFor(entry, title, ctx),
    title,
    status: fields.TaskStatus ?? parsed?.status ?? '',
    summary: parsed?.summary ?? '',
    body: entry.Text,
  };
}

function itemFor(entry: EngineEntry, id: string, ctx: Ctx): TurnItem | undefined {
  // A blank assistant record only opens a tool round; drawing it would split one group of steps.
  if (entry.Role === 'assistant') return entry.Text.trim() ? { kind: 'text', id, text: entry.Text, streaming: false } : undefined;
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
    if (entry.Role === 'user') {
      builders.push(newTurn(snapshot, entry, index));
      return;
    }
    const current = builders[builders.length - 1];
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
    out.push({ kind: 'thinking', id: `${turn.id}:live-thinking`, text: live.thinking, streaming: true });
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
  const last = turn.items[turn.items.length - 1];
  if (last?.kind === 'tools' && last.steps.some((s) => s.state === 'running')) return undefined;
  const { tool, hint } = live.activeTool;
  return { id: `${turn.id}:live-tool`, tool, hint, args: '', output: '', state: 'running' };
}

function applyOverlay(turn: Turn, snapshot: EngineSnapshot, live: LiveOverlay) {
  const extra = overlayItems(turn, snapshot, live);
  turn.items.push(...extra);
  const step = liveStep(live, turn);
  if (step) turn.items.push({ kind: 'tools', id: `${turn.id}:live-tools`, steps: [step] });
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
