import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';

/**
 * Whether a horizontal scroller still has content past its right edge, which is when its
 * 24px edge fade shows (design 1f, Scroll edges: Horizontal). Attach `ref` and `onScroll`;
 * style on `data-more`.
 */
export function useMoreToRight<E extends HTMLElement = HTMLDivElement>(): { ref: RefObject<E | null>; more: boolean; measure: () => void } {
 const ref = useRef<E>(null);
 const [more, setMore] = useState(false);
 const measure = useCallback(() => {
  const element = ref.current;
  if (element) setMore(element.scrollLeft + element.clientWidth < element.scrollWidth - 1);
 }, []);
 // Content can grow without the scroller's own box changing, so every render measures again.
 useEffect(measure);
 useEffect(() => {
  const element = ref.current;
  if (!element || typeof ResizeObserver === 'undefined') return;
  const watch = new ResizeObserver(measure);
  watch.observe(element);
  return () => watch.disconnect();
 }, [measure]);
 return { ref, more, measure };
}
