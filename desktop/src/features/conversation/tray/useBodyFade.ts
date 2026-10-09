import { useEffect, useState, type RefObject } from 'react';

/**
 * True while the scrolling body has more content below its visible edge, so the
 * bottom fade (1f Tray body) appears only on an edge that has something beyond it.
 * `page` re-arms the observers when another card takes the body.
 */
export function useMoreBelow(body: RefObject<HTMLElement | null>, page: string): boolean {
  const [more, setMore] = useState(false);
  useEffect(() => {
    const element = body.current;
    if (!element) return;
    const measure = () => setMore(element.scrollHeight - element.clientHeight - element.scrollTop > 1);
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    for (const child of Array.from(element.children)) observer.observe(child);
    element.addEventListener('scroll', measure, { passive: true });
    measure();
    return () => {
      observer.disconnect();
      element.removeEventListener('scroll', measure);
    };
  }, [body, page]);
  return more;
}
