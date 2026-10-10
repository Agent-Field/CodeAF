import { useEffect, useRef, type Dispatch } from 'react';
import { newTab } from '../tabs/helpers';
import { focusedPane, tabHolding, type WorkspaceAction, type WorkspaceState } from '../tabs/model';
import { bindingFor } from './target';
import { closePaneEvent, openConversationEvent, openTerminalEvent, type ClosePaneDetail, type OpenConversationDetail, type OpenTerminalDetail } from './events';
import { shortcutLayer } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import { openTerminalTab } from './open';

/** The conversation the active pane belongs to: the terminal opens under it (16 live terminals per conversation, Q6). */
export function activeSessionFile(state: WorkspaceState): string | undefined {
  const tab = state.tabs.find(t => t.id === state.activeId);
  const pane = tab && focusedPane(tab);
  return pane && (bindingFor(pane)?.sessionFile ?? pane.sessionFile);
}

/**
 * The workspace's side of the terminal kind, mounted once: the "New terminal" key, and the two things a
 * terminal pane cannot do itself because it cannot reach the tab strip (open the conversation that "Ask codeaf
 * about this output" started, and close its own tab after "Remove").
 */
export function useTerminalTabs({ enabled, state, dispatch }: { enabled: boolean; state: WorkspaceState; dispatch: Dispatch<WorkspaceAction> }) {
  const live = useRef(state);
  live.current = state;
  useShortcuts(shortcutLayer.workspace, shortcut => {
    if (shortcut.id !== 'terminal-new' || document.querySelector('dialog[open]')) return false;
    void openTerminalTab({ sessionFile: activeSessionFile(live.current) }).then(tab => dispatch({ type: 'open', tab, background: false }));
    return true;
  }, enabled);
  useEffect(() => {
    const onConversation = (event: Event) => {
      const { sessionFile, title } = (event as CustomEvent<OpenConversationDetail>).detail;
      dispatch({ type: 'open', tab: newTab({ kind: 'conversation', title, titleSource: 'message', sessionFile }), background: false });
    };
    const onTerminal = (event: Event) => {
      const { sessionFile, command, title } = (event as CustomEvent<OpenTerminalDetail>).detail;
      void openTerminalTab({ sessionFile, command, title }).then(tab => dispatch({ type: 'open', tab, background: false }));
    };
    const onClose = (event: Event) => {
      const { paneId } = (event as CustomEvent<ClosePaneDetail>).detail;
      const holder = tabHolding(live.current, paneId);
      if (!holder) return;
      dispatch(holder.split ? { type: 'split-close-pane', id: holder.id, paneId } : { type: 'close', id: holder.id });
    };
    window.addEventListener(openConversationEvent, onConversation);
    window.addEventListener(openTerminalEvent, onTerminal);
    window.addEventListener(closePaneEvent, onClose);
    return () => { window.removeEventListener(openConversationEvent, onConversation); window.removeEventListener(openTerminalEvent, onTerminal); window.removeEventListener(closePaneEvent, onClose); };
  }, [enabled, dispatch]);
}
