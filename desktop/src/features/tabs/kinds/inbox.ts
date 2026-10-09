import { PlainPreview } from '../preview/bodies';
import { InboxPane } from './inbox/InboxPane';
import type { KindDef } from './slots';

/**
 * The pinned Inbox (3j, 3l), also the Places rail's Inbox row (6a). Backed by the workspace's real session reads and the
 * engine-wide world stream: closed tabs whose work goes on, conversations elsewhere that wait on the person, and recent
 * failures. The tab strip carries its dot only while something needs you.
 */
export const inboxKind: KindDef = { kind: 'inbox', label: 'Inbox', icon: 'inbox', backed: true, pane: InboxPane, preview: PlainPreview };
