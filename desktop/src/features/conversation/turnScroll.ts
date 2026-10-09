// Scroll behaviour for turns (design v3 conversation 1f): ⌘↑/⌘↓ step between
// turns with a fixed top offset, Esc returns to the latest message, and
// folding or unfolding keeps the touched row where it was on screen.

import { useCallback, useEffect, useLayoutEffect, useRef, type RefObject } from 'react';
import { isMac } from '../../design/keyboard';
import { JUMP_OFFSET, jumpIndex } from './folding';

const behavior = (): ScrollBehavior => (window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth');

function stepDirection(event: KeyboardEvent): 1 | -1 | 0 {
  const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
  if (!primary || event.altKey || event.shiftKey) return 0;
  return event.key === 'ArrowUp' ? -1 : event.key === 'ArrowDown' ? 1 : 0;
}

/** A field with words in it keeps its own caret keys. */
function isWritingField(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLTextAreaElement || target instanceof HTMLInputElement)) return false;
  return target.value.length > 0;
}

const topOf = (pane: HTMLElement, element: Element) => element.getBoundingClientRect().top - pane.getBoundingClientRect().top;

const anchorOf = (pane: HTMLElement | null, id: string) => pane?.querySelector(`[data-anchor="${CSS.escape(id)}"]`) ?? null;

function step(pane: HTMLElement, direction: 1 | -1): boolean {
  const turns = [...pane.querySelectorAll('[data-turn]')];
  const index = jumpIndex(turns.map((turn) => topOf(pane, turn)), direction);
  const edge = direction === 1 ? pane.scrollHeight : 0;
  const top = index === -1 ? edge : pane.scrollTop + topOf(pane, turns[index]) - JUMP_OFFSET;
  pane.scrollTo({ top, behavior: behavior() });
  return index !== -1;
}

/** The window listener behind ⌘↑, ⌘↓ and Esc. */
export function useTurnJump(scroller: RefObject<HTMLElement | null>, active: boolean) {
  const away = useRef(false);
  useEffect(() => {
    if (!active) return;
    const onKey = (event: KeyboardEvent) => {
      const pane = scroller.current;
      if (!pane) return;
      const direction = stepDirection(event);
      if (direction && !isWritingField(event.target)) {
        event.preventDefault();
        away.current = step(pane, direction);
      } else if (event.key === 'Escape' && away.current && !event.defaultPrevented) {
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
