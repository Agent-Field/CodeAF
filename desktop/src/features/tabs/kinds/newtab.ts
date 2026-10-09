import { ConversationPane } from './ConversationPane';
import { PlainPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/**
 * Live stand-in. The new-tab lane replaces `pane` with the command field (3k "New tab is a field").
 * Until then `new` still opens a conversation tab, so this kind is registered but not yet opened.
 */
export const newtabKind: KindDef = { kind: 'newtab', label: 'New tab', icon: 'plus', backed: true, pane: ConversationPane, preview: PlainPreview };
