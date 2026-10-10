import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { Icon, KeyboardShortcut, TextInput } from '../../../components/ui';
import { isMac } from '../../../design/keyboard';
import { PlaceSwatch } from '../components/PlaceSwatch';
import { countLabel } from '../shell/chooserModel';
import type { PlaceRowModel } from '../shell/contracts';
import { paletteView, windowSlice, type PaletteRow } from './paletteModel';
import './palette.css';

type PlaceRow = Extract<PaletteRow, { kind: 'place' }>;

export type GoToPaletteProps = {
  open: boolean;
  /** The cached snapshot of every non-archived place. The palette never fetches, so it never shows a spinner (PL-200). */
  places: readonly PlaceRowModel[];
  childrenOf: ReadonlyMap<string, readonly string[]>;
  /** Now, for Recent's relative times; injected so tests do not depend on the clock. */
  now: Date;
  /** ↵ or a click on a place row. */
  onOpen: (placeId: string) => void;
  /** ⌘↵ / Ctrl↵: open the place in a new window. */
  onOpenInNewWindow: (placeId: string) => void;
  /** ⌘N / Ctrl N, or ↵ on the Create row. `name` is what is typed, trimmed; empty when nothing is typed. */
  onCreate: (name: string) => void;
  onClose: () => void;
};

/** The key one row up or down, held at the ends; none active yet starts at the first row (↓) or the last (↑). */
function stepKey(rows: readonly PaletteRow[], from: string | undefined, by: 1 | -1): string | undefined {
  if (!rows.length) return undefined;
  const at = rows.findIndex(row => row.key === from);
  if (at === -1) return (by === 1 ? rows[0] : rows[rows.length - 1]).key;
  return rows[Math.min(rows.length - 1, Math.max(0, at + by))].key;
}

/** ← inside the tree goes to the parent row, whose key is this row's path without its last id. */
function parentKey(row: PlaceRow): string | undefined {
  return row.depth === 0 ? undefined : `all:${row.path.slice(0, row.path.lastIndexOf('/'))}`;
}

/** How far past the viewport the list draws before it windows: the visible rows are measured on scroll, this is the first paint. */
const firstViewport = 400;

/** Row text for a screen reader: the name, where it sits, its status and whether its branch is open. */
function spokenRow(row: PlaceRow): string {
  const parts = [row.place.name];
  if (row.context) parts.push(`in ${row.context}`);
  if (row.place.statusLabel) parts.push(row.place.statusLabel);
  if (row.meta) parts.push(row.meta);
  if (row.hasChildren) parts.push(row.expanded ? 'expanded' : 'collapsed');
  return parts.join(', ');
}

/** The name with the matched letters in medium weight; a path-only match leaves it plain. */
function NameRun({ name, matched }: { name: string; matched: readonly number[] }) {
  if (!matched.length) return <>{name}</>;
  const hit = new Set(matched);
  return <>{Array.from(name).map((ch, at) => hit.has(at) ? <b key={at} className="palette-hit">{ch}</b> : ch)}</>;
}

/**
 * Go to a place (Places 6c, ⌘P): a modal over the current place with one field over Recent and the All tree, or one
 * ranked list while typing. It draws only the snapshot it is handed and writes nothing; every verb is the owner's callback.
 */
export function GoToPalette({ open, places, childrenOf, now, onOpen, onOpenInNewWindow, onCreate, onClose }: GoToPaletteProps) {
  const dialog = useRef<HTMLDialogElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const scroller = useRef<HTMLDivElement>(null);
  const rows = useRef<HTMLDivElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const openRef = useRef(open);
  openRef.current = open;
  const [query, setQuery] = useState('');
  const [expansion, setExpansion] = useState<ReadonlyMap<string, boolean>>(() => new Map());
  const [activeKey, setActiveKey] = useState<string>();
  const [scrollTop, setScrollTop] = useState(0);
  const [rowHeight, setRowHeight] = useState(0);
  const baseId = useId();
  const listId = `${baseId}-list`;

  const view = useMemo(() => paletteView({ places, childrenOf, now }, query, expansion), [places, childrenOf, now, query, expansion]);
  const recent = view.rows.filter((row): row is PlaceRow => row.kind === 'place' && row.section === 'recent');
  const scrolled = view.rows.filter(row => row.kind === 'create' || row.section !== 'recent');
  const active = view.rows.find(row => row.key === activeKey) ?? view.rows[0];
  const optionId = (row: PaletteRow) => `${baseId}-o${view.rows.indexOf(row)}`;
  const typed = query.trim();
  const total = places.filter(place => !place.archived).length;

  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      element.showModal();
      search.current?.focus();
    } else if (!open && element.open) element.close();
  }, [open]);
  useEffect(() => () => { if (dialog.current?.open && opener.current?.isConnected) opener.current.focus(); }, []);
  // The row height is a token, so it is read from a drawn row once rather than repeated here as a number.
  useEffect(() => {
    const measured = scroller.current?.querySelector('.palette-row')?.getBoundingClientRect().height;
    if (measured && measured !== rowHeight) setRowHeight(measured);
  });
  useEffect(() => {
    const at = scrolled.findIndex(row => row.key === active?.key);
    const box = scroller.current;
    if (at < 0 || !box || !rowHeight) return;
    const top = at * rowHeight;
    if (top < box.scrollTop) box.scrollTop = top;
    else if (top + rowHeight > box.scrollTop + box.clientHeight) box.scrollTop = top + rowHeight - box.clientHeight;
  }, [active?.key, rowHeight]);

  const closed = () => {
    setQuery(''); setExpansion(new Map()); setActiveKey(undefined); setScrollTop(0);
    if (opener.current?.isConnected) opener.current.focus();
    opener.current = null;
    if (openRef.current) onClose();
  };

  const choose = (row: PaletteRow | undefined) => {
    if (!row) return;
    if (row.kind === 'create') onCreate(row.name); else onOpen(row.place.id);
  };
  const toggle = (row: PlaceRow, to = !row.expanded) => {
    if (!row.hasChildren || row.expanded === to) return;
    setExpansion(previous => new Map(previous).set(row.path, to));
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    if (event.nativeEvent.isComposing) return;
    const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
    const plain = !event.shiftKey && !event.altKey && !event.metaKey && !event.ctrlKey;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      setActiveKey(stepKey(view.rows, active?.key, event.key === 'ArrowDown' ? 1 : -1));
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      if (primary && !event.shiftKey && !event.altKey) { if (active?.kind === 'place') onOpenInNewWindow(active.place.id); return; }
      choose(active);
      return;
    }
    if (primary && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 'n') {
      // Always cancelled: the browser's own ⌘N would open a window over the sheet.
      event.preventDefault();
      onCreate(typed);
      return;
    }
    // ← and → belong to the caret while there is text; in the tree they close and open branches.
    if (!view.searching && active?.kind === 'place' && active.section === 'all' && plain) {
      if (event.key === 'ArrowRight' && active.hasChildren) {
        event.preventDefault();
        if (!active.expanded) toggle(active, true); else setActiveKey(stepKey(view.rows, active.key, 1));
      } else if (event.key === 'ArrowLeft') {
        event.preventDefault();
        if (active.expanded) toggle(active, false);
        else { const up = parentKey(active); if (up) setActiveKey(up); }
      }
    }
  };

  const renderRow = (row: PaletteRow) => {
    const selected = row.key === active?.key;
    if (row.kind === 'create') return <div key={row.key} id={optionId(row)} role="option" aria-selected={selected} className="palette-row"
      onPointerMove={() => { if (!selected) setActiveKey(row.key); }} onClick={() => choose(row)}>
      <Icon name="plus" size="micro"/>
      <span className="palette-name">{row.label}</span>
      <KeyboardShortcut label="⌘/Ctrl N"/>
    </div>;
    return <div key={row.key} id={optionId(row)} role="option" aria-selected={selected} aria-label={spokenRow(row)} className="palette-row"
      onPointerMove={() => { if (!selected) setActiveKey(row.key); }} onClick={() => choose(row)}>
      <span className="palette-lead">
        {Array.from({ length: row.depth }, (_, level) => <span key={level} className="palette-indent"/>)}
        {row.section === 'all' && row.hasChildren
          ? <span className="palette-disclosure" data-open={row.expanded} onClick={event => { event.stopPropagation(); toggle(row); }}><Icon name="chevron" size="micro" motion="disclosure"/></span>
          : <span className="palette-disclosure-slot"/>}
      </span>
      <PlaceSwatch tint={row.place.tint} role="rail"/>
      <span className="palette-name"><NameRun name={row.place.name} matched={row.matched}/>{row.context && <span className="palette-context"> · {row.context}</span>}</span>
      {row.place.status && <span className="palette-dot" data-status={row.place.status} title={row.place.statusLabel}/>}
      {row.meta && <span className="palette-meta">{row.meta}</span>}
    </div>;
  };

  // Only the scrolling list is windowed; Recent is three rows and holds still.
  const slice = windowSlice(scrolled.length, scrollTop, scroller.current?.clientHeight || firstViewport, rowHeight || 1);
  const drawn = rowHeight ? scrolled.slice(slice.start, slice.end) : scrolled.slice(0, 20);
  // The windowed rows' padding is a measured pixel count, not a design value, so it is set on the element rather than in CSS.
  useEffect(() => {
    if (!rows.current) return;
    rows.current.style.paddingTop = `${slice.padTop}px`;
    rows.current.style.paddingBottom = `${slice.padBottom}px`;
  }, [slice.padTop, slice.padBottom]);

  return <dialog ref={dialog} className="palette" aria-label="Go to a place" onClose={closed}
    onCancel={event => { event.preventDefault(); onClose(); }}
    onClick={event => { if (event.target === dialog.current) onClose(); }}>
    <div className="palette-sheet">
      <div className="palette-search">
        <Icon name="search" size="md"/>
        <TextInput ref={search} role="combobox" aria-expanded="true" aria-controls={listId} aria-autocomplete="list"
          aria-activedescendant={active ? optionId(active) : undefined} aria-label="Go to a place, or create one" placeholder="Go to a place, or create one"
          value={query} autoComplete="off" spellCheck={false}
          onChange={event => { setQuery(event.target.value); setActiveKey(undefined); setScrollTop(0); }} onKeyDown={onKeyDown}/>
        {countLabel(total) && <span className="palette-count">{countLabel(total)}</span>}
      </div>
      <div className="palette-rule"/>
      <div id={listId} role="listbox" aria-label="Places" className="palette-list">
        {recent.length > 0 && <div role="group" className="palette-group" aria-labelledby={`${baseId}-recent`}>
          <span id={`${baseId}-recent`} className="palette-section">Recent</span>
          {recent.map(renderRow)}
        </div>}
        {scrolled.length > 0 && <>
          {!view.searching && <span id={`${baseId}-all`} className="palette-section">All</span>}
          <div ref={scroller} role="group" className="palette-scroll" aria-labelledby={view.searching ? undefined : `${baseId}-all`} aria-label={view.searching ? 'Matching places' : undefined}
            onScroll={event => setScrollTop(event.currentTarget.scrollTop)}>
            <div ref={rows}>{drawn.map(renderRow)}</div>
          </div>
        </>}
      </div>
      <div className="palette-hints">
        <span><KeyboardShortcut label="↵"/> open</span>
        <span><KeyboardShortcut label="⌘/Ctrl ↵"/> open in new window</span>
        <span><KeyboardShortcut label="⌘/Ctrl N"/> new place</span>
      </div>
    </div>
  </dialog>;
}
