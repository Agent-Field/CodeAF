// Shortcut labels the menus and tooltips of the closing lane show (design 3g and 3l spell them without a space on macOS).
import { isMac } from '../../../design/keyboard';

export const closeShortcut = isMac ? '⌘W' : 'Ctrl W';
/** Close and stop. The design also draws ⌥⌘W on "Close other tabs"; one key cannot do both, so it belongs to stopping. */
export const closeStopShortcut = isMac ? '⌥⌘W' : 'Ctrl Alt W';
export const copyLinkShortcut = isMac ? '⌘⇧C' : 'Ctrl Shift C';
export const newGroupShortcut = isMac ? '⌘G' : 'Ctrl G';

/** Option+Command+W on macOS, Ctrl+Alt+W elsewhere. `code` because Option+W types a symbol on a Mac. */
export function isCloseStopKey(event: Pick<KeyboardEvent, 'code' | 'metaKey' | 'ctrlKey' | 'shiftKey' | 'altKey'>): boolean {
  const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  return primary && event.altKey && !event.shiftKey && event.code === 'KeyW';
}
