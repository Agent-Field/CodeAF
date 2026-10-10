/**
 * The shapes of a conversation's Using list, as the bridge sends them
 * (GET /sessions/{token}/using; internal/desktopbridge/using.go).
 *
 * The typed client that fetches and validates them is owned by the places-runtime
 * lane (`using-client.ts`); every shape below is re-exported from it, never copied,
 * so the contract has one source. This file adds only what the components need that
 * the client does not say: the injectable `UsingApi` seam and the `SourceHandoff` a
 * source opens with. Nothing here decides a state: `settings[].state` is the engine's,
 * and the UI draws a place's model or permissions as in force only when it says 'applied'.
 */

export type {
  PlaceSetting, PolicyDecision, PolicyField, PolicyOutcome, SettingState, SourceOrigin, SourceStatus, UsedInstruction, UsedPlace, UsingKnowsLine,
  UsedSource, UsingBundle, UsingView, Want,
} from './using-client.ts';
import type { PolicyField, UsingView } from './using-client.ts';

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
