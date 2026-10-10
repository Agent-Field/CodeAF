import { useSyncExternalStore } from 'react';
import { nextUpWalk } from '../../nextup/useNextUpWalk';
import { chatIdFromSessionFile } from '../../places/client';
import { ConversationView } from '../../conversation/ConversationView';
import type { PaneRenderProps } from './slots';

/** The live renderer shared by conversation, task and new-tab panes: a conversation view bound to one pane's state. */
export function ConversationPane({ pane, label, focused, split, actions }: PaneRenderProps) {
  const walk = useSyncExternalStore(nextUpWalk.subscribe, nextUpWalk.getSnapshot);
  const item = walk.item;
  const nextUp = focused && item?.session === (pane.sessionFile ? chatIdFromSessionFile(pane.sessionFile) : '') ? walk.progress : undefined;
  return <ConversationView nextUp={nextUp} key={pane.id} tab={pane} label={label} onDraft={actions.onDraft} onView={actions.onView} onSummary={actions.onSummary} onOpenTaskTab={actions.onOpenTaskTab} onRename={actions.onRename} onOpenConversationTab={actions.onOpenConversationTab} usingApi={actions.usingApi} onOpenSource={actions.onOpenSource} onAddToPlace={actions.onAddToPlace} autoFocus={focused} split={split} focused={focused}/>;
}
