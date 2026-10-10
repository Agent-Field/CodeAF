import { useEffect, useRef } from 'react';
import { shortcutLayer, type Shortcut } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import { desktopTabEvent, isDesktopWebAction } from '../../lib/desktopMenuRoute';
import { focusAddress } from './addressFocus';
import { step } from './views';

/**
 * ⌘L, ⌘R and ⌘F for the focused web pane. While the page holds focus its keystrokes never reach the
 * renderer, so the native menu accelerators send the same ids through the tab event; while focus is in
 * the DOM the central keyboard registry sends them. Both land in one handler, so the two paths cannot drift.
 * Only the focused pane answers: in a split the other page must not reload or take the address.
 */
export function useWebKeys(pane: string, focused: boolean, onFind: () => void) {
  const latest = useRef({ pane, focused, onFind });
  latest.current = { pane, focused, onFind };

  const run = (id: string): boolean => {
    const { pane, focused, onFind } = latest.current;
    if (!focused || !isDesktopWebAction(id)) return false;
    if (id === 'web-address') focusAddress(pane);
    else if (id === 'web-reload') step(pane, 'reload');
    else onFind();
    return true;
  };

  useShortcuts(shortcutLayer.surface, (shortcut: Shortcut) => run(shortcut.id));
  useEffect(() => {
    const onAction = (event: Event) => { run((event as CustomEvent).detail); };
    window.addEventListener(desktopTabEvent, onAction);
    return () => window.removeEventListener(desktopTabEvent, onAction);
  }, []);
}
