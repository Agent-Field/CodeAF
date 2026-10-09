// Scroll behaviour for turns (design v3 conversation 1f): ⌘↑/⌘↓ step between
// turns with a fixed top offset, Esc returns to the latest message, and
// folding or unfolding keeps the touched row where it was on screen.

import { useCallback, useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import { shortcutLayer } from '../../design/keyboard';
import { useShortcuts } from '../../design/useShortcuts';
import { JUMP_OFFSET, jumpIndex } from './folding';

const behavior = (): ScrollBehavior => (window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth');

const topOf = (pane: HTMLElement, element: Element) => element.getBoundingClientRect().top - pane.getBoundingClientRect().top;

const anchorOf = (pane: HTMLElement | null, id: string) => pane?.querySelector(`[data-anchor="${CSS.escape(id)}"]`) ?? null;

/** 'turn' moved to a turn, 'edge' went to the top or bottom of the pane. */
function step(pane: HTMLElement, direction: 1 | -1): 'turn' | 'edge' {
  const turns = [...pane.querySelectorAll('[data-turn]')];
  const index = jumpIndex(turns.map((turn) => topOf(pane, turn)), direction);
  const edge = direction === 1 ? pane.scrollHeight : 0;
  const top = index === -1 ? edge : pane.scrollTop + topOf(pane, turns[index]) - JUMP_OFFSET;
  pane.scrollTo({ top, behavior: behavior() });
  return index === -1 ? 'edge' : 'turn';
}

/** ⌘↑ and ⌘↓ (through the shell's shortcut registry) and Esc. */
export function useTurnJump(scroller: RefObject<HTMLElement | null>, active: boolean) {
  const away = useRef(false);
  useShortcuts(shortcutLayer.surface, shortcut => {
    const pane = scroller.current;
    if (!pane || (shortcut.id !== 'turn-previous' && shortcut.id !== 'turn-next')) return false;
    away.current = step(pane, shortcut.id === 'turn-next' ? 1 : -1) === 'turn';
    return true;
  }, active);
  useEffect(() => {
    if (!active) return;
    const onKey = (event: KeyboardEvent) => {
      const pane = scroller.current;
      if (pane && event.key === 'Escape' && away.current && !event.defaultPrevented) {
        away.current = false;
        pane.scrollTo({ top: pane.scrollHeight, behavior: behavior() });
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [scroller, active]);
}

/**
 * Folding changes the height of everything below a row. Hold the row before the
 * change; once the new state has rendered, move the pane by how far the row moved.
 */
export function useFoldAnchor(scroller: RefObject<HTMLElement | null>, signature: unknown) {
  const held = useRef<{ id: string; top: number } | null>(null);
  const hold = useCallback(
    (id: string) => {
      const pane = scroller.current;
      const row = anchorOf(pane, id);
      held.current = pane && row ? { id, top: topOf(pane, row) } : null;
    },
    [scroller],
  );
  useLayoutEffect(() => {
    const pane = scroller.current;
    const target = held.current;
    held.current = null;
    const row = target ? anchorOf(pane, target.id) : null;
    if (pane && target && row) pane.scrollTop += topOf(pane, row) - target.top;
  }, [scroller, signature]);
  return hold;
}
