import { useEffect, useLayoutEffect, useMemo, useRef, useState, type KeyboardEvent } from 'react';
import { createPortal } from 'react-dom';
import design from '../../design/tokens.json';
import { DeleteConfirm } from './DeleteConfirm';
import { HistoryRow } from './HistoryRow';
import { indexOfRow, layout, stepRow, stickyGroup, windowOf, type ListEntry, type Metrics } from './model';
import type { HistoryItem } from './types';

const px = (value: string) => Number.parseFloat(value);
const foundation = design.foundation;
/** The fixed heights the virtual list is laid out with: the same tokens the stylesheet draws them with. */
export const metrics: Metrics = { group: px(foundation['history-group-height']), row: px(foundation['history-row-height']), rowFiles: px(foundation['history-row-files-height']), overscan: Number.parseInt(foundation['history-overscan'], 10) };
/** Rows left below the viewport when the next page is asked for. */
const NEAR_END = 8;

export type HistoryConfirm = { anchorId: string; count: number; busy: boolean; onCancel: () => void; onConfirm: () => void };

type Props = {
  items: readonly HistoryItem[]; now: number; selectedId?: string; label: string;
  /** Every row in the shift-click run. Absent means the single selectedId is the selection. */
  selectedIds?: readonly string[];
  /** Replaces the selection's first archived row. Never a dialog. */
  confirm?: HistoryConfirm;
  onSelect: (item: HistoryItem, gesture?: { shift: boolean }) => void;
  onOpen: (item: HistoryItem, event: { newTab: boolean }) => void;
  onRead?: (item: HistoryItem) => void;
  onArchive?: (item: HistoryItem) => void;
  onDelete?: (item: HistoryItem) => void;
  onNearEnd: () => void;
  /** Focus returns to the list when the person arrows out of the field. */
  listRef?: (node: HTMLDivElement | null) => void;
  /**
   * Where the inline confirm is painted. It has to sit outside the listbox: a listbox may only own options,
   * and the question's buttons are not options. The layer is aligned over the list, so the question still
   * covers the row it replaces.
   */
  confirmLayer?: HTMLElement | null;
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
export function HistoryList({ items, now, selectedId, selectedIds, confirm, label, onSelect, onOpen, onRead, onArchive, onDelete, onNearEnd, listRef, confirmLayer }: Props) {
  const scroller = useRef<HTMLDivElement>(null);
  const spaceNode = useRef<HTMLDivElement | null>(null);
  const confirmNode = useRef<HTMLDivElement | null>(null);
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
    // While the confirm is up the row is gone, so the list must not continue it or move the selection out from under the question.
    if (confirm) {
      if (event.key === 'Escape' && !confirm.busy) { event.preventDefault(); confirm.onCancel(); }
      return;
    }
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
  const confirming = confirm && selectedId === confirm.anchorId;
  const anchor = confirm ? entries.find(entry => entry.kind === 'row' && entry.item.id === confirm.anchorId) : undefined;
  // The layer is a sibling of the list, sized to the list's box, so the question scrolls and clips with the rows
  // without becoming a child of the listbox.
  useLayoutEffect(() => {
    const list = scroller.current;
    const layer = confirmLayer ?? null;
    const parent = layer?.parentElement;
    if (!list || !layer || !parent) return;
    const listBox = list.getBoundingClientRect();
    const parentBox = parent.getBoundingClientRect();
    layer.style.setProperty('--history-top', `${listBox.top - parentBox.top}px`);
    layer.style.setProperty('--history-left', `${listBox.left - parentBox.left}px`);
    layer.style.setProperty('--history-width', `${listBox.width}px`);
    layer.style.setProperty('--history-height', `${listBox.height}px`);
    const space = spaceNode.current;
    const node = confirmNode.current;
    if (!space || !node || anchor?.kind !== 'row') return;
    const spaceBox = space.getBoundingClientRect();
    node.style.setProperty('--history-top', `${spaceBox.top - listBox.top + anchor.top}px`);
    node.style.setProperty('--history-left', `${spaceBox.left - listBox.left}px`);
    node.style.setProperty('--history-width', `${spaceBox.width}px`);
    node.style.setProperty('--history-height', `${anchor.height}px`);
  }, [confirmLayer, confirm, anchor, scroll.top, scroll.height, height]);
  const setSpace = (node: HTMLDivElement | null) => {
    spaceNode.current = node;
    if (node) node.style.setProperty('--history-total', `${height}px`);
  };
  return <div ref={setRef} className="history-list" data-scroll-key="history-list" role="listbox" tabIndex={0} aria-label={label} aria-multiselectable={selectedIds ? true : undefined} aria-activedescendant={selectedId && !confirming ? `history-row-${selectedId}` : undefined}
    onScroll={event => setScroll({ top: event.currentTarget.scrollTop, height: event.currentTarget.clientHeight })} onKeyDown={onKeyDown}>
    {sticky && <div className="history-sticky" aria-hidden="true" ref={node => { if (node) node.style.setProperty('--history-push', `${sticky.push}px`); }}><span>{sticky.label}</span></div>}
    <div className="history-list-space" ref={setSpace}>
      {visible.map(entry => <Entry key={entry.key} entry={entry} now={now} selectedId={selectedId} selectedIds={selectedIds} confirm={confirm} onSelect={onSelect} onOpen={onOpen} onRead={onRead} onArchive={onArchive} onDelete={onDelete}/>)}
    </div>
    {confirm && anchor?.kind === 'row' && confirmLayer && createPortal(
      <DeleteConfirm count={confirm.count} busy={confirm.busy} onCancel={confirm.onCancel} onConfirm={confirm.onConfirm} placeRef={node => { confirmNode.current = node; }}/>,
      confirmLayer)}
  </div>;
}

function Entry({ entry, now, selectedId, selectedIds, confirm, onSelect, onOpen, onRead, onArchive, onDelete }: { entry: ListEntry; now: number; selectedId?: string } & Pick<Props, 'selectedIds' | 'confirm' | 'onSelect' | 'onOpen' | 'onRead' | 'onArchive' | 'onDelete'>) {
  if (entry.kind === 'group') return <div className="history-group" role="presentation" ref={place(entry.top, entry.height)}><span>{entry.label}</span></div>;
  // The row is gone while the question is up. The question itself is portaled onto the layer, not drawn here.
  if (confirm && entry.item.id === confirm.anchorId) return null;
  const selected = selectedIds ? selectedIds.includes(entry.item.id) : entry.item.id === selectedId;
  return <HistoryRow item={entry.item} now={now} domId={`history-row-${entry.item.id}`} selected={selected} onSelect={onSelect} onOpen={onOpen} onRead={onRead} onArchive={onArchive} onDelete={onDelete} placeRef={place(entry.top, entry.height)}/>;
}
