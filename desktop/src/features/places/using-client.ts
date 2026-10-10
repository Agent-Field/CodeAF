/**
 * Typed client for a conversation's Using list (internal/desktopbridge/using.go).
 *
 * The Using list and the model are fed by one resolver, and the effective
 * settings come from the one rule the engine applies at a turn's opening
 * (internal/session/placegraphpolicy.go). So the UI may say a place's model or
 * permissions are applied ONLY when `settings[].state` is 'applied'; 'pending'
 * means "from the next turn", and every other state means the conversation is
 * NOT running on the place's value, with `reason` saying why in a sentence the
 * engine wrote. Nothing here decides a state of its own.
 *
 * The `token` is the bridge's session token (the `id` of the conversation
 * snapshot), never the chat id: the bridge resolves the chat from the
 * conversation's own transcript.
 */

import { PlacesError, placesTransport, type PlacesTransport, type SourceKind, type Tint } from './client.ts';

export type PolicyField = 'model' | 'permissions';
export type PolicyOutcome = 'agreed' | 'decided' | 'chosen' | 'needsPick';
export type SourceStatus = 'ok' | 'missing' | 'refused';
export type SettingState = 'applied' | 'pending' | 'needsPick' | 'needsYou' | 'yours' | 'notNew' | 'unavailable';

export type UsedPlace = { id: string; name: string; tint: Tint; level: number; inherited: boolean; through?: string[] };
export type UsedInstruction = { placeId: string; alsoFrom?: string[]; text: string; bytes: number; trimmed?: boolean };
/** One knowledge line the Using popover lists. Replaced lines are here and are not part of the prompt. */
export type UsingKnowsLine = {
  id: string;
  placeId: string;
  text: string;
  source: { kind: 'you-wrote' | 'said-in-chat' | 'learned' | 'file'; chatId?: string; at?: string; answers?: number; path?: string };
  createdAt?: string;
  lastUsedAt?: string;
  replacedBy?: string;
  replacedAt?: string;
};
export type SourceOrigin = { placeId: string; sourceId: string; addedBy: 'you' | 'ai'; at?: string; level: number };
export type UsedSource = {
  key: string; kind: SourceKind; ref: string; label?: string; repoRoot?: string;
  from: SourceOrigin[]; status: SourceStatus; reason?: string;
};
export type Want = { placeId: string; value: string };
export type PolicyDecision = {
  field: PolicyField; value: string; outcome: PolicyOutcome;
  /** A place id, 'you' for a remembered pick, or absent while a pick is needed. */
  decidedBy?: string; chosen?: string; wanted: Want[];
};
export type UsingBundle = {
  chatId: string; revision: number;
  places: UsedPlace[]; instructions: UsedInstruction[];
  /** Knowledge lines for the popover. Absent on an older answer; empty when the places have none. */
  knows?: UsingKnowsLine[];
  /** Given to the model. */
  sources: UsedSource[];
  /** Past the source budget: named here, never given. */
  trimmed: UsedSource[];
  /** Failed the source policy (credentials folders, bad URLs ...). */
  refused: UsedSource[];
  policy: PolicyDecision[];
  /** The chip: "Using 3 places · 4 sources". */
  counts: { places: number; sources: number };
};
export type PlaceSetting = {
  field: PolicyField; state: SettingState;
  /** The places' decided value; absent while a pick is needed. */
  value?: string;
  /** What the conversation runs on now. */
  current?: string;
  decidedBy?: string;
  /** Why it is not applied (or when it will be), in the engine's words. */
  reason?: string;
};
export type UsingView = {
  chatId: string;
  /** places false: this conversation's engine reads no place file, so nothing below reached it. */
  engine: { places: boolean; reason?: string };
  revision: number; readAt: string;
  bundle: UsingBundle;
  settings: PlaceSetting[];
};

const FIELDS: readonly string[] = ['model', 'permissions'];
const OUTCOMES: readonly string[] = ['agreed', 'decided', 'chosen', 'needsPick'];
const STATES: readonly string[] = ['applied', 'pending', 'needsPick', 'needsYou', 'yours', 'notNew', 'unavailable'];
const STATUSES: readonly string[] = ['ok', 'missing', 'refused'];
const KNOW_KINDS: readonly string[] = ['you-wrote', 'said-in-chat', 'learned', 'file'];

const isObject = (v: unknown): v is Record<string, unknown> => !!v && typeof v === 'object' && !Array.isArray(v);
const isInt = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0;
const bad = (what: string): never => { throw new PlacesError(`The engine returned an invalid ${what}.`); };

function usedSources(v: unknown, what: string): UsedSource[] {
  if (!Array.isArray(v)) return bad(what);
  for (const s of v) {
    if (!isObject(s) || typeof s.key !== 'string' || typeof s.kind !== 'string' || typeof s.ref !== 'string'
      || !STATUSES.includes(s.status as string) || !Array.isArray(s.from)) bad(what);
  }
  return v as UsedSource[];
}

function bundle(v: unknown): UsingBundle {
  if (!isObject(v) || typeof v.chatId !== 'string' || !isInt(v.revision) || !Array.isArray(v.places) || !Array.isArray(v.instructions)
    || !Array.isArray(v.policy) || !isObject(v.counts) || !isInt(v.counts.places) || !isInt(v.counts.sources)) return bad('Using list');
  for (const p of v.places) if (!isObject(p) || typeof p.id !== 'string' || typeof p.name !== 'string' || !isInt(p.level) || typeof p.inherited !== 'boolean') bad('Using place');
  for (const i of v.instructions) if (!isObject(i) || typeof i.placeId !== 'string' || typeof i.text !== 'string' || !isInt(i.bytes)) bad('Using instruction');
  // Older answers omit knows. A present list must be lines, never a prose blob stuffed into the field.
  const knows: unknown = v.knows;
  if (knows !== undefined && !Array.isArray(knows)) bad('Using knows');
  if (Array.isArray(knows)) {
    for (const line of knows) {
      if (!isObject(line)) bad('Using knows');
      const source = line.source;
      if (typeof line.id !== 'string' || typeof line.placeId !== 'string' || typeof line.text !== 'string' || !isObject(source)) bad('Using knows');
      if (!KNOW_KINDS.includes(String(source.kind))) bad('Using knows');
      if (source.answers !== undefined && !isInt(source.answers)) bad('Using knows');
    }
  }
  usedSources(v.sources, 'Using source'); usedSources(v.trimmed, 'Using source'); usedSources(v.refused, 'Using source');
  for (const d of v.policy) {
    if (!isObject(d) || !FIELDS.includes(d.field as string) || typeof d.value !== 'string' || !OUTCOMES.includes(d.outcome as string) || !Array.isArray(d.wanted)) bad('Using decision');
    // A pick that is needed applies nothing, so it can carry no value.
    if ((d as Record<string, unknown>).outcome === 'needsPick' && (d as Record<string, unknown>).value !== '') bad('Using decision');
  }
  if (v.counts.places !== v.places.length || v.counts.sources !== (v.sources as unknown[]).length) bad('Using count');
  return v as UsingBundle;
}

function using(v: unknown): UsingView {
  if (!isObject(v) || typeof v.chatId !== 'string' || !v.chatId || !isObject(v.engine) || typeof v.engine.places !== 'boolean'
    || !isInt(v.revision) || typeof v.readAt !== 'string' || !Array.isArray(v.settings)) return bad('Using list');
  const b = bundle(v.bundle);
  if (b.chatId !== v.chatId) bad('Using list');
  for (const s of v.settings) {
    if (!isObject(s) || !FIELDS.includes(s.field as string) || !STATES.includes(s.state as string)) bad('Using setting');
    const state = (s as Record<string, unknown>).state;
    // An engine that reads no places applies none of them; a state that claims
    // otherwise is refused here rather than drawn as applied.
    if (!v.engine.places && state !== 'unavailable') bad('Using setting');
    if (state !== 'applied' && state !== 'pending' && typeof (s as Record<string, unknown>).reason !== 'string') bad('Using setting');
  }
  return v as unknown as UsingView;
}

const enc = encodeURIComponent;

export type UsingClient = ReturnType<typeof createUsingClient>;

export function createUsingClient(transport: PlacesTransport = placesTransport) {
  const base = (token: string) => `/sessions/${enc(token)}/using`;
  return {
    /** The conversation's places, instructions, sources and effective settings. */
    using: async (token: string, signal?: AbortSignal) => using(await transport(base(token), { method: 'GET', signal })),
    /**
     * Remembers which place wins a field the places disagree about, for this
     * conversation only. The place must be one of that decision's `wanted`;
     * anything else is refused (422 not_a_candidate). Applied at the next turn.
     */
    choose: async (token: string, field: PolicyField, placeId: string) =>
      using(await transport(`${base(token)}/choice`, { method: 'POST', body: { field, placeId } })),
    /**
     * The person takes the places' decided value as this conversation's own:
     * the one way a wider permissions setting ('needsYou') is opened, and the
     * way a conversation started before its place takes the place's model.
     * Afterwards the field is the person's ('yours') and no place changes it.
     */
    apply: async (token: string, field: PolicyField) =>
      using(await transport(`${base(token)}/apply`, { method: 'POST', body: { field } })),
  };
}

/** Whether the UI may draw a place's value as the one this conversation runs on now. */
export const settingIsApplied = (s: PlaceSetting): boolean => s.state === 'applied';
