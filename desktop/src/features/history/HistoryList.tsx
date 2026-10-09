import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import design from '../../design/tokens.json';
import { HistoryRow } from './HistoryRow';
import { indexOfRow, layout, stepRow, stickyGroup, windowOf, type ListEntry, type Metrics } from './model';
import type { HistoryItem } from './types';

const px = (value: string) => Number.parseFloat(value);
const foundation = design.foundation;
/** The fixed heights the virtual list is laid out with: the same tokens the stylesheet draws them with. */
export const metrics: Metrics = { group: px(foundation['history-group-height']), row: px(foundation['history-row-height']), rowFiles: px(foundation['history-row-files-height']), overscan: Number.parseInt(foundation['history-overscan'], 10) };
/** Rows left below the viewport when the next page is asked for. */
const NEAR_END = 8;

type Props = {
  items: readonly HistoryItem[]; now: number; selectedId?: string; label: string;
  onSelect: (item: HistoryItem) => void;
  onOpen: (item: HistoryItem, event: { newTab: boolean }) => void;
  onRead?: (item: HistoryItem) => void;
  onArchive?: (item: HistoryItem) => void;
  onNearEnd: () => void;
  /** Focus returns to the list when the person arrows out of the field. */
  listRef?: (node: HTMLDivElement | null) => void;
};

/** Sets CSS custom properties from layout numbers: the stylesheet places the entry, so no inline style is written. */
const place = (top: number, height: number) => (node: HTMLElement | null) => {
  if (!node) return;
  node.style.setProperty('--history-top', `${top}px`);
  node.style.setProperty('--history-height', `${height}px`);
};

/**
 * The virtualized list (design 4d): fixed-height rows under sticky time headings, newest first. Only the
 * entries near the viewport are in the DOM, so thousands of conversations cost the same as a dozen.
 * Up and Down move the selection, Enter continues, Home and End jump.
 */
export function HistoryList({ items, now, selectedId, label, onSelect, onOpen, onRead, onArchive, onNearEnd, listRef }: Props) {
  const scroller = useRef<HTMLDivElement>(null);
  const [scroll, setScroll] = useState({ top: 0, height: 0 });
  const { entries, height } = useMemo(() => layout(items, now, metrics), [items, now]);

  useLayoutEffect(() => {
    const node = scroller.current;
    if (!node) return;
    const measure = () => setScroll(current => (current.height === node.clientHeight && current.top === node.scrollTop ? current : { top: node.scrollTop, height: node.clientHeight }));
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  const range = windowOf(entries, scroll.top, scroll.height, metrics.overscan);
  useEffect(() => { if (entries.length && range.end >= entries.length - NEAR_END) onNearEnd(); }, [range.end, entries.length]);

  const selectedIndex = selectedId ? indexOfRow(entries, selectedId) : -1;
  // Keep the selected row on screen when the keyboard moves it.
  function reveal(index: number) {
    const node = scroller.current;
    const entry = entries[index];
    if (!node || !entry) return;
    if (entry.top < node.scrollTop + metrics.group) node.scrollTop = Math.max(0, entry.top - metrics.group);
    else if (entry.top + entry.height > node.scrollTop + node.clientHeight) node.scrollTop = entry.top + entry.height - node.clientHeight;
  }
  function choose(index: number) {
    const entry = entries[index];
    if (entry?.kind !== 'row') return;
    onSelect(entry.item);
    reveal(index);
  }
  function onKeyDown(event: KeyboardEvent) {
    // The row menu from the keyboard: the list holds focus, so the menu key or Shift F10 opens the selected row's.
    if (event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')) {
      const node = selectedId ? scroller.current?.querySelector<HTMLElement>(`#${CSS.escape(`history-row-${selectedId}`)}`) : null;
      if (!node) return;
      event.preventDefault();
      const bounds = node.getBoundingClientRect();
      node.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, button: 2, clientX: bounds.left + bounds.width / 2, clientY: bounds.bottom }));
      return;
    }
    if (event.altKey || event.shiftKey) return;
    const from = selectedIndex < 0 ? -1 : selectedIndex;
    if (event.key === 'ArrowDown') { event.preventDefault(); choose(from < 0 ? stepRow(entries, -1, 1) : stepRow(entries, from, 1)); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); choose(from < 0 ? stepRow(entries, -1, 1) : stepRow(entries, from, -1)); }
    else if (event.key === 'Home') { event.preventDefault(); choose(stepRow(entries, -1, 1)); }
    else if (event.key === 'End') { event.preventDefault(); choose(stepRow(entries, entries.length, -1)); }
    else if (event.key === 'Enter' && selectedIndex >= 0) { event.preventDefault(); const entry = entries[selectedIndex]; if (entry.kind === 'row') onOpen(entry.item, { newTab: event.metaKey || event.ctrlKey }); }
  }
  // A selection made elsewhere (a click, the first row on load) is brought into view too.
  useEffect(() => { if (selectedIndex >= 0) reveal(selectedIndex); }, [selectedId]);

  const sticky = stickyGroup(entries, scroll.top);
  const visible = entries.slice(range.start, range.end);
  const setRef = (node: HTMLDivElement | null) => { scroller.current = node; listRef?.(node); };
  return <div ref={setRef} className="history-list" data-scroll-key="history-list" role="listbox" tabIndex={0} aria-label={label} aria-activedescendant={selectedId ? `history-row-${selectedId}` : undefined}
    onScroll={event => setScroll({ top: event.currentTarget.scrollTop, height: event.currentTarget.clientHeight })} onKeyDown={onKeyDown}>
    {sticky && <div className="history-sticky" aria-hidden="true" ref={node => { if (node) node.style.setProperty('--history-push', `${sticky.push}px`); }}><span>{sticky.label}</span></div>}
    <div className="history-list-space" ref={node => { if (node) node.style.setProperty('--history-total', `${height}px`); }}>
      {visible.map(entry => <Entry key={entry.key} entry={entry} now={now} selectedId={selectedId} onSelect={onSelect} onOpen={onOpen} onRead={onRead} onArchive={onArchive}/>)}
    </div>
  </div>;
}

function Entry({ entry, now, selectedId, onSelect, onOpen, onRead, onArchive }: { entry: ListEntry; now: number; selectedId?: string } & Pick<Props, 'onSelect' | 'onOpen' | 'onRead' | 'onArchive'>) {
  if (entry.kind === 'group') return <div className="history-group" role="presentation" ref={place(entry.top, entry.height)}><span>{entry.label}</span></div>;
  return <HistoryRow item={entry.item} now={now} domId={`history-row-${entry.item.id}`} selected={entry.item.id === selectedId} onSelect={onSelect} onOpen={onOpen} onRead={onRead} onArchive={onArchive} placeRef={place(entry.top, entry.height)}/>;
}
