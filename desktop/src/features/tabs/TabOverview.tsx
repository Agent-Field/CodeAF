// The overview (design 2h, 3h, 3i): a full-window layer with a top bar (search, Grid/Filmstrip, Done), the grid
// sectioned by group, or the filmstrip of live panes. Both read one order (overview-model.ts) and one cursor.
import { useEffect, useMemo, useRef, useState, type KeyboardEvent, type ReactNode, type RefObject } from 'react';
import { Button, Icon, SectionHeading, Segmented, Text, TextInput, type MenuEntry } from '../../components/ui';
import { useDropTarget } from '../../components/ui/usePointerDrag';
import { isMac } from '../../design/keyboard';
import type { TabSummary } from '../conversation/tabSummary';
import type { Tab, TabGroup } from './model';
import { cardText, kindLabel, OverviewCard } from './OverviewCard';
import { OverviewFilmstrip } from './OverviewFilmstrip';
import { moveCursor, overviewOrder, overviewSections, searchPlaceholder, sectionPosition } from './overview-model';
import { useOverviewModelNames } from './useOverviewModelNames';
import './overview.css';

type View = 'grid' | 'film';
const viewKey = 'codeaf.desktop.overview.view';
const views = [{ value: 'grid', label: 'Grid' }, { value: 'film', label: 'Filmstrip' }] as const;
const readView = (): View => { try { return localStorage.getItem(viewKey) === 'film' ? 'film' : 'grid'; } catch { return 'grid'; } };

/** One overview section. Pinned has no drop target. A group's heading takes a tab into that group; "Other tabs" takes it out. */
function OverviewSectionBlock({ title, id, kind, count, target, splittable, onMoveGroup, onSplitGroup, onClose, children }: {
  title: string; id: string; kind: string; count: number; target: string | null | undefined; splittable: number;
  onMoveGroup: (tabId: string, groupId?: string) => void; onSplitGroup: (groupId: string) => void; onClose: () => void; children: ReactNode;
}) {
  const el = useRef<HTMLElement | null>(null);
  const dropRef = useDropTarget(target === undefined ? null : {
    kind: 'overview-section',
    id,
    accepts: (payload) => payload.kind === 'tab',
    hover: () => { if (el.current) el.current.dataset.drop = 'section'; },
    leave: () => { if (el.current) delete el.current.dataset.drop; },
    drop: (_point, payload) => {
      if (el.current) delete el.current.dataset.drop;
      if (payload.kind !== 'tab') return false;
      onMoveGroup(payload.id, target ?? undefined);
      return true;
    },
  });
  return <section ref={node => { el.current = node; dropRef(node); }} className="overview-section" aria-label={title}>
    <div className="overview-section-head">
      <SectionHeading className="overview-section-title">{title}</SectionHeading><span className="overview-section-count">{count}</span>
      {kind === 'group' && <Button className="overview-split" disabled={splittable < 2} onClick={() => { onSplitGroup(id); onClose(); }}><Icon name="grid" size="micro"/>Open as split</Button>}
    </div>
    <div className="overview-grid">{children}</div>
  </section>;
}

type Props = {
  summaries: Readonly<Record<string, TabSummary>>; now: number; open: boolean; tabs: readonly Tab[]; groups: readonly TabGroup[]; activeId: string;
  onClose: () => void; onSelect: (id: string) => void;
  /** Regroups a tab; `beside` is the card it was dropped on (before it, or `after` it). */
  onMoveGroup: (id: string, groupId?: string, beside?: { id: string; after?: boolean }) => void;
  onCloseTab?: (id: string) => void; onSplitGroup: (groupId: string) => void;
  /** A card's right-click menu: the strip's own tab menu (hosts/menuHost), so the two can never differ. Absent for a tab with no menu. */
  menuFor?: (tab: Tab) => MenuEntry[] | undefined;
  returnFocus?: RefObject<HTMLElement | null>;
};

const isPrimary = (event: Pick<KeyboardEvent, 'metaKey' | 'ctrlKey'>) => (isMac ? event.metaKey && !event.ctrlKey : event.ctrlKey && !event.metaKey);

/** The nearest card in the next row above or below, by horizontal distance. */
function verticalTarget(root: HTMLElement | null, id: string | undefined, direction: 1 | -1): string | undefined {
  const cards = [...(root?.querySelectorAll<HTMLElement>('.overview-card') ?? [])];
  const current = cards.find(card => card.dataset.cardId === id)?.getBoundingClientRect();
  if (!current) return undefined;
  const beyond = cards.filter(card => (card.getBoundingClientRect().top - current.top) * direction > 1);
  if (!beyond.length) return undefined;
  const nextTop = (direction === 1 ? Math.min : Math.max)(...beyond.map(card => card.getBoundingClientRect().top));
  const row = beyond.filter(card => Math.abs(card.getBoundingClientRect().top - nextTop) < 1);
  return row.reduce((best, card) => (Math.abs(card.getBoundingClientRect().left - current.left) < Math.abs(best.getBoundingClientRect().left - current.left) ? card : best)).dataset.cardId;
}

export function TabOverview({ summaries, now, open, tabs, groups, activeId, onClose, onSelect, onMoveGroup, onCloseTab, onSplitGroup, menuFor, returnFocus }: Props) {
  const modelNames = useOverviewModelNames(open);
  const dialog = useRef<HTMLDialogElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const body = useRef<HTMLDivElement>(null);
  const previousFocus = useRef<HTMLElement | null>(null);
  const [query, setQuery] = useState('');
  const [view, setView] = useState<View>(readView);
  const [cursor, setCursor] = useState<string>();
  useEffect(() => {
    if (open && !dialog.current?.open) {
      previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setCursor(activeId);
      dialog.current?.showModal();
      search.current?.focus();
    } else if (!open) {
      dialog.current?.close();
      setQuery('');
    }
  }, [open]);

  const sections = useMemo(() => overviewSections(tabs, groups, query, tab => `${tab.title} ${tab.draft} ${kindLabel(tab)} ${cardText(tab, summaries)} ${groups.find(group => group.id === tab.groupId)?.title ?? ''}`), [tabs, groups, query, summaries]);
  const ordered = useMemo(() => overviewOrder(sections), [sections]);
  const ids = ordered.map(tab => tab.id);
  const cursorId = cursor && ids.includes(cursor) ? cursor : ids.includes(activeId) ? activeId : ids[0];
  useEffect(() => {
    if (view === 'grid') body.current?.querySelector('[data-cursor="true"]')?.scrollIntoView({ block: 'nearest', behavior: 'instant' });
  }, [cursorId, view]);

  const changeView = (next: View) => { setView(next); try { localStorage.setItem(viewKey, next); } catch { /* The choice is a convenience; it must never block the overview. */ } };
  const openTab = (id: string) => { onSelect(id); onClose(); };
  const move = (id: string | undefined, focusCard: boolean) => {
    if (!id) return;
    setCursor(id);
    if (focusCard) requestAnimationFrame(() => dialog.current?.querySelector<HTMLElement>(`[data-card-id="${CSS.escape(id)}"] :is(.overview-card-open, .overview-film-open)`)?.focus());
  };
  function onKeyDown(event: KeyboardEvent<HTMLDialogElement>) {
    if (isPrimary(event) && event.key.toLowerCase() === 'w' && !event.shiftKey && !event.altKey) {
      event.preventDefault();
      if (onCloseTab && cursorId) { move(ids.length > 1 ? moveCursor(ids, cursorId, 1) : undefined, false); onCloseTab(cursorId); }
      return;
    }
    const target = event.target as HTMLElement;
    const typing = target === search.current;
    if (!typing && !target.matches('dialog, .overview-card-open, .overview-film-open')) return;
    if (event.metaKey || event.ctrlKey || event.altKey || event.shiftKey) return;
    const film = view === 'film';
    const horizontal = event.key === 'ArrowRight' ? 1 : event.key === 'ArrowLeft' ? -1 : 0;
    if (horizontal && (!typing || film || !query)) { event.preventDefault(); move(moveCursor(ids, cursorId, horizontal), !typing); return; }
    if (!film && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
      event.preventDefault();
      move(verticalTarget(body.current, cursorId, event.key === 'ArrowDown' ? 1 : -1), !typing);
    } else if (event.key === 'Enter' && typing && cursorId) { event.preventDefault(); openTab(cursorId); }
  }

  return <dialog ref={dialog} className="tab-overview" data-view={view} aria-label="All tabs overview" onCancel={onClose} onKeyDown={onKeyDown} onClose={() => {
    onClose();
    // Safari pointer clicks do not focus buttons, so the owner supplies the reliable return target.
    const target = returnFocus?.current ?? previousFocus.current;
    if (target?.isConnected) target.focus();
  }}>
    <div className="overview-bar" data-tauri-drag-region>
      {isMac && <span className="overview-lights" aria-hidden="true"/>}
      <label className="overview-search"><Icon name="search" size="sm"/><TextInput ref={search} aria-label="Filter tabs" placeholder={searchPlaceholder(tabs.length)} value={query} onChange={event => setQuery(event.target.value)}/></label>
      {/* A pointer click hands the keyboard back to the search field, so the arrows then move the cursor; Space and Enter (detail 0) stay on the switch. */}
      <div className="overview-view" onClick={event => { if (event.detail > 0) search.current?.focus(); }}><Segmented label="Overview view" options={[...views]} value={view} onChange={changeView}/></div>
      <Button className="overview-done" onClick={onClose}>Done</Button>
    </div>
    {view === 'grid'
      ? <div ref={body} className="overview-body">
        {open && sections.map(section => {
          const splittable = section.tabs.filter(tab => !tab.split && !tab.pinned).length;
          // Dropping a card on a group's section regroups it; "Other tabs" ungroups it. Pinned tabs sit outside groups.
          const target = section.kind === 'pinned' ? undefined : section.kind === 'group' ? section.id : null;
          return <OverviewSectionBlock key={section.id} title={section.title} id={section.id} kind={section.kind} count={section.tabs.length} target={target} splittable={splittable} onMoveGroup={onMoveGroup} onSplitGroup={onSplitGroup} onClose={onClose}>
            {section.tabs.map(tab => <OverviewCard key={tab.id} modelNames={modelNames} tab={tab} summaries={summaries} now={now} active={tab.id === activeId} cursor={tab.id === cursorId} menu={menuFor?.(tab)} onDropTab={target === undefined ? undefined : (id, after) => onMoveGroup(id, target ?? undefined, { id: tab.id, after })} onOpen={() => openTab(tab.id)} onBackground={() => move(tab.id, false)} onClose={onCloseTab && (() => { move(moveCursor(ids, tab.id, 1), false); onCloseTab(tab.id); })}/>)}
          </OverviewSectionBlock>;
        })}
        {!ordered.length && <Text className="overview-no-results">No matching tabs</Text>}
      </div>
      : open && (ordered.length
        ? <OverviewFilmstrip tabs={ordered} summaries={summaries} activeId={activeId} cursorId={cursorId} position={sectionPosition(sections, cursorId)} onCursor={id => move(id, false)} onOpen={openTab}/>
        : <Text className="overview-no-results">No matching tabs</Text>)}
  </dialog>;
}
