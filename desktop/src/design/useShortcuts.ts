import { useEffect, useRef } from 'react';
import { registerShortcuts, type ShortcutHandler } from './keyboard';

/** Registers a handler on the shell's one shortcut registry for as long as the component is mounted and `active`. */
export function useShortcuts(layer: number, handler: ShortcutHandler, active = true) {
  const latest = useRef(handler);
  latest.current = handler;
  useEffect(() => (active ? registerShortcuts(layer, (shortcut, event) => latest.current(shortcut, event)) : undefined), [layer, active]);
}
