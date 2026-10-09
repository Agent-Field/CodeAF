// How a terminal pane asks the workspace to do something to the tab strip. A pane cannot open or close
// tabs (its slot only receives its own state), so it announces on the window and `useTerminalTabs`,
// mounted once by the workspace, acts. Same seam as the native menu events in lib/desktopTabs.

export const openConversationEvent = 'codeaf:terminal-open-conversation';
export const openTerminalEvent = 'codeaf:terminal-open-terminal';
export const closePaneEvent = 'codeaf:terminal-close-pane';

export type OpenConversationDetail = { sessionFile: string; title: string };
export type OpenTerminalDetail = { sessionFile?: string; command: string; title: string };
export type ClosePaneDetail = { paneId: string };

const announce = <T,>(name: string, detail: T) => window.dispatchEvent(new CustomEvent<T>(name, { detail }));

/** "Ask codeaf about this output" answered: show the new conversation in a tab of its own. */
export const announceOpenConversation = (detail: OpenConversationDetail) => announce(openConversationEvent, detail);
/** "Remove job" done: the tab has nothing left to show. */
export const announceClosePane = (detail: ClosePaneDetail) => announce(closePaneEvent, detail);
/** "Run again": a finished job's command, started once more in a tab of its own. */
export const announceOpenTerminal = (detail: OpenTerminalDetail) => announce(openTerminalEvent, detail);
