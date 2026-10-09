/**
 * The shapes of a conversation's Using list, as the bridge sends them
 * (GET /sessions/{token}/using; internal/desktopbridge/using.go).
 *
 * The typed client that fetches them is owned by the places-runtime lane
 * (`using-client.ts`). These are the same shapes, kept here so the Using
 * components compile and are tested against the contract before that file
 * reaches this branch; `createUsingClient()` from it satisfies `UsingApi`
 * without an adapter. Nothing here decides a state: `settings[].state` is the
 * engine's, and the UI draws a place's model or permissions as in force only
 * when it says 'applied'.
 */

import type { SourceKind, Tint } from './client.ts';

export type PolicyField = 'model' | 'permissions';
export type PolicyOutcome = 'agreed' | 'decided' | 'chosen' | 'needsPick';
export type SourceStatus = 'ok' | 'missing' | 'refused';
export type SettingState = 'applied' | 'pending' | 'needsPick' | 'needsYou' | 'yours' | 'notNew' | 'unavailable';

export type UsedPlace = { id: string; name: string; tint: Tint; level: number; inherited: boolean; through?: string[] };
export type UsedInstruction = { placeId: string; alsoFrom?: string[]; text: string; bytes: number; trimmed?: boolean };
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
  /** Given to the model. */
  sources: UsedSource[];
  /** Past the source budget: named, never given. */
  trimmed: UsedSource[];
  /** Failed the source policy: named with the reason, never given. */
  refused: UsedSource[];
  policy: PolicyDecision[];
  counts: { places: number; sources: number };
};
export type PlaceSetting = {
  field: PolicyField; state: SettingState;
  value?: string; current?: string; decidedBy?: string;
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

/** The three calls the Using list makes. `token` is the bridge's session token, never the chat id. */
export type UsingApi = {
  using: (token: string, signal?: AbortSignal) => Promise<UsingView>;
  choose: (token: string, field: PolicyField, placeId: string) => Promise<UsingView>;
  apply: (token: string, field: PolicyField) => Promise<UsingView>;
};

/**
 * What opening a source asks of whoever owns navigation. The Using list never
 * opens anything itself: with no `onOpenSource` there is no open control, so a
 * button is never drawn that goes nowhere. A path is absolute and was stat-checked
 * by the engine; a url is http(s) only.
 */
export type SourceHandoff =
  | { kind: 'file' | 'folder' | 'repo'; path: string; repoRoot?: string }
  | { kind: 'url'; url: string }
  | { kind: 'chat'; chatId: string };
