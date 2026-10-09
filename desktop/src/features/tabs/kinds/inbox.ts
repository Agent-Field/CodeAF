import { InboxPane } from '../../inbox/InboxPane';
import { PlainPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/** The Inbox (Places 6a rail, Shell 3j): what needs the person and what runs in the background, from the engine-wide world stream. */
export const inboxKind: KindDef = { kind: 'inbox', label: 'Inbox', icon: 'inbox', backed: true, pane: InboxPane, preview: PlainPreview };
