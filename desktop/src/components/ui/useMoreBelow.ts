import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';

/** True while a vertical scroller still has content past its bottom edge (1px of rounding slack). */
export function isMoreBelow(element: HTMLElement): boolean {
 return element.scrollTop + element.clientHeight < element.scrollHeight - 1;
}

/**
 * Whether a bounded vertical scroller still has content below its visible edge, which is when
 * its bottom fade shows, so a hard cut never reads as the end of the text. Attach `ref` and
 * `onScroll`; style on `data-more`. Mirrors `useMoreToRight` for the block axis.
 */
export function useMoreBelow<E extends HTMLElement = HTMLDivElement>(): { ref: RefObject<E | null>; more: boolean; measure: () => void } {
 const ref = useRef<E>(null);
 const [more, setMore] = useState(false);
 const measure = useCallback(() => {
  const element = ref.current;
  if (element) setMore(isMoreBelow(element));
 }, []);
 // Content can grow without the scroller's own box changing (it is capped), so every render measures again.
 useEffect(measure);
 useEffect(() => {
  const element = ref.current;
  if (!element || typeof ResizeObserver === 'undefined') return;
  const watch = new ResizeObserver(measure);
  watch.observe(element);
  window.addEventListener('resize', measure);
  return () => {
   watch.disconnect();
   window.removeEventListener('resize', measure);
  };
 }, [measure]);
 return { ref, more, measure };
}
