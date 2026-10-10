import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { Button, Icon, KeyboardShortcut, PageHeading, Text, TextInput } from '../../components/ui';
import design from '../../design/tokens.json';
import { isMac } from '../../design/keyboard';
import type { TabSummary } from '../conversation/tabSummary';
import type { PaneRenderProps } from '../tabs/kinds/slots';
import type { HistoryPress } from './historyMenu';
import { HistoryList } from './HistoryList';
import { useHistoryHost } from './host';
import { queryTerms } from './model';
import { ReadView } from './ReadView';
import { RecapPane } from './RecapPane';
import { SearchResults } from './SearchResults';
import { useHistoryDetail, useHistoryList, useHistorySearch } from './useHistory';
import { filterLabels, historyFilters, type HistoryDetail, type HistoryFilter, type HistoryItem } from './types';
import './history.css';

const compactBelow = Number.parseFloat(design.foundation['history-compact-width']);

type Reading = { id: string; at?: number };
type Press = HistoryPress;

/** How wide the pane is, so a split or a small window can drop the recap card beside the list. */
function useCompact(ref: React.RefObject<HTMLElement | null>): boolean {
  const [compact, setCompact] = useState(false);
  useLayoutEffect(() => {
    const node = ref.current;
    if (!node || typeof ResizeObserver === 'undefined') return;
    const measure = () => setCompact(node.clientWidth < compactBelow);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, [ref]);
  return compact;
}

/**
 * The History tab (design 4a, 4b): every conversation under its time heading with one sentence of what was
 * discussed or decided, a living recap beside it, and search in plain words. Opening a result continues the
 * conversation in this tab's place (or reads it here, read-only).
 */
export function HistoryPane({ pane, focused, actions }: PaneRenderProps) {
  const host = useHistoryHost();
  const root = useRef<HTMLDivElement>(null);
  const field = useRef<HTMLInputElement>(null);
  const list = useRef<HTMLDivElement | null>(null);
  const compact = useCompact(root);
  const [filter, setFilter] = useState<HistoryFilter>('all');
  // A search handed over by the new-tab field ("See all N in History") arrives as the pane's draft: it seeds the field once and is consumed.
  const [query, setQuery] = useState(pane.draft);
  const [selectedId, setSelectedId] = useState<string>();
  const [recapOpen, setRecapOpen] = useState(false);
  const [reading, setReading] = useState<Reading>();
  const [now, setNow] = useState(Date.now);
  useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), design.interaction.activityRefreshInterval); return () => window.clearInterval(timer); }, []);

  useEffect(() => { if (!pane.draft) return; setQuery(pane.draft); setReading(undefined); actions.onDraft(''); }, [pane.draft]);

  const searching = query.trim().length > 0;
  const { items, total, loading, error, loadMore, refresh } = useHistoryList(filter);
  const [revision, setRevision] = useState(0);
  const search = useHistorySearch(query, filter);
  const showingRecap = !searching && !reading && (!compact || recapOpen);
  const { detail } = useHistoryDetail(showingRecap ? selectedId : undefined, revision);

  // The first row is selected until the person picks another, and again when the selected one leaves the list.
  useEffect(() => { if (items.length && !items.some(item => item.id === selectedId) && !(selectedId && recapOpen)) setSelectedId(items[0].id); }, [items]);

  // The tab says what it is showing, the way the design's tab reads "History · lexer".
  // The subject of a question trails it ("what did we decide about the lexer"), so the last content word names the tab.
  const lead = search.result?.best?.terms?.slice(-1)[0] ?? queryTerms(query).slice(-1)[0] ?? '';
  const tabTitle = searching && lead ? `History · ${lead}` : 'History';
  useEffect(() => {
    const blank: TabSummary = { title: tabTitle, firstLine: '', digest: '' };
    actions.onSummary(blank);
  }, [tabTitle]);

  // ⌘F focuses the field while this pane is the one in front.
  useEffect(() => {
    if (!focused) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== 'f' || event.altKey || event.shiftKey) return;
      if (!(isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey)) return;
      if (document.querySelector('dialog[open]')) return;
      event.preventDefault();
      setReading(undefined);
      field.current?.focus();
      field.current?.select();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [focused]);

  const openConversation = useCallback((item: HistoryItem, press: Press) => host?.continueConversation(item, pane.id, press), [host, pane.id]);
  // Archive from the row menu: the list and the recap are read again so the row says "archived" at once.
  const archiveConversation = useCallback((item: HistoryItem) => {
    void host?.archiveConversation(item).catch(() => undefined).finally(() => { refresh(); setRevision(value => value + 1); });
  }, [host, refresh]);
  const showRecap = (id: string) => { setQuery(''); setReading(undefined); setSelectedId(id); setRecapOpen(true); };
  const onFieldKey = (event: React.KeyboardEvent<HTMLInputElement>) => {
    if (event.key === 'Escape') { if (query) { event.preventDefault(); event.stopPropagation(); setQuery(''); } else field.current?.blur(); }
    else if (event.key === 'ArrowDown' && !searching) { event.preventDefault(); list.current?.focus(); }
  };

  if (reading) {
    return <div ref={root} className="history-pane" data-mode="read" data-compact={compact || undefined}>
      <section className="history-card history-card-read" aria-label="Conversation">
        <ReadView id={reading.id} at={reading.at} now={now} onBack={() => setReading(undefined)} onContinue={(read: HistoryDetail, press) => openConversation(read.item, press)}/>
      </section>
    </div>;
  }

  const filters = <div className="history-filters" role="group" aria-label="Filter">
    {historyFilters.map(one => <Button key={one} variant="ghost" className="history-pill" aria-pressed={filter === one} onClick={() => setFilter(one)}>{filterLabels[one]}</Button>)}
  </div>;
  const count = total > 0 ? `${total.toLocaleString()} ${total === 1 ? 'conversation' : 'conversations'}` : '';
  const emptyLine = filter === 'all' ? 'Conversations you have had appear here.' : 'No conversation matches this filter.';
  const recap = detail && <RecapPane detail={detail} now={now} onBack={compact ? () => setRecapOpen(false) : undefined}
    onContinue={press => openConversation(detail.item, press)} onRead={() => setReading({ id: detail.item.id })}/>;

  // One card holds the field in both modes, in the same place in the tree, so typing never loses focus.
  const body = searching
    ? (search.error ? <p className="history-empty" role="alert">{search.error}</p>
      : search.result ? <div className="history-results-scroll" data-scroll-key="history-results"><SearchResults result={search.result} now={now} onRecap={id => showRecap(id)} onJump={(id, index) => setReading({ id, at: index })} onContinue={openConversation}/></div> : null)
    : error ? <p className="history-empty" role="alert">{error}</p>
    : items.length ? <HistoryList items={items} now={now} selectedId={selectedId} label="Conversations" listRef={node => { list.current = node; }}
        onSelect={item => { setSelectedId(item.id); if (compact) setRecapOpen(true); }} onOpen={openConversation}
        onRead={item => { setSelectedId(item.id); setReading({ id: item.id }); }} onArchive={host ? archiveConversation : undefined} onNearEnd={loadMore}/>
    : !loading ? <Text className="history-empty">{emptyLine}</Text> : null;
  const card = <section className="history-card" data-mode={searching ? 'search' : 'browse'} aria-label={searching ? 'Search results' : 'Conversations'}>
    <div className="history-column">
      {!searching && <div className="history-head"><PageHeading className="history-title">History</PageHeading>{count && <span className="history-count">{count}</span>}</div>}
      <div className="history-field" data-filled={searching || undefined}>
        <Icon name="search" size="sm"/>
        <TextInput ref={field} aria-label="Search history" placeholder="Search what you discussed, decided or changed" value={query} onChange={event => setQuery(event.target.value)} onKeyDown={onFieldKey} autoComplete="off" spellCheck={false}/>
        {searching ? <span className="history-field-hint">Esc to clear</span> : <KeyboardShortcut label="⌘/Ctrl F"/>}
      </div>
      {filters}
      {body}
    </div>
  </section>;
  return <div ref={root} className="history-pane" data-mode={searching ? 'search' : 'browse'} data-compact={compact || undefined}>
    {(searching || !compact || !recapOpen) && card}
    {showingRecap && recap}
  </div>;
}
