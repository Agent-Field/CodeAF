// The small shared state between the rail (App) and the workspace: which kind of tab is on screen, and the
// request to open a kind. The workspace owns its tabs; the rail only needs to light the item that is open.
import { useSyncExternalStore } from 'react';
import type { TabKind } from '../tabs/kinds/types';
import type { ShellKind } from './openKind';

export const shellEvent = 'codeaf:shell-open';

/** Ask the workspace to open (or focus) the tab of this kind. */
export function requestOpenKind(kind: ShellKind) {
  window.dispatchEvent(new CustomEvent<ShellKind>(shellEvent, { detail: kind }));
}

export const shellLeaveEvent = 'codeaf:shell-leave';
/** Ask the workspace to move off the tab of this kind (the rail's Workspace item while Settings is showing). */
export function requestLeaveKind(kind: ShellKind) {
  window.dispatchEvent(new CustomEvent<ShellKind>(shellLeaveEvent, { detail: kind }));
}

let activeHome: string | undefined;
const homeListeners = new Set<() => void>();
/** The place of the focused Home tab (`root` for All places), or undefined when the focused tab is not a Home. */
export function publishActiveHome(place: string | undefined) {
  if (place === activeHome) return;
  activeHome = place;
  homeListeners.forEach(listener => listener());
}
export function useActiveHome(): string | undefined {
  return useSyncExternalStore(listener => { homeListeners.add(listener); return () => { homeListeners.delete(listener); }; }, () => activeHome);
}

let activeKind: TabKind = 'conversation';
const listeners = new Set<() => void>();
export function publishActiveKind(kind: TabKind) {
  if (kind === activeKind) return;
  activeKind = kind;
  listeners.forEach(listener => listener());
}
/** The kind of the focused pane of the active tab. */
export function useActiveKind(): TabKind {
  return useSyncExternalStore(listener => { listeners.add(listener); return () => { listeners.delete(listener); }; }, () => activeKind);
}
