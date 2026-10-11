// What Focus means for the strip, kept pure (owner decision 2026-10-11, "Places are libraries"). Focus is a FILTER on
// the one tab set: it never closes, moves or regroups a tab, so everything here only reads.
import { inFocus, placesOf } from './helpers.ts';
import type { Tab, WorkspaceState } from './types.ts';

/** The place the shell shows for the tab in front: the focused place when the tab belongs to it, else its first place, else Now. */
export function shownPlace(tab: Pick<Tab, 'kind' | 'place' | 'memberOf'>, focus: string | undefined): string {
  const places = placesOf(tab);
  if (focus && places.includes(focus)) return focus;
  const first = places[0];
  return !first || first === 'root' ? 'now' : first;
}

/** The tab to bring forward when Focus is on a place and the active tab is not in it: the most recently used member. */
export function landingFor(state: Pick<WorkspaceState, 'tabs' | 'recentIds'>, focus: string): string | undefined {
  const members = new Set(inFocus(state.tabs, focus).map(tab => tab.id));
  return state.recentIds.find(id => members.has(id)) ?? [...members][0];
}
