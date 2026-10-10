import { useLayoutEffect, useRef, type HTMLAttributes, type KeyboardEvent } from 'react';

const focusable = '[data-places-tile-focusable]';

/**
 * The grid of place tiles (Places 8a, 8g). Four columns, fewer when the page is narrow (the CSS decides),
 * with the New place tile last. It is ONE tab stop: Tab enters on the tile that was last focused, and the
 * arrow keys, Home and End move between tiles by row and column. Enter and click (Go to), Command or
 * Control Enter and click (new window) and Space (Quick Look) belong to each tile, so this grid only moves focus.
 */
export function PlaceGrid({ label, onKeyDown, onFocus, className = '', ...props }: HTMLAttributes<HTMLUListElement> & { label: string }) {
  const list = useRef<HTMLUListElement>(null);
  // The tile that holds the tab stop. Kept as an element so a reordered or re-rendered grid keeps the stop where the person left it.
  const stop = useRef<HTMLElement | null>(null);

  // Tiles come and go (a place is created, archived, filed away), so the stop is re-asserted after every render.
  useLayoutEffect(() => {
    const tiles = [...(list.current?.querySelectorAll<HTMLElement>(focusable) ?? [])];
    const enabled = tiles.filter(tile => !(tile as HTMLButtonElement).disabled);
    const holder = stop.current && enabled.includes(stop.current) ? stop.current : enabled[0];
    stop.current = holder ?? null;
    for (const tile of tiles) tile.tabIndex = tile === holder ? 0 : -1;
  });

  const claim = (event: React.FocusEvent<HTMLUListElement>) => {
    onFocus?.(event);
    const target = event.target as HTMLElement;
    if (!target.matches(focusable) || target === stop.current) return;
    stop.current?.setAttribute('tabindex', '-1');
    target.tabIndex = 0;
    stop.current = target;
  };

  const move = (event: KeyboardEvent<HTMLUListElement>) => {
    onKeyDown?.(event);
    const target = event.target as HTMLElement;
    if (event.defaultPrevented || !target.matches(focusable)) return;
    const tiles = [...event.currentTarget.querySelectorAll<HTMLElement>(`${focusable}:not(:disabled)`)];
    const index = tiles.indexOf(target);
    const here = target.getBoundingClientRect();
    // Up and Down stay in the neighbouring row and pick the tile nearest this column, so a short last row is still reachable.
    const vertical = (down: boolean) => {
      const rows = tiles.filter(tile => { const top = tile.getBoundingClientRect().top; return down ? top > here.top : top < here.top; });
      if (!rows.length) return undefined;
      const nearest = rows.reduce((best, tile) => {
        const top = tile.getBoundingClientRect().top;
        return down ? Math.min(best, top) : Math.max(best, top);
      }, down ? Infinity : -Infinity);
      return rows.filter(tile => tile.getBoundingClientRect().top === nearest)
        .sort((a, b) => Math.abs(a.getBoundingClientRect().left - here.left) - Math.abs(b.getBoundingClientRect().left - here.left))[0];
    };
    const next = event.key === 'ArrowRight' ? tiles[index + 1] : event.key === 'ArrowLeft' ? tiles[index - 1]
      : event.key === 'ArrowDown' ? vertical(true) : event.key === 'ArrowUp' ? vertical(false)
      : event.key === 'Home' ? tiles[0] : event.key === 'End' ? tiles[tiles.length - 1] : undefined;
    if (!next) return;
    event.preventDefault();
    next.focus();
  };

  return <ul {...props} ref={list} aria-label={label} className={`places-tile-grid ${className}`} onKeyDown={move} onFocus={claim}/>;
}
