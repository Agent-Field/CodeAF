import { useEffect, useState, type RefObject } from 'react';

/** The cursor draws immediately; other bodies mount once their card reaches the scroll viewport. */
export function usePreviewVisible(host: RefObject<HTMLElement | null>, eager: boolean): boolean {
  const [seen, setSeen] = useState(false);
  useEffect(() => {
    const node = host.current;
    if (!node || eager || seen) return;
    const root = node.closest('.overview-body, .overview-film-track');
    const observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) {
        setSeen(true);
        observer.disconnect();
      }
    }, { root });
    observer.observe(node);
    return () => observer.disconnect();
  }, [host, eager, seen]);
  return eager || seen;
}
