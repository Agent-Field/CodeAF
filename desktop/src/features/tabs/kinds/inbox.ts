import { placeholderPane } from './Placeholder';
import { PlainPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/** Placeholder. The pinned Inbox (3j) needs an engine-wide feed of work that needs you; not opened in the live app until that exists. */
export const inboxKind: KindDef = { kind: 'inbox', label: 'Inbox', icon: 'inbox', backed: false, pane: placeholderPane('Inbox', 'inbox'), preview: PlainPreview };
