import { isTauri } from '@tauri-apps/api/core';
import { runShortcut } from '../design/keyboard';
import { deliverDesktopAction } from './desktopMenuRoute';
export { isDesktopTabAction, type DesktopTabAction } from './desktopMenuRoute';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';

export const desktopTabEvent = 'codeaf:desktop-tab-action';
const toWorkspace = (action: string) => window.dispatchEvent(new CustomEvent(desktopTabEvent, { detail: action }));
/** Native menu accelerators select UI views; they never call the session engine. */
export function connectDesktopTabs() {
 if (!isTauri()) return () => {};
 let disposed = false;
 let disconnect: UnlistenFn | undefined;
 void listen<unknown>('desktop-tab-action', event => {
  if (!disposed) deliverDesktopAction(event.payload, toWorkspace, runShortcut);
 }).then(unlisten => { if (disposed) unlisten(); else disconnect = unlisten; }).catch(error => console.warn('Native tab menu could not connect', error));
 return () => { disposed = true; disconnect?.(); };
}
