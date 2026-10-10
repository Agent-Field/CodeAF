// Reducer slice: groups. A group is a named capsule of tabs in the strip; it exists only while it has members, and its
// members always stand together as one run (helpers `arrange`, applied after every action).
import { createId, fitIndex, insertAt, nextGroupTitle, normalize, pinnedCount, runOf } from '../helpers.ts';
import type { Tab, TabGroup, WorkspaceState } from '../types.ts';

export type GroupAction =
  /** Makes a new group holding `id` (and, for a multi-selection, every id in `ids`), where the first of them stands. */
  | { type: 'group'; id: string; ids?: string[]; title?: string }
  /** ⌘G: groups the active tab with every tab picked by ⌘-click (Shell "⌘-selecting and pressing ⌘G"). */
  | { type: 'group-picked' }
  /**
   * Moves a tab to the end of a group, or out of every group (to just after the one it leaves) when `groupId` is absent.
   * `beside` is the tab it was dropped on in the overview: it lands just before (or `after`) that tab when that tab is
   * in the destination, and never inside another group's run.
   */
  | { type: 'move-group'; id: string; groupId?: string; beside?: { id: string; after?: boolean } }
  /**
   * Moves a whole group (Interactions, group label: "Drag moves the whole group") before or `after` a tab, or a whole
   * other group when `targetId` names a group or one of its members. Pinned tabs stay first.
   */
  | { type: 'reorder-group'; id: string; targetId: string; after?: boolean }
  /** The same move under the name the group label's drag and Alt+Shift+←/→ use (S-3g-18): the block keeps its member order. */
  | { type: 'move-group-block'; id: string; targetId: string; after?: boolean }
  | { type: 'rename-group'; id: string; title: string }
  | { type: 'collapse-group'; id: string }
  | { type: 'ungroup'; id: string };

/** Group only ids that are still present after a workspace rebase. */
function makeGroup(state: WorkspaceState, ids: readonly string[], title?: string): WorkspaceState {
  const members = new Set(ids.filter(id => state.tabs.some(tab => tab.id === id)));
  if (!members.size) return state;
  const group: TabGroup = { id: createId(), title: title?.trim() || nextGroupTitle(state.groups), collapsed: false };
  // The arrange law gathers the members where the first of them stands, so the new group keeps that place.
  return normalize({ ...state, picked: [], groups: [...state.groups, group], tabs: state.tabs.map(t => (members.has(t.id) ? { ...t, pinned: false, groupId: group.id } : t)) });
}

/** A group's block slot in the strip: loose tabs and whole groups in order, pinned tabs excluded (they never move). */
export function blockSlots(tabs: readonly Tab[]): string[] {
  const slots: string[] = [];
  for (const tab of tabs) {
    const slot = tab.groupId ?? tab.id;
    if (!tab.pinned && !slots.includes(slot)) slots.push(slot);
  }
  return slots;
}

/** What Alt+Shift+←/→ drops the group beside: the neighbouring slot in that direction, or undefined at the strip's end. */
export const blockNeighbour = (tabs: readonly Tab[], groupId: string, dir: -1 | 1): string | undefined => {
  const slots = blockSlots(tabs);
  return slots[slots.indexOf(groupId) + dir];
};

function moveBlock(state: WorkspaceState, id: string, targetId: string, after?: boolean): WorkspaceState {
  const members = state.tabs.filter(t => t.groupId === id);
  const target = state.tabs.find(t => t.id === targetId);
  const targetGroup = state.groups.some(g => g.id === targetId) ? targetId : target && !target.pinned ? target.groupId : undefined;
  if (!members.length || targetGroup === id || (!target && !targetGroup)) return state;
  const rest = state.tabs.filter(t => t.groupId !== id);
  const run = runOf(rest, targetGroup);
  const wanted = run ? (after ? run.end + 1 : run.start) : rest.indexOf(target!) + (after ? 1 : 0);
  // Pinned tabs stand first whatever the drop says, so a block dropped before one lands after the last pin.
  const at = Math.max(wanted, pinnedCount(rest));
  const tabs = [...rest.slice(0, at), ...members, ...rest.slice(at)];
  return tabs.every((t, index) => t === state.tabs[index]) ? state : { ...state, tabs };
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
      const rest = state.tabs.filter(t => t.id !== tab.id);
      const anchor = a.beside && a.beside.id !== tab.id ? rest.find(t => t.id === a.beside!.id && !t.pinned && t.groupId === a.groupId) : undefined;
      if (tab.groupId === a.groupId && !tab.pinned && !anchor) return { ...state, groups };
      const moved: Tab = { ...tab, groupId: a.groupId, pinned: false };
      // Joining goes to the end of the group; leaving goes to just after the group it left, never into its middle.
      const run = runOf(rest, a.groupId ?? tab.groupId);
      const wanted = anchor ? rest.indexOf(anchor) + (a.beside!.after ? 1 : 0) : run ? run.end + 1 : state.tabs.indexOf(tab);
      return normalize({ ...state, groups, tabs: insertAt(rest, moved, fitIndex(rest, moved, wanted)) });
    }
    case 'reorder-group':
    case 'move-group-block': return moveBlock(state, a.id, a.targetId, a.after);
    // An emptied name takes the next default name no other group has, so two groups are never both "New group".
    case 'rename-group': return { ...state, groups: state.groups.map(g => (g.id === a.id ? { ...g, title: a.title.trim() || nextGroupTitle(state.groups.filter(other => other.id !== a.id)) } : g)) };
    case 'collapse-group': return { ...state, groups: state.groups.map(g => (g.id === a.id ? { ...g, collapsed: !g.collapsed } : g)) };
    case 'ungroup': return { ...state, tabs: state.tabs.map(t => (t.groupId === a.id ? { ...t, groupId: undefined } : t)), groups: state.groups.filter(g => g.id !== a.id) };
    default: return undefined;
  }
}
