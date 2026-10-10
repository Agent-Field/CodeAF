// ⌘N and File ▸ New Window open a window on Now (IX Flows "Multiple windows").
// The shortcut registry id is `new-window`. The native menu delivers that same id
// (`desktop-tab-action` → runShortcut), so one handler is both the key and the menu event.
// In a plain browser the chord is not taken: the capability is absent, and the browser keeps New Window.
// Inside Go to, the sheet's own key handler creates a place (PLD-02); this layer must not cancel that key.
import { useEffect, useRef } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { registerShortcuts, shortcutLayer } from '../../design/keyboard.ts';
import { toasts } from '../../design/toasts.ts';
import { createNativeWindows } from '../../lib/native/windows.ts';

export type NewWindowRequest = { placeKey: 'now' };

/** Opens a window on Now. Outside the desktop app this does nothing: there is no window to open. */
export function windowOpen(request: NewWindowRequest): Promise<string | undefined> {
  if (!isTauri() || request.placeKey !== 'now') return Promise.resolve(undefined);
  return createNativeWindows().openPlaceWindow(request.placeKey);
}

function reportOpenFailure(failure: unknown) {
  const sentence = failure instanceof Error && failure.message ? failure.message : 'Could not open a window';
  toasts.show({ message: [sentence], tone: 'warning' });
}

/** True when the key landed in Go to, which uses ⌘N to create a place rather than a window. */
export function goToHasTheKey(target: EventTarget | null): boolean {
  const element = target as { closest?: (selector: string) => unknown } | null;
  return !!element?.closest?.('.goto-chooser');
}

/**
 * What the `new-window` registry id does. Returns false when the chord must keep travelling:
 * a browser, or Go to. `open` defaults to `windowOpen`, which asks the shell for Now.
 */
export function takeNewWindow(
  shortcut: { id: string },
  desktop: boolean,
  event: { target?: EventTarget | null } | undefined,
  open: (request: NewWindowRequest) => void | Promise<unknown> = windowOpen,
): boolean {
  if (shortcut.id !== 'new-window' || !desktop || goToHasTheKey(event?.target ?? null)) return false;
  void Promise.resolve(open({ placeKey: 'now' })).catch(reportOpenFailure);
  return true;
}

/** Registers the New Window chord and the menu event that arrives as the same registry id. */
export function useWindowKeys() {
  // The handler stays current without re-registering. The desktop flag is read when the chord arrives,
  // so a stub installed with the page is seen. useShortcuts imports the registry without a .ts suffix,
  // which the node test loader cannot follow, so this hook registers directly.
  const latest = useRef(takeNewWindow);
  latest.current = takeNewWindow;
  useEffect(() => registerShortcuts(shortcutLayer.app, (shortcut, event) => latest.current(shortcut, isTauri(), event)), []);
}
