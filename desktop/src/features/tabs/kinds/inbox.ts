import { PlainPreview } from '../preview/bodies';
import { InboxPane } from './inbox/InboxPane';
import type { KindDef } from './slots';

/**
 * The pinned Inbox (3j, 3l). Backed by the workspace's real session reads: closed tabs whose work goes on, and open
 * tabs that wait on the person. The tab strip carries its dot only while something needs you.
 */
export const inboxKind: KindDef = { kind: 'inbox', label: 'Inbox', icon: 'inbox', backed: true, pane: InboxPane, preview: PlainPreview };
