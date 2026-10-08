import { isTauri } from '@tauri-apps/api/core';
import { listen, type UnlistenFn } from '@tauri-apps/api/event';

export const desktopTabEvent = 'codeaf:desktop-tab-action';
const actions = ['new', 'close', 'reopen', 'overview', 'next', 'previous'] as const;
export type DesktopTabAction = typeof actions[number];
export function isDesktopTabAction(value: unknown): value is DesktopTabAction {
 return typeof value === 'string' && actions.some(action => action === value);
}
/** Native menu accelerators select UI views; they never call the session engine. */
export function connectDesktopTabs() {
 if (!isTauri()) return () => {};
 let disposed = false;
 let disconnect: UnlistenFn | undefined;
 void listen<unknown>('desktop-tab-action', event => {
  if (!disposed && isDesktopTabAction(event.payload)) window.dispatchEvent(new CustomEvent(desktopTabEvent, { detail: event.payload }));
 }).then(unlisten => { if (disposed) unlisten(); else disconnect = unlisten; }).catch(error => console.warn('Native tab menu could not connect', error));
 return () => { disposed = true; disconnect?.(); };
}
