import { placeholderPane } from './Placeholder';
import type { KindDef } from './slots';

/** Placeholder. The history lane builds the tab (4a-4e); recaps need an engine recap source that does not exist yet. */
export const historyKind: KindDef = { kind: 'history', label: 'History', icon: 'history', backed: false, pane: placeholderPane('History', 'history'), preview: null };
