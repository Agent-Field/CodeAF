import { useEffect, useState, type RefObject } from 'react';
import { isMoreBelow } from '../../../components/ui';

/**
 * True while the scrolling middle has more content below its visible edge, so the
 * bottom fade (1f Tray body) appears only on an edge that has something beyond it.
 * The pinned answers sit under that middle and never scroll, so the body itself
 * is not the scroller. `page` re-arms the observers when another card takes the body.
 */
export function useMoreBelow(body: RefObject<HTMLElement | null>, page: string): boolean {
  const [more, setMore] = useState(false);
  useEffect(() => {
    const root = body.current;
    const element = root?.querySelector<HTMLElement>('.tray-scroll') ?? root;
    if (!element) return;
    // A fresh card starts at the top. The fade is how the reader knows the rest of
    // the question is below the answers, which stay pinned underneath.
    element.scrollTop = 0;
    const measure = () => setMore(isMoreBelow(element));
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    for (const child of Array.from(element.children)) observer.observe(child);
    element.addEventListener('scroll', measure, { passive: true });
    window.addEventListener('resize', measure);
    measure();
    return () => {
      observer.disconnect();
      element.removeEventListener('scroll', measure);
      window.removeEventListener('resize', measure);
    };
  }, [body, page]);
  return more;
}
