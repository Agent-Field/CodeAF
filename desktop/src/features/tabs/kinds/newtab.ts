import { PlainPreview } from '../preview/bodies';
import { NewTabPane } from './newtab/NewTabPane';
import type { KindDef } from './slots';

/**
 * The new tab (design 3f): an empty card with one field. It is a pane like any other kind; the workspace hands
 * it the tab list through NewTabHostContext, and it turns its own tab into a conversation, a file or a reopened
 * tab with the `newtab-*` actions (reducers/newtab.ts).
 */
export const newtabKind: KindDef = { kind: 'newtab', label: 'New tab', icon: 'plus', backed: true, pane: NewTabPane, preview: PlainPreview };
