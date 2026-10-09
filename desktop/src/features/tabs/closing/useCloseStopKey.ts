// Option+Command+W (Ctrl+Alt+W elsewhere): close the active tab and stop its work. The plain close key lives in useTabKeys.
import { useEffect } from 'react';
import { isMac, isTerminalTarget } from '../../../design/keyboard';
import { isCloseStopKey } from './shortcuts';

export function useCloseStopKey(enabled: boolean, onCloseAndStop: () => void) {
  useEffect(() => {
    if (!enabled) return;
    const onKey = (event: KeyboardEvent) => {
      if (!isCloseStopKey(event) || document.querySelector('dialog[open]')) return;
      // Off a Mac, Ctrl chords typed into a terminal belong to its shell (design/keyboard.ts); only Ctrl+Shift ones are ours.
      if (!isMac && isTerminalTarget(event.target)) return;
      event.preventDefault();
      onCloseAndStop();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [enabled, onCloseAndStop]);
}
