import { useEffect, useId, useMemo, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from 'react';
import { Button, Icon, KeyboardShortcut, TextInput } from '../../../components/ui';
import { isMac } from '../../../design/keyboard';
import { PlaceSwatch } from '../components/PlaceSwatch';
import { Suggestion } from '../home/Suggestion';
import { chooserTitle, chooserView, countLabel, firstChoosable, parentRowKey, step, type ChooserRow } from './chooserModel';
import type { ChooserMode, ChooserProps } from './contracts';
import './go-to-chooser.css';

/** ↵'s verb in the footer, per mode: going is "open", every other mode ends in one write named by its question. */
const enterVerb: Record<ChooserMode['kind'], string> = { go: 'open', merge: 'merge', parent: 'add', file: 'add' };

/** The words a rejection without a message is shown as. */
const fallbackFailure = 'That did not go through. Try again.';

/** Row text for a screen reader: the name, where it sits, its status and whether its branch is open. Option children are
 * presentational, so the disclosure state cannot live on a nested control and is spoken here instead. */
function spokenRow(row: ChooserRow): string {
  const parts = [row.place.name];
  if (row.context) parts.push(`in ${row.context}`);
  if (row.place.statusLabel) parts.push(row.place.statusLabel);
  if (row.meta) parts.push(row.meta);
  if (row.hasChildren) parts.push(row.expanded ? 'expanded' : 'collapsed');
  if (row.disabled) parts.push('not offered');
  return parts.join(', ');
}

/**
 * Go to (Places 6c, "All places (⌘P)"): one search field over Recent and the All tree, or a flat list while typing.
 * The same sheet answers merge, second-parent and filing questions; it never fetches and never writes by itself. Every
 * write is the owner's callback, and while one is pending the rows hold still; a refusal is read out inside the sheet.
 */
export function GoToChooser({ open, mode, places, childrenOf, total, now, onChoose, onChooseInNewWindow, onCreate, suggestion, onClose }: ChooserProps) {
  const dialog = useRef<HTMLDialogElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const opener = useRef<HTMLElement | null>(null);
  const openRef = useRef(open);
  openRef.current = open;
  const [query, setQuery] = useState('');
  const [expansion, setExpansion] = useState<ReadonlyMap<string, boolean>>(() => new Map());
  const [activeKey, setActiveKey] = useState<string>();
  const [pending, setPending] = useState(false);
  const [failure, setFailure] = useState('');
  const baseId = useId();
  const listId = `${baseId}-list`;

  const view = useMemo(() => chooserView({ places, childrenOf, now }, query, mode, expansion), [places, childrenOf, now, query, mode, expansion]);
  const active = view.rows.find(row => row.key === activeKey) ?? view.rows.find(row => row.key === firstChoosable(view.rows));
  const optionId = (row: ChooserRow) => `${baseId}-o${view.rows.indexOf(row)}`;
  const title = chooserTitle(mode);
  const typed = query.trim();
  const newWindow = mode.kind === 'go' ? onChooseInNewWindow : undefined;

  useEffect(() => {
    const element = dialog.current;
    if (!element) return;
    if (open && !element.open) {
      opener.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      element.showModal();
      search.current?.focus();
    } else if (!open && element.open) element.close();
  }, [open]);
  // Leaving the page while open: no close event will come, so focus goes back by hand. React has already cleared the dialog ref by now,
  // so the opener (forgotten by `closed` after a normal close) is the only sign that the sheet was still open.
  useEffect(() => () => { if (opener.current?.isConnected) opener.current.focus(); }, []);
  useEffect(() => {
    if (active) document.getElementById(optionId(active))?.scrollIntoView({ block: 'nearest' });
  }, [active?.key]);

  /** The dialog has closed, by its owner or by the browser: forget the visit and give focus back to where it came from. */
  const closed = () => {
    setQuery(''); setExpansion(new Map()); setActiveKey(undefined); setFailure(''); setPending(false);
    if (opener.current?.isConnected) opener.current.focus();
    opener.current = null;
    if (openRef.current) onClose();
  };

  const sentence = (error: unknown) => (error instanceof Error && error.message ? error.message : typeof error === 'string' && error ? error : fallbackFailure);
  const run = async (action: () => void | Promise<void>) => {
    if (pending) return;
    setPending(true); setFailure('');
    try {
      await action();
      if (openRef.current) onClose();
    } catch (error) {
      setFailure(sentence(error));
    } finally {
      setPending(false);
    }
  };
  /** The suggestion line's verbs leave the sheet open: Archive and "Not now" change what the sheet lists, and Merge swaps the sheet's question,
   * so closing it after them (as `run` does) would shut the merge chooser they just opened. A refusal is read out the same way. */
  const runSuggestion = async (action: () => void | Promise<void>) => {
    if (pending) return;
    setPending(true); setFailure('');
    try { await action(); } catch (error) { setFailure(sentence(error)); } finally { setPending(false); }
  };
  const choose = (row: ChooserRow | undefined) => { if (row && !row.disabled) void run(() => onChoose(row.place.id)); };
  const create = () => { if (onCreate && typed) void run(() => onCreate(typed)); };
  const toggle = (row: ChooserRow, to = !row.expanded) => {
    if (!row.hasChildren || row.expanded === to) return;
    setExpansion(previous => new Map(previous).set(row.path, to));
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLInputElement>) => {
    const primary = isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey;
    if (event.nativeEvent.isComposing) return;
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault();
      setActiveKey(step(view.rows, active?.key, event.key === 'ArrowDown' ? 1 : -1));
      return;
    }
    if (event.key === 'Enter') {
      event.preventDefault();
      if (pending) return;
      if (primary && !event.shiftKey && !event.altKey) {
        if (newWindow && active && !active.disabled) void run(() => newWindow(active.place.id));
        return;
      }
      if (active) { if (active.disabled) toggle(active); else choose(active); }
      else if (view.searching) create();
      return;
    }
    if (primary && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 'n') {
      // Always cancelled: the browser's own ⌘N would open a window over the sheet.
      event.preventDefault();
      if (!pending) create();
      return;
    }
    // ← and → belong to the caret while there is text; in the tree they open and close branches.
    if (!view.searching && active?.section === 'all' && !event.shiftKey && !event.altKey && !event.metaKey && !event.ctrlKey) {
      if (event.key === 'ArrowRight' && active.hasChildren) {
        event.preventDefault();
        if (!active.expanded) toggle(active, true);
        else setActiveKey(step(view.rows, active.key, 1));
      } else if (event.key === 'ArrowLeft') {
        event.preventDefault();
        if (active.expanded) toggle(active, false);
        else { const up = parentRowKey(active); if (up) setActiveKey(up); }
      }
    }
  };

  const renderRow = (row: ChooserRow) => {
    const selected = row.key === active?.key;
    return <div key={row.key} id={optionId(row)} role="option" aria-selected={selected} aria-disabled={row.disabled || pending || undefined}
      aria-label={spokenRow(row)} className="goto-row" data-disabled={row.disabled || undefined}
      onPointerMove={() => { if (!selected) setActiveKey(row.key); }}
      onClick={() => { if (pending) return; if (row.disabled) toggle(row); else choose(row); }}>
      <span className="goto-lead">
        {Array.from({ length: row.depth }, (_, level) => <span key={level} className="goto-indent"/>)}
        {row.section === 'all' && row.hasChildren
          ? <span className="goto-disclosure" data-open={row.expanded} onClick={event => { event.stopPropagation(); toggle(row); }}><Icon name="chevron" size="micro" motion="disclosure"/></span>
          : <span className="goto-disclosure-slot"/>}
      </span>
      <PlaceSwatch tint={row.place.tint} role="rail"/>
      <span className="goto-name">{row.place.name}{row.context && <span className="goto-context"> · {row.context}</span>}</span>
      {row.place.status && <span className="goto-dot" data-status={row.place.status} title={row.place.statusLabel}/>}
      {row.meta && <span className="goto-meta">{row.meta}</span>}
    </div>;
  };

  /** Recent and All carry their 6c labels; search results are one unlabelled list, named only for a screen reader. */
  const section = (label: string, rows: ChooserRow[], name: string, drawLabel = true) => rows.length > 0 && <div role="group" className="goto-group" data-section={name}
    aria-labelledby={drawLabel ? `${baseId}-${name}` : undefined} aria-label={drawLabel ? undefined : label}>
    {drawLabel && <span id={`${baseId}-${name}`} className="goto-section">{label}</span>}
    {rows.map(renderRow)}
  </div>;

  return <dialog ref={dialog} className="goto-chooser" aria-label={title} onClose={closed}
    onCancel={event => { event.preventDefault(); onClose(); }}
    onClick={event => { if (event.target === dialog.current) onClose(); }}>
    <div className="goto-sheet">
      <div className="goto-search">
        <Icon name="search" size="md"/>
        <TextInput ref={search} role="combobox" aria-expanded="true" aria-controls={listId} aria-autocomplete="list"
          aria-activedescendant={active ? optionId(active) : undefined} aria-label={title} placeholder={title}
          value={query} autoComplete="off" spellCheck={false}
          onChange={event => { setQuery(event.target.value); setActiveKey(undefined); setFailure(''); }} onKeyDown={onKeyDown}/>
        {countLabel(total) && <span className="goto-count">{countLabel(total)}</span>}
      </div>
      <div className="goto-rule"/>
      {view.searching && view.results.length === 0 && <div className="goto-empty">
        <p className="goto-empty-line">No place called “{typed}”.</p>
        {onCreate && <Button variant="quiet" disabled={pending} onClick={create}>Create “{typed}” <KeyboardShortcut label="⌘/Ctrl N"/></Button>}
      </div>}
      <div id={listId} role="listbox" aria-label={title} aria-busy={pending || undefined} className="goto-list" data-searching={view.searching || undefined}
        onMouseDown={event => event.preventDefault()}>
        {section('Recent', view.recent, 'recent')}
        {view.searching ? section('Matching places', view.results, 'results', false) : section('All', view.tree, 'all')}
      </div>
      {suggestion && mode.kind === 'go' && !view.searching && <Suggestion layout="line" className="goto-suggestion" text={suggestion.text} actions={suggestion.actions} run={runSuggestion} busy={pending}/>}
      {failure && <p role="alert" className="goto-failure">{failure}</p>}
      <div className="goto-hints">
        <span><KeyboardShortcut label="↵"/> {enterVerb[mode.kind]}</span>
        {newWindow && <span><KeyboardShortcut label="⌘/Ctrl ↵"/> open in new window</span>}
        {onCreate && <span><KeyboardShortcut label="⌘/Ctrl N"/> new place</span>}
      </div>
    </div>
  </dialog>;
}
