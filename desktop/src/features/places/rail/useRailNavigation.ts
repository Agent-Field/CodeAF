import { useEffect, useRef, type FocusEvent, type KeyboardEvent } from 'react';

/** One rail row owns the tab stop; arrows move focus without changing the window's place. */
export function useRailNavigation(selection: string) {
  const ref = useRef<HTMLElement>(null);
  const rows = () => Array.from(ref.current?.querySelectorAll<HTMLButtonElement>('.nav-item:not(:disabled)') ?? []);
  function stopAt(row: HTMLButtonElement) {
    rows().forEach(item => { item.tabIndex = item === row ? 0 : -1; });
  }
  useEffect(() => {
    const list = rows();
    const focused = list.find(row => row === document.activeElement);
    const current = list.find(row => row.getAttribute('aria-current') === 'page');
    const entry = focused ?? current ?? list[0];
    if (entry) stopAt(entry);
  }, [selection]);
  return {
    ref,
    onFocusCapture: (event: FocusEvent<HTMLElement>) => {
      if (event.target instanceof HTMLButtonElement && event.target.matches('.nav-item')) stopAt(event.target);
    },
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return;
      if (!(event.target instanceof HTMLButtonElement) || !event.target.matches('.nav-item')) return;
      const list = rows(), index = list.indexOf(event.target);
      const next = event.key === 'ArrowDown' ? (index + 1) % list.length
        : event.key === 'ArrowUp' ? (index - 1 + list.length) % list.length
        : event.key === 'Home' ? 0 : event.key === 'End' ? list.length - 1 : undefined;
      if (next === undefined) return;
      event.preventDefault();
      stopAt(list[next]);
      list[next].focus();
    },
  };
}
