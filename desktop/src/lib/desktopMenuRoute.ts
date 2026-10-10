// The pure half of the native menu bridge: which payloads are tab commands and which are shell chords.
// The event name lives here so a node test can dispatch it without loading the Tauri bridge.
import type { Shortcut } from '../design/keyboard';

/** The window event the native menu and an in-page tab open share. The workspace listens once. */
export const desktopTabEvent = 'codeaf:desktop-tab-action';

const actions = ['new', 'close', 'close-stop', 'reopen', 'overview', 'next', 'previous'] as const;
export type DesktopTabAction = typeof actions[number];
export function isDesktopTabAction(value: unknown): value is DesktopTabAction {
 return typeof value === 'string' && actions.some(action => action === value);
}
/** Menu commands that are shell chords: they run the handler the same keypress reaches, so menu and key cannot drift. */
const shellShortcuts = { settings: 'settings', focus: 'focus', sidebar: 'rail', history: 'history', 'new-window': 'new-window' } as const;
type DesktopShellAction = keyof typeof shellShortcuts;
export function shellShortcutOf(value: unknown): Shortcut | undefined {
 return typeof value === 'string' && Object.prototype.hasOwnProperty.call(shellShortcuts, value) ? { id: shellShortcuts[value as DesktopShellAction] } : undefined;
}
/** Routes one native menu payload: shell chords through the shortcut registry, tab actions to the workspace. */
export function deliverDesktopAction(payload: unknown, toWorkspace: (action: DesktopTabAction) => void, run: (shortcut: Shortcut) => boolean): boolean {
 const shortcut = shellShortcutOf(payload);
 if (shortcut) return run(shortcut);
 if (!isDesktopTabAction(payload)) return false;
 toWorkspace(payload);
 return true;
}
