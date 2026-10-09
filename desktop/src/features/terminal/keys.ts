
type KeyEvent = Pick<KeyboardEvent, 'key' | 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>;

import { shortcutOf } from '../../design/keyboard';
export { newTerminalShortcut, isNewTerminalShortcut } from '../../design/keyboard';

/** Control+Tab (and with Shift) is the workspace's recent-tab switcher; a terminal must let it through. */
export const isSwitchShortcut = (event: KeyEvent) => event.ctrlKey && !event.metaKey && !event.altKey && event.key === 'Tab';

/** True for the chords the workspace keeps inside a terminal field on this platform; everything else is the shell's. */
export const isTerminalShortcut = (event: KeyEvent) => shortcutOf(event, { terminal: true }) !== undefined;
