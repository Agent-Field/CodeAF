import { ConversationPane } from './ConversationPane';
import { SessionPreview } from '../preview/bodies';
import type { KindDef } from './slots';

/** Live. A task opened from a conversation is the conversation components drawn from the task page (route.taskId). */
export const taskKind: KindDef = { kind: 'task', label: 'Task', icon: 'checklist', backed: true, pane: ConversationPane, preview: SessionPreview };
