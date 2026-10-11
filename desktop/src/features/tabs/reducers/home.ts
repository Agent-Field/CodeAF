// Reducer slice: the Places shell's tab rules. Places are libraries, not workspaces (owner decision 2026-10-11): a
// place's Home is an ORDINARY tab in the one strip, opened from the rail or a page and focused when it is already
// open, so nothing here ever replaces the tab set. A tab belongs to places through `memberOf`; a Home belongs to its
// own place. All places is an ordinary Home tab on `root`.
import { newTab, normalize, panesOf, placesOf } from '../helpers.ts';
import type { Tab, WorkspaceState } from '../types.ts';

export type HomeAction =
  /** Focuses All places, or opens a fresh view when the shortcut or a background press asks for one. */
  | { type: 'home-root'; background?: boolean; fresh?: boolean }
  /** The place's Home as a tab right after the active one, or the tab already showing it. Other tabs stay. */
  | { type: 'home-open'; place: string; title: string; background?: boolean }
  /** Adds a place to a tab's membership (a tab dragged onto a rail row). Nothing moves; a tab may belong to many places. */
  | { type: 'tab-place'; id: string; place: string };

export const ALL_PLACES_TITLE = 'All places';

const isRootHome = (tab: Tab) => !tab.split && tab.kind === 'home' && tab.place === 'root';

/** Inserts a tab right after the active one; a background open leaves focus where it was. Nothing already open is replaced. */
function openAfterActive(state: WorkspaceState, tab: Tab, background: boolean): WorkspaceState {
  const at = state.tabs.findIndex(t => t.id === state.activeId);
  const tabs = [...state.tabs];
  tabs.splice(at < 0 ? tabs.length : at + 1, 0, tab);
  return normalize({ ...state, tabs, activeId: background ? state.activeId : tab.id, recentIds: background ? [...state.recentIds, tab.id] : [tab.id, ...state.recentIds] });
}

export function reduceHome(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as HomeAction;
  switch (a.type) {
    case 'home-root': {
      const root = !a.fresh && !a.background ? state.tabs.find(isRootHome) : undefined;
      if (root) return normalize({ ...state, activeId: root.id, recentIds: [root.id, ...state.recentIds] });
      return openAfterActive(state, newTab({ kind: 'home', place: 'root', title: ALL_PLACES_TITLE, titleSource: 'manual' }), !!a.background);
    }
    case 'home-open': {
      if (a.place === 'root') return reduceHome(state, { type: 'home-root', background: a.background } as HomeAction);
      const shown = state.tabs.find(tab => !tab.split && tab.kind === 'home' && tab.place === a.place);
      if (shown) return a.background ? state : normalize({ ...state, activeId: shown.id, recentIds: [shown.id, ...state.recentIds] });
      return openAfterActive(state, newTab({ kind: 'home', place: a.place, title: a.title, titleSource: 'manual' }), !!a.background);
    }
    case 'tab-place': {
      const tab = state.tabs.find(t => t.id === a.id || panesOf(t).some(pane => pane.id === a.id));
      if (!tab || placesOf(tab).includes(a.place)) return state;
      return { ...state, tabs: state.tabs.map(t => (t.id === tab.id ? { ...t, memberOf: [...(t.memberOf ?? []), a.place] } : t)) };
    }
    default: return undefined;
  }
}
