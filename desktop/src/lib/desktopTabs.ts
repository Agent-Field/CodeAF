import { isTauri } from '@tauri-apps/api/core';
import { runShortcut } from '../design/keyboard';
import { deliverDesktopAction, desktopTabEvent, shellShortcutOf } from './desktopMenuRoute';
export { desktopTabEvent, isDesktopTabAction, type DesktopTabAction } from './desktopMenuRoute';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';
const toWorkspace = (action: string) => window.dispatchEvent(new CustomEvent(desktopTabEvent, { detail: action }));
/** Native menu accelerators select UI views; they never call the session engine. */
export function connectDesktopTabs() {
 // The same event carries shell chords in a browser (the native listener below runs them directly), so a page can be driven exactly as the menu drives it.
 const onEvent = (event: Event) => { const shortcut = shellShortcutOf((event as CustomEvent).detail); if (shortcut) runShortcut(shortcut); };
 window.addEventListener(desktopTabEvent, onEvent);
 const stopEvent = () => window.removeEventListener(desktopTabEvent, onEvent);
 if (!isTauri()) return stopEvent;
 let disposed = false;
 let disconnect: UnlistenFn | undefined;
 void listen<unknown>('desktop-tab-action', event => {
  if (!disposed) deliverDesktopAction(event.payload, toWorkspace, runShortcut);
 }).then(unlisten => { if (disposed) unlisten(); else disconnect = unlisten; }).catch(error => console.warn('Native tab menu could not connect', error));
 return () => { disposed = true; disconnect?.(); stopEvent(); };
}
