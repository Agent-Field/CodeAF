import { isMac } from '../../design/keyboard';

type KeyEvent = Pick<KeyboardEvent, 'key' | 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>;

/** Control + backtick on every platform, as in the ⌘T field's "New terminal ⌃`" row. */
export const newTerminalShortcut = isMac ? '⌃`' : 'Ctrl `';

export const isNewTerminalShortcut = (event: KeyEvent) =>
  event.ctrlKey && !event.metaKey && !event.altKey && !event.shiftKey && event.code === 'Backquote';

/** Control+Tab (and with Shift) is the workspace's recent-tab switcher; a terminal must let it through. */
export const isSwitchShortcut = (event: KeyEvent) => event.ctrlKey && !event.metaKey && !event.altKey && event.key === 'Tab';
