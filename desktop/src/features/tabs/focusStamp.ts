// Tabs opened while Focus is on a place belong to that place (owner decision 2026-10-11, "Places are libraries"), so
// that a new tab never opens and then vanishes from the very strip it was opened in. Pure: the Workspace runs every
// action through this before the reducer sees it, and with no focus an action passes through untouched.
import { placesOf } from './helpers.ts';
import type { Tab } from './types.ts';
import type { WorkspaceAction } from './model.ts';

const joined = (tab: Tab, place: string): Tab => (placesOf(tab).includes(place) ? tab : { ...tab, memberOf: [...(tab.memberOf ?? []), place] });

/** The action with the focused place added to the tab it opens. Selecting, closing and the like are not openings. */
export function stampFocus(action: WorkspaceAction, focus: string | undefined): WorkspaceAction {
  if (!focus) return action;
  if (action.type === 'new') return { ...action, place: action.place ?? focus };
  if (action.type === 'open' || action.type === 'open-task') return { ...action, tab: joined(action.tab, focus) };
  return action;
}
