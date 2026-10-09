// Reducer slice: a tab that has MOVED to another window leaves this one. Adopting a tab sent here is the ordinary `open`.
// A moved tab is not a closed one: it must not appear under Reopen, because it lives on in the window that claimed it.
import { reduceTabs, type TabAction } from './tabs.ts';
import type { WorkspaceState } from '../types.ts';

export type HandoffAction =
  /** The target window claimed the tab: remove it here without a closed record. Unknown ids and split members are left alone. */
  | { type: 'release-moved'; id: string };

export function reduceHandoff(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as HandoffAction;
  if (a.type !== 'release-moved') return undefined;
  const tab = state.tabs.find(t => t.id === a.id);
  if (!tab || tab.split) return state;
  const next = reduceTabs(state, { type: 'close', id: a.id } as TabAction) ?? state;
  // Closing a lone tab makes a fresh one; either way the departed tab must not linger in `closed`.
  return { ...next, closed: next.closed.filter(t => t.id !== a.id) };
}
