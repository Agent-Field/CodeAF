// Reducer slice: what the new-tab field does to its own tab. The field never creates a tab; it turns the
// tab it lives in into something else (a conversation, a file) or gives its place up to a tab that was closed.
import { defaultLayout, layoutFits, makeSplit, mapContent, normalize, paneOf, tabHolding, tabOf, withSplitTitle } from '../helpers.ts';
import type { TabKind } from '../kinds/types.ts';
import type { Pane, TitleSource, WorkspaceState } from '../types.ts';
import { restore } from './tabs.ts';

export type NewTabAction =
  /** The field's tab becomes `kind`. A conversation started from the field carries its session; a file carries its path and the session that can read it. */
  | { type: 'newtab-become'; id: string; kind: TabKind; title: string; titleSource?: TitleSource; sessionFile?: string; path?: string; draft?: string }
  /**
   * Reopens the closed tab `closedId` from the field `id`. The closed tab comes back WHOLE, with its own ids (so every
   * pane keeps its draft, session, terminal binding and the split's focused pane), exactly as ⌘⇧T would bring it back:
   * where it stood and in its group. The field itself is not a closed tab; it simply goes. A field that is a pane of a
   * split takes in a closed plain tab as that pane; a closed split cannot nest in a split, so it reopens as its own tab
   * and the field pane leaves the split.
   */
  | { type: 'newtab-reopen'; id: string; closedId: string };

const becomes = (change: (pane: Pane) => Pane) => (pane: Pane): Pane => (pane.kind === 'newtab' ? change(pane) : pane);

/** Takes the field out of the strip without a closed record: an empty field is nothing to reopen. */
function withoutField(state: WorkspaceState, id: string): WorkspaceState {
  const holder = tabHolding(state, id)!;
  if (holder.id === id) return { ...state, tabs: state.tabs.filter(t => t.id !== id) };
  const split = holder.split!;
  const panes = split.panes.filter(pane => pane.id !== id);
  const index = split.panes.findIndex(pane => pane.id === id);
  const next = panes.length === 1
    ? tabOf(panes[0], { pinned: holder.pinned, groupId: holder.groupId })
    : withSplitTitle({ ...holder, split: makeSplit(panes, index < split.focus ? split.focus - 1 : Math.min(split.focus, panes.length - 1), layoutFits(split.layout, panes.length) ? split.layout : defaultLayout(panes.length)) });
  return { ...state, tabs: state.tabs.map(t => (t.id === holder.id ? next : t)), recentIds: state.recentIds.map(r => (r === holder.id ? next.id : r)) };
}

export function reduceNewTab(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as NewTabAction;
  switch (a.type) {
    case 'newtab-become':
      return mapContent(state, a.id, becomes(pane => ({ ...pane, kind: a.kind, title: a.title, titleSource: a.titleSource ?? 'manual', draft: a.draft ?? '', sessionFile: a.sessionFile, path: a.path, file: a.path && (a.kind === 'file' || a.kind === 'diff') ? { path: a.path, view: a.kind === 'diff' ? 'changes' : 'file' } : undefined })));
    case 'newtab-reopen': {
      const closed = state.closed.find(tab => tab.id === a.closedId);
      const holder = tabHolding(state, a.id);
      const field = holder && (holder.id === a.id ? holder : holder.split?.panes.find(pane => pane.id === a.id));
      if (!closed || !holder || field?.kind !== 'newtab') return state;
      if (holder.id !== a.id && !closed.split) {
        // A field pane takes in a closed plain tab as itself, under the closed tab's own id.
        const pane: Pane = paneOf(closed);
        const split = holder.split!;
        const tab = withSplitTitle({ ...holder, split: { ...split, panes: split.panes.map(p => (p.id === a.id ? pane : p)) } });
        return normalize({ ...state, tabs: state.tabs.map(t => (t.id === holder.id ? tab : t)), closed: state.closed.filter(t => t.id !== closed.id) });
      }
      const without = withoutField(state, a.id);
      return restore({ ...without, recentIds: without.recentIds.filter(id => id !== a.id) }, closed);
    }
  }
  return undefined;
}
