// The window frame's own state (design Shell 2h "Rail", Places 9e): the rail collapsed by ⌘S, Focus mode (⌘⇧F)
// that hides the rail and the strip, and the peeks that bring them back. Resting on the left 8px edge for 300ms
// peeks the full rail (Places 9e); in Focus mode the top 8px of the window brings the strip back (Shell 2h).
import { useCallback, useEffect, useRef, useState } from 'react';
import design from '../../design/tokens.json';

export type Peek = 'rail' | 'strip';

/** While the pointer is over one of these, the peek that revealed it stays on screen. */
const peekSurfaces: Record<Peek, string> = { rail: '.app-shell > .sidebar', strip: '.workspace-tabbar, .content-toolbar' };
// The delay is a rest on the edge, not the slide. Reduced motion already zeroes the overlay's
// transition through the duration tokens; shortening the rest would open the rail on a pass across it.
const peekDelay = parseFloat(design.foundation['rail-peek-delay']);

const covers = (event: PointerEvent, surface: Element, which: Peek) => {
  // The rail slides in, so its painted box lags the rectangle it rests in. A pointer already
  // in that rectangle has not left the overlay (Places 9e, R4).
  if (which === 'rail') {
    const width = (surface as HTMLElement).offsetWidth;
    return event.clientX >= 0 && event.clientX <= width && event.clientY >= 0 && event.clientY <= window.innerHeight;
  }
  const box = surface.getBoundingClientRect();
  return event.clientX >= box.left && event.clientX <= box.right && event.clientY >= box.top && event.clientY <= box.bottom;
};

const overAny = (event: PointerEvent, which: Peek) => [...document.querySelectorAll(peekSurfaces[which])].some(surface => covers(event, surface, which));

export function useShellFrame(narrow: boolean) {
  const [collapsed, setCollapsed] = useState(false);
  const [focus, setFocus] = useState(false);
  const [peek, setPeek] = useState<Peek | null>(null);
  const dwell = useRef<number | undefined>(undefined);
  const cancelDwell = useCallback(() => { window.clearTimeout(dwell.current); dwell.current = undefined; }, []);
  // The rail key from Focus mode brings the rail back for good: the person asked for it by name.
  const toggleRail = useCallback(() => {
    cancelDwell(); setPeek(null);
    if (focus) { setFocus(false); setCollapsed(false); } else setCollapsed(value => !value);
  }, [focus, cancelDwell]);
  const toggleFocus = useCallback(() => { cancelDwell(); setPeek(null); setFocus(value => !value); }, [cancelDwell]);
  const railPeekable = !narrow && (focus || collapsed);
  useEffect(() => {
    if (!peek) return;
    const onMove = (event: PointerEvent) => {
      // A menu opened from the revealed strip keeps it until the menu closes.
      if (peek === 'strip' && document.querySelector('.workspace-tabbar [aria-expanded="true"]')) return;
      if (!overAny(event, peek)) setPeek(null);
    };
    const onLeave = () => setPeek(null);
    window.addEventListener('pointermove', onMove);
    document.documentElement.addEventListener('pointerleave', onLeave);
    return () => { window.removeEventListener('pointermove', onMove); document.documentElement.removeEventListener('pointerleave', onLeave); };
  }, [peek]);
  useEffect(() => cancelDwell, [cancelDwell]);
  return {
    collapsed, focus,
    peek: peek === 'rail' && !railPeekable ? null : peek === 'strip' && !focus ? null : peek,
    /** The rail is off the layout: collapsed, Focus mode, or narrow (where it is a drawer). */
    railHidden: narrow || collapsed || focus,
    /** Which window edges are listening: the left edge while the rail is put away, the top while Focus mode hides the strip. */
    edges: { left: railPeekable && peek !== 'rail', top: focus && peek !== 'strip' },
    toggleRail, toggleFocus,
    /** The pointer reached the left edge: the rail comes out once it has rested there for the design's 300ms. */
    enterLeftEdge: () => { cancelDwell(); dwell.current = window.setTimeout(() => setPeek('rail'), peekDelay); },
    leaveLeftEdge: cancelDwell,
    enterTopEdge: () => setPeek('strip'),
  };
}
