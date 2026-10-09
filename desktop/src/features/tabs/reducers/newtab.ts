// Reducer slice: what the new-tab field does to its own tab. The field never creates a tab; it turns the
// tab it lives in into something else (a conversation, a file) or into a tab that was closed.
import { mapContent, paneOf } from '../helpers.ts';
import type { TabKind } from '../kinds/types.ts';
import type { Pane, TitleSource, WorkspaceState } from '../types.ts';

export type NewTabAction =
  /** The field's tab becomes `kind`. A conversation started from the field carries its session; a file carries its path and the session that can read it. */
  | { type: 'newtab-become'; id: string; kind: TabKind; title: string; titleSource?: TitleSource; sessionFile?: string; path?: string; draft?: string }
  /** The field's tab becomes the closed tab `closedId`, which leaves the closed list. */
  | { type: 'newtab-reopen'; id: string; closedId: string };

const becomes = (change: (pane: Pane) => Pane) => (pane: Pane): Pane => (pane.kind === 'newtab' ? change(pane) : pane);

export function reduceNewTab(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as NewTabAction;
  switch (a.type) {
    case 'newtab-become':
      return mapContent(state, a.id, becomes(pane => ({ ...pane, kind: a.kind, title: a.title, titleSource: a.titleSource ?? 'manual', draft: a.draft ?? '', sessionFile: a.sessionFile, path: a.path })));
    case 'newtab-reopen': {
      const closed = state.closed.find(tab => tab.id === a.closedId);
      if (!closed) return state;
      const source = closed.split ? closed.split.panes[closed.split.focus] ?? closed.split.panes[0] : paneOf(closed);
      const next = mapContent(state, a.id, becomes(pane => ({ ...pane, ...source, id: pane.id })));
      return next === state ? state : { ...next, closed: next.closed.filter(tab => tab.id !== a.closedId) };
    }
  }
  return undefined;
}
