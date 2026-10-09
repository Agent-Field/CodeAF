import { placeholderPane } from './Placeholder';
import type { KindDef } from './slots';

/** Placeholder. No engine diff bridge backs a changes view yet. */
export const diffKind: KindDef = { kind: 'diff', label: 'Diff', icon: 'diff', backed: false, pane: placeholderPane('Diff', 'diff'), preview: null };
