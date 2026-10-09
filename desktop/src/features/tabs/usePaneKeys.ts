// Split keys (design 2h): ⌘⌥← and ⌘⌥→ (Ctrl+Alt on other platforms) move focus between the panes of a split.
import { useEffect, type Dispatch, type RefObject } from 'react';
import { isMac } from '../../design/keyboard';
import type { Tab, WorkspaceAction } from './model';

export const paneFocusShortcuts = { previous: isMac ? '⌘⌥←' : 'Ctrl Alt ←', next: isMac ? '⌘⌥→' : 'Ctrl Alt →' };

export function usePaneKeys(tab: Tab, dispatch: Dispatch<WorkspaceAction>, grid: RefObject<HTMLElement | null>) {
  const split = tab.split;
  useEffect(() => {
    if (!split) return;
    const onKey = (event: KeyboardEvent) => {
      const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
      if (!primary || !event.altKey || event.shiftKey || (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight')) return;
      if (document.querySelector('dialog[open]')) return;
      event.preventDefault();
      const index = Math.min(Math.max(split.focus + (event.key === 'ArrowRight' ? 1 : -1), 0), split.panes.length - 1);
      if (index === split.focus) return;
      dispatch({ type: 'split-focus', id: tab.id, index });
      // A pane that holds no field of its own still takes the keyboard: focus its card once it has rendered.
      requestAnimationFrame(() => {
        const pane = grid.current?.querySelectorAll<HTMLElement>('.workspace-pane')[index];
        if (pane && !pane.contains(document.activeElement)) pane.focus({ preventScroll: true });
      });
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [tab.id, split?.focus, split?.panes.length]);
}
