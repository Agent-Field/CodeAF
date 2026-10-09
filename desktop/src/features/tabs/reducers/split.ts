// Reducer slice: split tabs. A split is ONE merged tab holding two to four panes (Split in types.ts).
// Merging, closing a pane, moving focus, changing layout and unmerging are all pure here; the drag
// gestures that call them belong to the split lane.
import { clampRatio, createId, defaultLayout, layoutFits, makeSplit, normalize, paneOf, splitCapacity, tabOf, withSplitTitle } from '../helpers.ts';
import { closedRecord } from './tabs.ts';
import type { SplitLayout, Tab, WorkspaceState } from '../types.ts';

export type SplitAction =
  /** Merges tab `withId` into tab `id` as new pane(s). If `id` is already a split the panes are appended. The new pane takes focus. */
  | { type: 'split-merge'; id: string; withId: string; layout?: SplitLayout; at?: 'start' | 'end' }
  /** Moves a divider. `col` and `row` are shares (0..1) of the first column and first row; either may be omitted. */
  | { type: 'split-resize'; id: string; col?: number; row?: number }
  /** Trades the places of two panes of a split ("Swap" in the pane menu). Focus stays with the pane that had it. */
  | { type: 'split-swap'; id: string; paneId: string; withPaneId: string }
  /** Closes one pane. A split left with one pane becomes the plain tab it was. */
  | { type: 'split-close-pane'; id: string; paneId: string }
  | { type: 'split-focus'; id: string; index: number }
  | { type: 'split-layout'; id: string; layout: SplitLayout }
  /** Breaks a split back into one tab per pane, in place. */
  | { type: 'split-unmerge'; id: string }
  /** Opens a group as a split: its first (up to) four plain tabs merge into one. */
  | { type: 'split-group'; groupId: string };

const swapId = (ids: string[], from: string, to: string) => [...new Set(ids.map(id => (id === from ? to : id)))];

export function reduceSplit(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as SplitAction;
  switch (a.type) {
    case 'split-merge': return merge(state, a.id, a.withId, a.layout, a.at);
    case 'split-resize': {
      const tab = state.tabs.find(t => t.id === a.id);
      if (!tab?.split) return state;
      const now = tab.split.ratios ?? { col: 0.5, row: 0.5 };
      const ratios = { col: a.col === undefined ? now.col : clampRatio(a.col), row: a.row === undefined ? now.row : clampRatio(a.row) };
      if (ratios.col === now.col && ratios.row === now.row) return state;
      return { ...state, tabs: state.tabs.map(t => (t.id === a.id ? { ...t, split: { ...t.split!, ratios } } : t)) };
    }
    case 'split-swap': {
      const tab = state.tabs.find(t => t.id === a.id);
      const split = tab?.split;
      const from = split ? split.panes.findIndex(p => p.id === a.paneId) : -1;
      const to = split ? split.panes.findIndex(p => p.id === a.withPaneId) : -1;
      if (!tab || !split || from < 0 || to < 0 || from === to) return state;
      const panes = [...split.panes];
      [panes[from], panes[to]] = [panes[to], panes[from]];
      const held = split.panes[split.focus].id;
      const next = withSplitTitle({ ...tab, split: makeSplit(panes, panes.findIndex(p => p.id === held), split.layout, split.ratios) });
      return { ...state, tabs: state.tabs.map(t => (t.id === tab.id ? next : t)) };
    }
    case 'split-group': {
      const members = state.tabs.filter(t => t.groupId === a.groupId && !t.split && !t.pinned).slice(0, splitCapacity);
      if (members.length < 2) return state;
      let next = merge(state, members[0].id, members[1].id);
      // Each merge makes the merged tab active, so its id is the host for the next one.
      for (const member of members.slice(2)) next = merge(next, next.activeId, member.id);
      return next;
    }
    case 'split-close-pane': {
      const tab = state.tabs.find(t => t.id === a.id);
      const split = tab?.split;
      const index = split ? split.panes.findIndex(p => p.id === a.paneId) : -1;
      if (!tab || !split || index < 0) return state;
      const panes = split.panes.filter(p => p.id !== a.paneId);
      // A pane closed out of a split reopens as its own tab just after the split (or the tab the split falls back to), in the split's group.
      const after = panes.length === 1 ? panes[0].id : tab.id;
      const closed = [...state.closed.slice(-19), closedRecord(tabOf(split.panes[index], { groupId: tab.groupId }), { after, group: state.groups.find(g => g.id === tab.groupId) })];
      if (panes.length === 1) {
        // Back to the plain tab it was: same strip place, pin and group, the surviving pane's content and id.
        const rest = tabOf(panes[0], { pinned: tab.pinned, groupId: tab.groupId });
        return normalize({ ...state, closed, tabs: state.tabs.map(t => (t.id === tab.id ? rest : t)), activeId: state.activeId === tab.id ? rest.id : state.activeId, recentIds: swapId(state.recentIds, tab.id, rest.id) });
      }
      const focus = index < split.focus ? split.focus - 1 : Math.min(split.focus, panes.length - 1);
      const layout = layoutFits(split.layout, panes.length) ? split.layout : defaultLayout(panes.length);
      return normalize({ ...state, closed, tabs: state.tabs.map(t => (t.id === tab.id ? withSplitTitle({ ...t, split: makeSplit(panes, focus, layout) }) : t)) });
    }
    case 'split-focus': {
      const tab = state.tabs.find(t => t.id === a.id);
      if (!tab?.split || a.index < 0 || a.index >= tab.split.panes.length || a.index === tab.split.focus) return state;
      return { ...state, tabs: state.tabs.map(t => (t.id === a.id ? { ...t, split: { ...t.split!, focus: a.index } } : t)) };
    }
    case 'split-layout': {
      const tab = state.tabs.find(t => t.id === a.id);
      if (!tab?.split || tab.split.layout === a.layout || !layoutFits(a.layout, tab.split.panes.length)) return state;
      return { ...state, tabs: state.tabs.map(t => (t.id === a.id ? { ...t, split: { ...t.split!, layout: a.layout } } : t)) };
    }
    case 'split-unmerge': {
      const tab = state.tabs.find(t => t.id === a.id);
      if (!tab?.split) return state;
      const split = tab.split;
      const apart = split.panes.map(pane => tabOf(pane, { pinned: tab.pinned, groupId: tab.groupId }));
      const at = state.tabs.findIndex(t => t.id === tab.id);
      const tabs = [...state.tabs.slice(0, at), ...apart, ...state.tabs.slice(at + 1)];
      const activeId = state.activeId === tab.id ? (apart[split.focus] ?? apart[0]).id : state.activeId;
      return normalize({ ...state, tabs, activeId, recentIds: [activeId, ...apart.map(t => t.id), ...state.recentIds.filter(id => id !== tab.id)] });
    }
    default: return undefined;
  }
}

function merge(state: WorkspaceState, id: string, withId: string, layout?: SplitLayout, at: 'start' | 'end' = 'end'): WorkspaceState {
  const host = state.tabs.find(t => t.id === id);
  const guest = state.tabs.find(t => t.id === withId);
  if (!host || !guest || host.id === guest.id || host.pinned || guest.pinned) return state;
  const own = host.split ? host.split.panes : [paneOf(host)];
  const joining = guest.split ? guest.split.panes : [paneOf(guest)];
  const panes = at === 'start' ? [...joining, ...own] : [...own, ...joining];
  if (panes.length > splitCapacity) return state;
  const kept = host.split && !layout && layoutFits(host.split.layout, panes.length) ? host.split.layout : undefined;
  // A split tab's own content fields are unused: it carries only its place in the strip and its panes.
  const merged: Tab = withSplitTitle({
    id: host.split ? host.id : createId(), kind: panes[0].kind, title: host.split ? host.title : '', draft: '', titleSource: host.split ? host.titleSource : undefined,
    pinned: false, groupId: host.groupId, split: makeSplit(panes, at === 'start' ? 0 : own.length, layout ?? kept),
  });
  const tabs = state.tabs.filter(t => t.id !== guest.id).map(t => (t.id === host.id ? merged : t));
  const recentIds = [merged.id, ...state.recentIds.filter(r => r !== host.id && r !== guest.id)];
  return normalize({ ...state, tabs, activeId: merged.id, recentIds, groups: state.groups.map(g => (g.id === merged.groupId ? { ...g, collapsed: false } : g)) });
}
