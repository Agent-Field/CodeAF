import { useEffect, useState, type RefObject } from 'react';

export type ScrollEdges = { above: boolean; below: boolean };

const NONE: ScrollEdges = { above: false, below: false };

/** Which sides of a scroller have more to show. The list fades only on a side that does. */
export function useScrollEdges(scroller: RefObject<HTMLElement | null>): ScrollEdges {
  const [edges, setEdges] = useState(NONE);
  useEffect(() => {
    const el = scroller.current;
    if (!el) return;
    const measure = () => {
      const next = { above: el.scrollTop > 0, below: el.scrollTop + el.clientHeight < el.scrollHeight - 1 };
      setEdges((before) => (before.above === next.above && before.below === next.below ? before : next));
    };
    measure();
    el.addEventListener('scroll', measure, { passive: true });
    // The content grows and shrinks as branches fold, and the pane resizes; both move the edges.
    const watcher = new ResizeObserver(measure);
    watcher.observe(el);
    if (el.firstElementChild) watcher.observe(el.firstElementChild);
    return () => {
      el.removeEventListener('scroll', measure);
      watcher.disconnect();
    };
  }, [scroller]);
  return edges;
}
