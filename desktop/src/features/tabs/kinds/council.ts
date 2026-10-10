import { createElement, useState } from 'react';
import { CouncilPane } from '../../council/CouncilPane';
import { useCouncilOf } from '../../council/useCouncilChat';
import { ConversationPane } from './ConversationPane';
import type { PaneRenderProps } from './slots';

/**
 * A council is an ordinary conversation tab (Decisions 12): opening it from Live, Recent or History makes a plain
 * conversation tab on its saved session, so no new tab kind exists. This pane is the one place that learns the session
 * belongs to a discussion and draws it with place-labelled lines; every other session falls through unchanged. The registry swaps it into the conversation kind; it is a function declaration
 * so it is usable even while the import cycle through the registry is still loading. Until the
 * list has answered it draws nothing rather than attaching an engine session that a council does not have.
 */
export function ConversationOrCouncilPane(props: PaneRenderProps) {
  // A tab that began with no saved session is a conversation being started: it gains a file on its first send, and must
  // not be unmounted while the list is asked about that new file.
  const [started] = useState(() => !props.pane.sessionFile);
  const { council, settled } = useCouncilOf(started ? undefined : props.pane.sessionFile);
  if (!settled) return null;
  return council ? createElement(CouncilPane, { ...props, initial: council }) : createElement(ConversationPane, props);
}
