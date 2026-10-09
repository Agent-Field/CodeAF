// Reducer slice: groups. A group is a named capsule of tabs in the strip; it exists only while it has members, and its
// members always stand together as one run (helpers `arrange`, applied after every action).
import { createId, fitIndex, insertAt, nextGroupTitle, normalize, pinnedCount, runOf } from '../helpers.ts';
import type { Tab, TabGroup, WorkspaceState } from '../types.ts';

export type GroupAction =
  /** Makes a new group holding `id` (and, for a multi-selection, every id in `ids`), where the first of them stands. */
  | { type: 'group'; id: string; ids?: string[]; title?: string }
  /** ⌘G: groups the active tab with every tab picked by ⌘-click (Shell "⌘-selecting and pressing ⌘G"). */
  | { type: 'group-picked' }
  /** Moves a tab to the end of a group, or out of every group (to just after the one it leaves) when `groupId` is absent. */
  | { type: 'move-group'; id: string; groupId?: string }
  /**
   * Moves a whole group (Interactions, group label: "Drag moves the whole group") before or `after` a tab, or a whole
   * other group when `targetId` names a group or one of its members. Pinned tabs stay first.
   */
  | { type: 'reorder-group'; id: string; targetId: string; after?: boolean }
  | { type: 'rename-group'; id: string; title: string }
  | { type: 'collapse-group'; id: string }
  | { type: 'ungroup'; id: string };

/** The Inbox is the window's own pinned tab, never a member of a group. */
const groupable = (tab: Tab) => tab.kind !== 'inbox';

function makeGroup(state: WorkspaceState, ids: readonly string[], title?: string): WorkspaceState {
  const members = new Set(ids.filter(id => state.tabs.some(tab => tab.id === id && groupable(tab))));
  if (!members.size) return state;
  const group: TabGroup = { id: createId(), title: title?.trim() || nextGroupTitle(state.groups), collapsed: false };
  // The arrange law gathers the members where the first of them stands, so the new group keeps that place.
  return normalize({ ...state, picked: [], groups: [...state.groups, group], tabs: state.tabs.map(t => (members.has(t.id) ? { ...t, pinned: false, groupId: group.id } : t)) });
}

export function reduceGroups(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as GroupAction;
  switch (a.type) {
    case 'group': return makeGroup(state, [a.id, ...(a.ids ?? [])], a.title);
    case 'group-picked': return makeGroup(state, [state.activeId, ...(state.picked ?? [])]);
    case 'move-group': {
      const tab = state.tabs.find(t => t.id === a.id);
      if (!tab || (a.groupId && !state.groups.some(group => group.id === a.groupId))) return state;
      const groups = state.groups.map(g => (g.id === a.groupId ? { ...g, collapsed: false } : g));
      if (tab.groupId === a.groupId && !tab.pinned) return { ...state, groups };
      const rest = state.tabs.filter(t => t.id !== tab.id);
      const moved: Tab = { ...tab, groupId: a.groupId, pinned: false };
      // Joining goes to the end of the group; leaving goes to just after the group it left, never into its middle.
      const run = runOf(rest, a.groupId ?? tab.groupId);
      const wanted = run ? run.end + 1 : state.tabs.indexOf(tab);
      return normalize({ ...state, groups, tabs: insertAt(rest, moved, fitIndex(rest, moved, wanted)) });
    }
    case 'reorder-group': {
      const members = state.tabs.filter(t => t.groupId === a.id);
      const target = state.tabs.find(t => t.id === a.targetId);
      const targetGroup = state.groups.some(g => g.id === a.targetId) ? a.targetId : target && !target.pinned ? target.groupId : undefined;
      if (!members.length || targetGroup === a.id || (!target && !targetGroup)) return state;
      const rest = state.tabs.filter(t => t.groupId !== a.id);
      const run = runOf(rest, targetGroup);
      const wanted = run ? (a.after ? run.end + 1 : run.start) : rest.indexOf(target!) + (a.after ? 1 : 0);
      const at = Math.max(wanted, pinnedCount(rest));
      const tabs = [...rest.slice(0, at), ...members, ...rest.slice(at)];
      return tabs.every((t, index) => t === state.tabs[index]) ? state : { ...state, tabs };
    }
    // An emptied name takes the next default name no other group has, so two groups are never both "New group".
    case 'rename-group': return { ...state, groups: state.groups.map(g => (g.id === a.id ? { ...g, title: a.title.trim() || nextGroupTitle(state.groups.filter(other => other.id !== a.id)) } : g)) };
    case 'collapse-group': return { ...state, groups: state.groups.map(g => (g.id === a.id ? { ...g, collapsed: !g.collapsed } : g)) };
    case 'ungroup': return { ...state, tabs: state.tabs.map(t => (t.groupId === a.id ? { ...t, groupId: undefined } : t)), groups: state.groups.filter(g => g.id !== a.id) };
    default: return undefined;
  }
}
