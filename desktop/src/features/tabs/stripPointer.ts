// Pointer rules for a strip tab that the design leaves to a choice (SH-OQ4).
// Middle-click is the same close as the tab's ×: the view goes, the work keeps running.
import type { Tab } from './types.ts';

/**
 * Whether a middle-click closes this strip tab. A pinned tab has no × and ⌘W leaves it open, so it does not
 * go. Every other tab does, including a split, which is one merged tab.
 */
export function stripMiddleCloses(tab: Pick<Tab, 'pinned'>): boolean {
  return !tab.pinned;
}
