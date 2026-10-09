// The not-yet-recorded tail of a running turn, built from stream events.
// The snapshot stays canonical: the overlay only adds and updates.

import type { EngineEvent } from '../../chat/engine-client.ts';

export type LivePhase = 'forming' | 'announced' | 'running' | 'finished' | 'done' | 'failed';

export type LiveCall = {
  id: string; // CallID, or `live:<n>` when the event carried none
  tool: string;
  hint: string;
  args: string;
  output: string;
  phase: LivePhase;
  batch: number;
  narration?: string; // text streamed before the batch's first call
  caption?: string;
  category?: string;
  tookMs?: number;
  startedAt?: number; // first observed toolBegin in this reader; never a reconstructed history time
};

export type LiveSteer = { id: string; text: string; landing?: string; consumed: boolean };

export type LiveOverlayV2 = {
  startedAt?: number; // first observed event of this turn; resets on completion/reload
  baseEntries: number; // snapshot entry count when this turn's overlay began
  text: string;
  textDone: boolean; // AssistantDone seen: the next delta starts a new message
  thinking: { text: string; startedAt?: number; seconds?: number };
  calls: LiveCall[];
  steers: LiveSteer[];
  retry?: string;
  error?: string;
};

export const emptyLive = (entryCount = 0): LiveOverlayV2 => ({
  baseEntries: entryCount,
  text: '',
  textDone: false,
  thinking: { text: '' },
  calls: [],
  steers: [],
});

type Raw = Record<string, unknown>;
type Handler = (o: LiveOverlayV2, event: EngineEvent, now: number) => LiveOverlayV2;

const OTHER_KINDS: Record<number, string> = {
  14: 'toolAnnounced',
  15: 'toolForming',
  32: 'toolFinished',
  33: 'retrying',
  41: 'steerAccepted',
  42: 'steerConsumed',
  43: 'steerFellThrough',
  47: 'caption',
};

/** A Go field read as written ("CallID") or in camel case ("callId"). */
export function rawField(raw: Raw | undefined, name: string): unknown {
  if (!raw) return undefined;
  const camel = name.charAt(0).toLowerCase() + name.slice(1);
  return raw[name] ?? raw[camel] ?? raw[name.toLowerCase()];
}

export const rawText = (raw: Raw | undefined, name: string): string => {
  const value = rawField(raw, name);
  return typeof value === 'string' ? value : '';
};

function kindOf(event: EngineEvent): string {
  const kind = rawField(event.raw, 'Kind');
  return event.kind === 'other' && typeof kind === 'number' ? (OTHER_KINDS[kind] ?? 'other') : event.kind;
}

const PHASE_RANK: Record<LivePhase, number> = { forming: 0, announced: 1, running: 2, finished: 3, done: 4, failed: 4 };
const SETTLED: LivePhase[] = ['finished', 'done', 'failed'];

function endThinking(o: LiveOverlayV2, now: number): LiveOverlayV2 {
  const { startedAt, seconds } = o.thinking;
  if (startedAt === undefined || seconds !== undefined) return o;
  return { ...o, thinking: { ...o.thinking, seconds: Math.max(1, Math.round((now - startedAt) / 1000)) } };
}

function addReasoning(o: LiveOverlayV2, event: EngineEvent, now: number): LiveOverlayV2 {
  const chunk = event.text || rawText(event.raw, 'Text');
  const startedAt = o.thinking.startedAt ?? now;
  return { ...o, retry: undefined, thinking: { text: o.thinking.text + chunk, startedAt } };
}

function addText(o: LiveOverlayV2, event: EngineEvent, now: number): LiveOverlayV2 {
  const ended = endThinking(o, now);
  const base = o.textDone ? '' : o.text;
  return { ...ended, retry: undefined, text: base + (event.text ?? ''), textDone: false };
}

function startsBatch(calls: LiveCall[]): boolean {
  const last = calls[calls.length - 1];
  if (!last) return true;
  return calls.filter((c) => c.batch === last.batch).every((c) => SETTLED.includes(c.phase));
}

function patchCall(calls: LiveCall[], id: string, patch: Partial<LiveCall>): LiveCall[] {
  return calls.map((c) => (c.id === id ? { ...c, ...patch } : c));
}

/** Streamed text before a batch's first call was that batch's narration. */
function openCall(o: LiveOverlayV2, event: EngineEvent): LiveOverlayV2 {
  const id = rawText(event.raw, 'CallID') || `live:${o.calls.length}`;
  const batch = startsBatch(o.calls) ? (o.calls[o.calls.length - 1]?.batch ?? -1) + 1 : o.calls[o.calls.length - 1].batch;
  const fresh = !o.calls.some((c) => c.batch === batch);
  const narration = fresh && o.text.trim() ? o.text : undefined;
  const call: LiveCall = { id, tool: event.tool, hint: event.hint, args: '', output: '', phase: 'forming', batch, narration };
  return { ...o, text: fresh ? '' : o.text, textDone: fresh ? false : o.textDone, calls: [...o.calls, call] };
}

function advance(phase: LivePhase, next: LivePhase): LivePhase {
  return PHASE_RANK[next] >= PHASE_RANK[phase] ? next : phase;
}

const OPENING: LivePhase[] = ['forming', 'announced', 'running'];

/** An event with a CallID finds its call; one without finds the call still open. */
function targetOf(o: LiveOverlayV2, id: string): LiveCall | undefined {
  if (id) return o.calls.find((c) => c.id === id);
  return [...o.calls].reverse().find((c) => !SETTLED.includes(c.phase));
}

function patchOf(call: LiveCall, event: EngineEvent, next: LivePhase): Partial<LiveCall> {
  const took = Number(rawField(event.raw, 'Took')) / 1e6;
  return {
    phase: advance(call.phase, next),
    tool: event.tool || call.tool,
    hint: event.hint || call.hint,
    args: rawText(event.raw, 'Args') || rawText(event.raw, 'ArgsText') || call.args,
    output: rawText(event.raw, 'Output') || event.error || rawText(event.raw, 'Err') || call.output,
    tookMs: took > 0 ? Math.round(took) : call.tookMs,
  };
}

function toolEvent(next: LivePhase): Handler {
  return (o, event, now) => {
    const calm = endThinking(o, now);
    const id = rawText(event.raw, 'CallID');
    const known = id || !OPENING.includes(next) ? targetOf(calm, id) : undefined;
    const opened = known || !OPENING.includes(next) ? calm : openCall(calm, event);
    const call = known ?? (OPENING.includes(next) ? opened.calls[opened.calls.length - 1] : undefined);
    if (!call) return opened;
    return { ...opened, retry: undefined, calls: patchCall(opened.calls, call.id, { ...patchOf(call, event, next), startedAt: call.startedAt ?? (next === 'running' ? now : undefined) }) };
  };
}

const onCaption: Handler = (o, event) => {
  const id = rawText(event.raw, 'CallID') || o.calls[o.calls.length - 1]?.id;
  const category = rawText(event.raw, 'Category') || undefined;
  return id ? { ...o, calls: patchCall(o.calls, id, { caption: event.text, category }) } : o;
};

function steerOf(event: EngineEvent): LiveSteer | undefined {
  const steer = rawField(event.raw, 'Steer') as Raw | undefined;
  const id = rawText(steer, 'ID');
  if (!id) return undefined;
  return { id, text: rawText(steer, 'Words'), landing: rawText(steer, 'Landing') || undefined, consumed: false };
}

const onSteerAccepted: Handler = (o, event) => {
  const steer = steerOf(event);
  return steer && !o.steers.some((s) => s.id === steer.id) ? { ...o, steers: [...o.steers, steer] } : o;
};

const onSteerConsumed: Handler = (o, event) => {
  const id = steerOf(event)?.id;
  return { ...o, steers: o.steers.map((s) => (s.id === id ? { ...s, consumed: true, landing: undefined } : s)) };
};

// A fell-through steer becomes the next turn's message, so it is not drawn here.
const onSteerFell: Handler = (o, event) => ({ ...o, steers: o.steers.filter((s) => s.id !== steerOf(event)?.id) });

// Retrying discards everything drawn since the last user line.
const onRetrying: Handler = (o, event) => ({ ...emptyLive(o.baseEntries), startedAt: o.startedAt, steers: o.steers, retry: event.text || 'retrying' });

const onError: Handler = (o, event) => ({ ...o, error: event.error || event.text || 'The engine reported an error.' });

const HANDLERS: Record<string, Handler> = {
  text: addText,
  thinking: (o, _event, now) => ({ ...o, thinking: { ...o.thinking, startedAt: o.thinking.startedAt ?? now } }),
  reasoning: addReasoning,
  toolForming: toolEvent('forming'),
  toolAnnounced: toolEvent('announced'),
  toolBegin: toolEvent('running'),
  toolFinished: toolEvent('finished'),
  toolEnd: toolEvent('done'),
  toolFailed: toolEvent('failed'),
  caption: onCaption,
  assistantDone: (o) => ({ ...o, textDone: true }),
  steerAccepted: onSteerAccepted,
  steerConsumed: onSteerConsumed,
  steerFellThrough: onSteerFell,
  retrying: onRetrying,
  error: onError,
};

const isIdle = (o: LiveOverlayV2) => !o.text && !o.thinking.text && !o.thinking.startedAt && o.calls.length === 0 && o.steers.length === 0;

export function reduceLive(overlay: LiveOverlayV2, event: EngineEvent, entryCount: number, now = Date.now()): LiveOverlayV2 {
  const kind = kindOf(event);
  if (kind === 'turnDone') return emptyLive(entryCount);
  const handler = HANDLERS[kind];
  if (!handler) return overlay;
  const base = isIdle(overlay) ? { ...overlay, baseEntries: entryCount } : overlay;
  return handler({ ...base, startedAt: base.startedAt ?? now }, event, now);
}
