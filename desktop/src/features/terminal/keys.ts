
type KeyEvent = Pick<KeyboardEvent, 'key' | 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>;

export { newTerminalShortcut, isNewTerminalShortcut } from '../../design/keyboard';

/** Control+Tab (and with Shift) is the workspace's recent-tab switcher; a terminal must let it through. */
export const isSwitchShortcut = (event: KeyEvent) => event.ctrlKey && !event.metaKey && !event.altKey && event.key === 'Tab';
