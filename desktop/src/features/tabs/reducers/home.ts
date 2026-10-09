// Reducer slice: the Places shell's tab rules. A place's strip starts with its Home, pinned, which never closes,
// never joins a group or a split and never moves off the first slot (Places 6e "Home is the pinned first tab of every
// place (⌘0)"; Shell 3j). All places is an ordinary Home tab on `root` that lives in whatever strip it was opened from
// (Places 9e). `adopt` takes the tab set another window wrote for the same place (Places 6d "Same place, two windows").
import { newTab, normalize, panesOf } from '../helpers.ts';
import type { Tab, WorkspaceState } from '../types.ts';

export type HomeAction =
  /** Makes the place's Home the pinned first tab, titled with the place's name; `focus` selects it (Go to lands on Home). */
  | { type: 'home-ensure'; place: string; title: string; focus?: boolean }
  /** Focuses the All places tab of this strip, or opens one (after the active tab, or at the end in the background). */
  | { type: 'home-root'; background?: boolean }
  /** Another window saved this place's tabs: take them, keeping this window's own focus when that tab still exists. */
  | { type: 'adopt'; state: WorkspaceState };

export const ALL_PLACES_TITLE = 'All places';

/** True for a tab that is this strip's place Home (not All places, which is an ordinary tab). */
export const isPlaceHome = (tab: Pick<Tab, 'kind' | 'place'>) => tab.kind === 'home' && !!tab.place && tab.place !== 'root';
const isRootHome = (tab: Tab) => !tab.split && tab.kind === 'home' && tab.place === 'root';
const holdsPlaceHome = (state: WorkspaceState, id: string | undefined) => !!id && state.tabs.some(tab => (tab.id === id || panesOf(tab).some(pane => pane.id === id)) && panesOf(tab).some(isPlaceHome));

/** The tab set a strip shares with other windows: everything but this window's own focus and recency. */
export const sharedShape = (state: WorkspaceState) => JSON.stringify([state.tabs, state.groups, state.closed]);

export function reduceHome(state: WorkspaceState, action: { type: string }): WorkspaceState | undefined {
  const a = action as HomeAction | { type: string; id?: string; withId?: string; targetId?: string };
  switch (a.type) {
    case 'home-ensure': {
      const { place, title, focus } = a as Extract<HomeAction, { type: 'home-ensure' }>;
      const existing = state.tabs.find(tab => isPlaceHome(tab) && tab.place === place);
      const home: Tab = existing
        ? { ...existing, title, titleSource: 'manual', pinned: true, groupId: undefined }
        : newTab({ kind: 'home', place, title, titleSource: 'manual', pinned: true });
      // Any other place's Home that slipped into this strip becomes nothing: a strip shows one place.
      const rest = state.tabs.filter(tab => tab.id !== home.id && !isPlaceHome(tab));
      const tabs = [home, ...rest];
      const unchanged = existing && state.tabs[0]?.id === home.id && existing.title === title && existing.pinned && rest.length === state.tabs.length - 1;
      if (unchanged && (!focus || state.activeId === home.id)) return state;
      const activeId = focus ? home.id : tabs.some(tab => tab.id === state.activeId) ? state.activeId : home.id;
      return normalize({ ...state, tabs, activeId, recentIds: focus ? [home.id, ...state.recentIds] : state.recentIds });
    }
    case 'home-root': {
      const { background } = a as Extract<HomeAction, { type: 'home-root' }>;
      const root = state.tabs.find(isRootHome);
      if (root) return background ? state : normalize({ ...state, activeId: root.id, recentIds: [root.id, ...state.recentIds] });
      const tab = newTab({ kind: 'home', place: 'root', title: ALL_PLACES_TITLE, titleSource: 'manual' });
      const at = state.tabs.findIndex(t => t.id === state.activeId);
      const tabs = [...state.tabs];
      tabs.splice(at < 0 ? tabs.length : at + 1, 0, tab);
      return normalize({ ...state, tabs, activeId: background ? state.activeId : tab.id, recentIds: background ? [...state.recentIds, tab.id] : [tab.id, ...state.recentIds] });
    }
    case 'adopt': {
      const incoming = (a as Extract<HomeAction, { type: 'adopt' }>).state;
      if (sharedShape(incoming) === sharedShape(state)) return state;
      const activeId = incoming.tabs.some(tab => tab.id === state.activeId) ? state.activeId : incoming.activeId;
      return normalize({ ...incoming, activeId, recentIds: [activeId, ...state.recentIds] });
    }
    // The place Home never joins a split or a group: those slices never see it.
    case 'split-merge': {
      const m = a as { id?: string; withId?: string };
      return holdsPlaceHome(state, m.id) || holdsPlaceHome(state, m.withId) ? state : undefined;
    }
    case 'group':
    case 'move-group':
      return holdsPlaceHome(state, (a as { id?: string }).id) ? state : undefined;
    default: return undefined;
  }
}
