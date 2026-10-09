// Reducer slice: groups. A group is a named capsule of tabs in the strip; it exists only while it has members.
import { createId, nextGroupTitle, normalize } from '../helpers.ts';
import type { TabGroup, WorkspaceState } from '../types.ts';

export type GroupAction =
  /** Makes a new group holding `id` (and, for a multi-selection, every id in `ids`). */
  | { type: 'group'; id: string; ids?: string[]; title?: string }
  /** Moves a tab into a group, or out of every group when `groupId` is absent. */
  | { type: 'move-group'; id: string; groupId?: string }
  | { type: 'rename-group'; id: string; title: string }
  | { type: 'collapse-group'; id: string }
  | { type: 'ungroup'; id: string };

export function reduceGroups(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as GroupAction;
  switch (a.type) {
    case 'group': {
      const members = new Set([a.id, ...(a.ids ?? [])]);
      if (!state.tabs.some(tab => members.has(tab.id))) return state;
      const group: TabGroup = { id: createId(), title: a.title?.trim() || nextGroupTitle(state.groups), collapsed: false };
      return normalize({ ...state, groups: [...state.groups, group], tabs: state.tabs.map(t => (members.has(t.id) ? { ...t, pinned: false, groupId: group.id } : t)) });
    }
    case 'move-group': {
      if (!state.tabs.some(tab => tab.id === a.id) || (a.groupId && !state.groups.some(group => group.id === a.groupId))) return state;
      return normalize({ ...state, tabs: state.tabs.map(t => (t.id === a.id ? { ...t, groupId: a.groupId, pinned: false } : t)), groups: state.groups.map(g => (g.id === a.groupId ? { ...g, collapsed: false } : g)) });
    }
    case 'rename-group': return { ...state, groups: state.groups.map(g => (g.id === a.id ? { ...g, title: a.title.trim() || 'New group' } : g)) };
    case 'collapse-group': return { ...state, groups: state.groups.map(g => (g.id === a.id ? { ...g, collapsed: !g.collapsed } : g)) };
    case 'ungroup': return { ...state, tabs: state.tabs.map(t => (t.groupId === a.id ? { ...t, groupId: undefined } : t)), groups: state.groups.filter(g => g.id !== a.id) };
    default: return undefined;
  }
}
