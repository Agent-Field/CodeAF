import { ConversationPane } from './ConversationPane';
import type { KindDef } from './slots';

/** Live. Backed by the canonical engine session. */
export const conversationKind: KindDef = { kind: 'conversation', label: 'Conversation', icon: 'tab', backed: true, pane: ConversationPane, preview: null };
