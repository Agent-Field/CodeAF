// Pointer rules for a strip tab that the design leaves to a choice (SH-OQ4).
// Middle-click is the same close as the tab's ×: the view goes, the work keeps running.
import { isPlaceHome } from './reducers/home.ts';
import type { Tab } from './types.ts';

/**
 * Whether a middle-click closes this strip tab. A pinned tab has no × and ⌘W leaves it open, and a place Home
 * never closes, so neither goes. Every other tab does, including a split, which is one merged tab.
 */
export function stripMiddleCloses(tab: Pick<Tab, 'pinned' | 'kind' | 'place'>): boolean {
  return !tab.pinned && !isPlaceHome(tab);
}
