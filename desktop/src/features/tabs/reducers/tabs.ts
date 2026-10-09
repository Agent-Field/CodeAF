// Reducer slice: opening, selecting, closing, pinning, naming and ordering tabs.
// A slice returns undefined for an action it does not own; model.ts composes the slices.
import type { TabView } from '../view-state.ts';
import type { TabKind } from '../kinds/types.ts';
import { fitIndex, initialWorkspace, insertAt, mapContent, newConversationTitle, newTabTitle, newTab, normalize, pinnedCount, rankOf, tabHolding, titleRank, visibleTabs } from '../helpers.ts';
import type { ClosedPlace, ClosedTab, Tab, TabGroup, WorkspaceState } from '../types.ts';

export type TabAction =
  | { type: 'new'; groupId?: string; kind?: TabKind }
  | { type: 'open'; tab: Tab; background: boolean }
  /**
   * Opening a task. `from` is the pane it was opened from: the task joins that tab's group (Shell, "Tasks opened from a
   * conversation join its group automatically"), at the end of the group.
   */
  | { type: 'open-task'; tab: Tab; background: boolean; from?: string }
  /** `id` is a tab id, or a pane id inside a split (which also focuses that pane). */
  | { type: 'select'; id: string }
  | { type: 'close'; id: string }
  /** Reopens the newest closed tab where it stood (its recorded place), in its group, made again if closing emptied it. */
  | { type: 'reopen' }
  /**
   * Reopens one chosen closed tab the same way. `before`, `after` and `group` override the recorded place; they are
   * for callers that remembered a place themselves (the closing toast's Undo).
   */
  | { type: 'reopen-id'; id: string; before?: string; after?: string; group?: TabGroup }
  /** ⌘-click (Ctrl-click on Linux): adds a tab to, or takes it out of, the tabs ⌘G will group. The active tab is always in. */
  | { type: 'pick'; id: string }
  | { type: 'pin'; id: string }
  | { type: 'rename'; id: string; title: string }
  | { type: 'title'; id: string; title: string; source: 'message' | 'engine' }
  | { type: 'view'; id: string; change: TabView }
  | { type: 'draft'; id: string; draft: string }
  /**
   * Moves a tab before (or `after`) another. Pinning never changes here: a pinned tab stays among the pinned and a
   * loose one among the loose. A tab dropped between two members of a group joins it. At a group's outer edge only a
   * member stays in, so a tab dropped beside a group sits beside it; joining is the middle of a tab or the group label.
   */
  | { type: 'reorder'; id: string; targetId: string; after?: boolean };

const uncollapse = (state: WorkspaceState, groupId?: string) => state.groups.map(g => (g.id === groupId ? { ...g, collapsed: false } : g));

/** Where a tab stands, as its neighbours and its group, for Reopen. */
function placeOf(state: WorkspaceState, tab: Tab, group = state.groups.find(g => g.id === tab.groupId)): ClosedPlace {
  const index = state.tabs.indexOf(tab);
  return { before: state.tabs[index + 1]?.id, after: state.tabs[index - 1]?.id, group };
}

/** A closed tab with where it stood, for `closed`. A tab of this file's `close` and a pane closed out of a split both use it. */
export const closedRecord = (tab: Tab, place: ClosedPlace): ClosedTab => ({ ...tab, place });

/**
 * Puts a closed tab back: before the tab that followed it, else after the tab that preceded it, else last; in its
 * group, which is made again from the record if closing emptied it. The tab becomes active and leaves `closed`.
 */
export function restore(state: WorkspaceState, closed: ClosedTab, hint: ClosedPlace = {}): WorkspaceState {
  const { place, ...plain } = closed;
  const where: ClosedPlace = { before: hint.before ?? place?.before, after: hint.after ?? place?.after, group: hint.group ?? place?.group };
  const rest = state.closed.filter(t => t.id !== closed.id);
  // A tab that is somehow open already is only selected: two tabs never share an id.
  if (tabHolding(state, plain.id) || plain.split?.panes.some(pane => tabHolding(state, pane.id))) return { ...state, closed: rest };
  const record = where.group?.id === plain.groupId ? where.group : undefined;
  const groups = plain.groupId && !plain.pinned && record && !state.groups.some(g => g.id === record.id) ? [...state.groups, { ...record, collapsed: false }] : state.groups;
  const groupId = !plain.pinned && groups.some(g => g.id === plain.groupId) ? plain.groupId : undefined;
  const back: Tab = { ...plain, groupId };
  const before = state.tabs.findIndex(t => t.id === where.before);
  const after = state.tabs.findIndex(t => t.id === where.after);
  const wanted = before >= 0 ? before : after >= 0 ? after + 1 : state.tabs.length;
  const tabs = insertAt(state.tabs, back, fitIndex(state.tabs, back, wanted));
  return normalize({ ...state, tabs, groups: groups.map(g => (g.id === groupId ? { ...g, collapsed: false } : g)), closed: rest, activeId: back.id, recentIds: [back.id, ...state.recentIds.filter(id => id !== back.id)] });
}

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
      const opener = a.type === 'open-task' && a.from ? tabHolding(state, a.from) : undefined;
      // The arrange law gathers a member at the end of its group's run, so joining is only taking the group's id.
      const tab = opener?.groupId && !opener.pinned ? { ...a.tab, pinned: false, groupId: opener.groupId } : a.tab;
      return { ...state, tabs: [...state.tabs, tab], activeId: a.background ? state.activeId : tab.id, recentIds: a.background ? [...state.recentIds, tab.id] : [tab.id, ...state.recentIds] };
    }
    case 'pick': {
      if (!state.tabs.some(t => t.id === a.id) || a.id === state.activeId) return state;
      const picked = state.picked ?? [];
      return { ...state, picked: picked.includes(a.id) ? picked.filter(id => id !== a.id) : [...picked, a.id] };
    }
    case 'select': {
      const holder = tabHolding(state, a.id);
      const unpicked = state.picked?.length ? { picked: [] } : {};
      if (!holder) return { ...state, ...unpicked, activeId: a.id, recentIds: [a.id, ...state.recentIds.filter(id => id !== a.id)] };
      const index = holder.split?.panes.findIndex(pane => pane.id === a.id) ?? -1;
      const tabs = holder.split && index >= 0 ? state.tabs.map(t => (t.id === holder.id ? { ...t, split: { ...t.split!, focus: index } } : t)) : state.tabs;
      return { ...state, ...unpicked, tabs, activeId: holder.id, recentIds: [holder.id, ...state.recentIds.filter(id => id !== holder.id)], groups: uncollapse(state, holder.groupId) };
    }
    case 'close': {
      const closing = state.tabs.find(t => t.id === a.id);
      if (!closing) return state;
      const order = visibleTabs({ ...state, groups: state.groups });
      const index = order.findIndex(t => t.id === closing.id);
      const remaining = order.filter(t => t.id !== closing.id);
      const tabs = state.tabs.filter(t => t.id !== a.id);
      const closed = [...state.closed.slice(-19), closedRecord(closing, placeOf(state, closing))];
      if (!tabs.length) return { ...initialWorkspace(), closed, nextNumber: state.nextNumber };
      const activeId = state.activeId === a.id ? (remaining[Math.min(Math.max(index, 0), remaining.length - 1)] ?? tabs[0]).id : state.activeId;
      return normalize({ ...state, tabs, closed, activeId });
    }
    case 'reopen': {
      const tab = state.closed[state.closed.length - 1];
      return tab ? restore(state, tab) : state;
    }
    case 'reopen-id': {
      const tab = state.closed.find(t => t.id === a.id);
      return tab ? restore(state, tab, { before: a.before, after: a.after, group: a.group }) : state;
    }
    case 'pin': return normalize({ ...state, tabs: state.tabs.map(t => (t.id === a.id ? { ...t, pinned: !t.pinned, groupId: undefined } : t)) });
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
      if (!tab || !target || tab.id === target.id) return state;
      const rest = state.tabs.filter(t => t.id !== tab.id);
      const pinned = pinnedCount(rest);
      const wanted = rest.indexOf(target) + (a.after ? 1 : 0);
      if (tab.pinned) return normalize({ ...state, tabs: insertAt(rest, tab, Math.min(wanted, pinned)) });
      const at = Math.max(wanted, pinned);
      const left = rest[at - 1]?.groupId;
      const right = rest[at]?.groupId;
      // The last member of a group carries the group wherever it goes: a group of one is moved, never emptied.
      const alone = !!tab.groupId && !rest.some(t => t.groupId === tab.groupId);
      const groupId = left && left === right ? left : tab.groupId && (alone || left === tab.groupId || right === tab.groupId) ? tab.groupId : undefined;
      return normalize({ ...state, tabs: insertAt(rest, { ...tab, groupId }, at) });
    }
    default: return undefined;
  }
}
