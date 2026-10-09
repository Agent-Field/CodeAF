// Reducer slice: opening, selecting, closing, pinning, naming and ordering tabs.
// A slice returns undefined for an action it does not own; model.ts composes the slices.
import type { TabView } from '../view-state.ts';
import type { TabKind } from '../kinds/types.ts';
import { initialWorkspace, mapContent, newConversationTitle, newTabTitle, newTab, normalize, rankOf, tabHolding, titleRank, visibleTabs } from '../helpers.ts';
import type { Tab, WorkspaceState } from '../types.ts';
import { isPlaceHome } from './home.ts';

export type TabAction =
  | { type: 'new'; groupId?: string; kind?: TabKind }
  /** `at` places the tab at that strip index (a chat started from a place's Home opens right after Home); default the end. */
  | { type: 'open'; tab: Tab; background: boolean; at?: number }
  /** Kept for the task panel: opening a task is `open` with a task tab. */
  | { type: 'open-task'; tab: Tab; background: boolean }
  /** `id` is a tab id, or a pane id inside a split (which also focuses that pane). */
  | { type: 'select'; id: string }
  | { type: 'close'; id: string }
  | { type: 'reopen' }
  | { type: 'pin'; id: string }
  | { type: 'rename'; id: string; title: string }
  | { type: 'title'; id: string; title: string; source: 'message' | 'engine' }
  | { type: 'view'; id: string; change: TabView }
  | { type: 'draft'; id: string; draft: string }
  | { type: 'reorder'; id: string; targetId: string; after?: boolean };

const uncollapse = (state: WorkspaceState, groupId?: string) => state.groups.map(g => (g.id === groupId ? { ...g, collapsed: false } : g));

export function reduceTabs(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as TabAction;
  switch (a.type) {
    case 'new': {
      const kind = a.kind ?? 'newtab';
      const tab = newTab({ groupId: a.groupId, kind, ...(kind === 'newtab' ? { title: newTabTitle } : {}) });
      return { ...state, tabs: [...state.tabs, tab], activeId: tab.id, recentIds: [tab.id, ...state.recentIds], nextNumber: state.nextNumber + 1, groups: uncollapse(state, a.groupId) };
    }
    case 'open':
    case 'open-task': {
      const tabs = [...state.tabs];
      const at = a.type === 'open' && a.at !== undefined ? Math.min(Math.max(a.at, 0), tabs.length) : tabs.length;
      tabs.splice(at, 0, a.tab);
      return { ...state, tabs, activeId: a.background ? state.activeId : a.tab.id, recentIds: a.background ? [...state.recentIds, a.tab.id] : [a.tab.id, ...state.recentIds] };
    }
    case 'select': {
      const holder = tabHolding(state, a.id);
      if (!holder) return { ...state, activeId: a.id, recentIds: [a.id, ...state.recentIds.filter(id => id !== a.id)] };
      const index = holder.split?.panes.findIndex(pane => pane.id === a.id) ?? -1;
      const tabs = holder.split && index >= 0 ? state.tabs.map(t => (t.id === holder.id ? { ...t, split: { ...t.split!, focus: index } } : t)) : state.tabs;
      return { ...state, tabs, activeId: holder.id, recentIds: [holder.id, ...state.recentIds.filter(id => id !== holder.id)], groups: uncollapse(state, holder.groupId) };
    }
    case 'close': {
      const closing = state.tabs.find(t => t.id === a.id);
      // A place's Home is the strip's first tab and never closes (Interactions "Home tab (pinned)").
      if (!closing || isPlaceHome(closing)) return state;
      const order = visibleTabs({ ...state, groups: state.groups });
      const index = order.findIndex(t => t.id === closing.id);
      const remaining = order.filter(t => t.id !== closing.id);
      const tabs = state.tabs.filter(t => t.id !== a.id);
      const closed = [...state.closed.slice(-19), closing];
      if (!tabs.length) return { ...initialWorkspace(), closed, nextNumber: state.nextNumber };
      const activeId = state.activeId === a.id ? (remaining[Math.min(Math.max(index, 0), remaining.length - 1)] ?? tabs[0]).id : state.activeId;
      return normalize({ ...state, tabs, closed, activeId });
    }
    case 'reopen': {
      const tab = state.closed[state.closed.length - 1];
      if (!tab) return state;
      const groupId = state.groups.some(g => g.id === tab.groupId) ? tab.groupId : undefined;
      return { ...state, tabs: [...state.tabs, { ...tab, groupId }], closed: state.closed.slice(0, -1), activeId: tab.id, recentIds: [tab.id, ...state.recentIds.filter(id => id !== tab.id)], groups: uncollapse(state, groupId) };
    }
    case 'pin': if (state.tabs.some(t => t.id === a.id && isPlaceHome(t))) return state; return normalize({ ...state, tabs: state.tabs.map(t => (t.id === a.id ? { ...t, pinned: !t.pinned, groupId: undefined } : t)) });
    case 'rename': return mapContent(state, a.id, pane => ({ ...pane, title: a.title.trim() || newConversationTitle, titleSource: 'manual' }));
    case 'title': {
      const title = a.title.trim();
      let allowed = false;
      const next = mapContent(state, a.id, pane => {
        if (!title || pane.title === title || rankOf(pane) > titleRank[a.source]) return pane;
        allowed = true;
        return { ...pane, title, titleSource: a.source };
      });
      return allowed ? next : state;
    }
    case 'view': return mapContent(state, a.id, pane => ({ ...pane, ...a.change }));
    case 'draft': return mapContent(state, a.id, pane => ({ ...pane, draft: a.draft }));
    case 'reorder': {
      const tab = state.tabs.find(t => t.id === a.id);
      const target = state.tabs.find(t => t.id === a.targetId);
      if (!tab || !target || tab.id === target.id || isPlaceHome(tab) || (isPlaceHome(target) && !a.after)) return state;
      const tabs = state.tabs.filter(t => t.id !== tab.id);
      tabs.splice(tabs.findIndex(t => t.id === target.id) + (a.after ? 1 : 0), 0, { ...tab, pinned: target.pinned, groupId: target.groupId });
      return normalize({ ...state, tabs });
    }
    default: return undefined;
  }
}
