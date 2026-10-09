// Option+Command+W (Ctrl+Alt+W elsewhere): close the active tab and stop its work. The plain close key lives in useTabKeys.
import { useEffect } from 'react';
import { isCloseStopKey } from './shortcuts';

export function useCloseStopKey(enabled: boolean, onCloseAndStop: () => void) {
  useEffect(() => {
    if (!enabled) return;
    const onKey = (event: KeyboardEvent) => {
      if (!isCloseStopKey(event) || document.querySelector('dialog[open]')) return;
      event.preventDefault();
      onCloseAndStop();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [enabled, onCloseAndStop]);
}
