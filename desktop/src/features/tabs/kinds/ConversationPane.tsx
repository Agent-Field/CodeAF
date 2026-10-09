import { ConversationView } from '../../conversation/ConversationView';
import type { PaneRenderProps } from './slots';

/** The live renderer shared by conversation, task and new-tab panes: a conversation view bound to one pane's state. */
export function ConversationPane({ pane, label, focused, split, actions }: PaneRenderProps) {
  return <ConversationView key={pane.id} tab={pane} label={label} onDraft={actions.onDraft} onView={actions.onView} onSummary={actions.onSummary} onOpenTaskTab={actions.onOpenTaskTab} usingApi={actions.usingApi} onOpenSource={actions.onOpenSource} onAddToPlace={actions.onAddToPlace} autoFocus={focused} split={split} focused={focused}/>;
}
