import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react';
import { Button, DropdownMenu, Icon, IconButton } from '../../components/ui';
import design from '../../design/tokens.json';
import { useMediaQuery } from '../../design/useMediaQuery';
import { overviewShortcut, tabShortcuts } from '../../design/keyboard';
import { BackChip } from '../focus-history/BackChip';
import { Banner, type NextUpBannerItem } from '../nextup/Banner';
import { FramePill, framePillVisible } from '../nextup/FramePill';
import { QueuePopover, type QueueRow } from '../nextup/QueuePopover';
import type { TabsApi } from './context';
import { useStripAnnouncement } from './announce';
import { GroupCapsule, MemberSlot } from './GroupCapsule';
import { groupDragProps } from './hosts/dragHost';
import { overflowItems, withGroupMenu } from './hosts/menuHost';
import { openStripBackgroundMenu, StripMenu } from './hosts/stripMenu';
import { stateOfMark, TabItem, tabDomIds } from './TabItem';
import { focusedPane, stripItems, visibleTabs, type Tab } from './model';
import { departureIds, departuresFrom, exitHoldMs, groupsForDepartures, mergeDepartures, retainDepartures, withDepartures, type TabDeparture } from './tabExit';
import './strip.css';
import './tab-motion.css';

/**
 * The strip had already sized this slot. Pin that used width, then take it to zero so the CSS transition
 * runs. A keyword width (max-content) does not interpolate, so the start has to be the measured length.
 */
function releaseWidth(box: HTMLElement) {
  const width = box.getBoundingClientRect().width;
  box.style.width = `${width}px`;
  box.style.minWidth = `${width}px`;
  box.style.maxWidth = `${width}px`;
  void box.offsetWidth;
  box.style.width = '0px';
  box.style.minWidth = '0px';
  box.style.maxWidth = '0px';
}

/**
 * The walk listens for this. The strip does not import the walker: that module lands in its own file,
 * and a click here has to reach it without this file changing again.
 */
export const nextUpStartEvent = 'codeaf:next-up-start';

/** `itemKey` is the question to open. Absent, the walk starts at the front of the queue. */
export type NextUpStartDetail = { itemKey?: string };

/** Asks the walk to start. A window with no listener yet still shows the pill; the click is not lost as a navigation. */
export function requestNextUp(detail: NextUpStartDetail = {}) {
  window.dispatchEvent(new CustomEvent<NextUpStartDetail>(nextUpStartEvent, { detail }));
}

/** The queue hung on the right end of the strip. Zero draws nothing: the slot is absent, not an empty pill. */
export type StripFrame = {
  count: number;
  rows: readonly QueueRow[];
  acceptable: number;
  banner: NextUpBannerItem | null;
  onOpen: () => void;
  onJump: (key: string) => void;
  onStart: () => void;
  onAccept: () => void;
};

/** The return chip. `key` changes on each jump the person did not start, so a dismissed chip can appear again. */
export type StripBack = {
  key: string;
  parentName: string;
  onBack: () => void;
  onDismiss: () => void;
};

/**
 * The strip (46px): an optional back chip, then pinned tabs, a hairline, tabs and group capsules, "+" and
 * the overview grid, then the drag spacer's empty room, then the frame pill. The pill is outside the
 * scrolling tabs and outside that spacer, so window drag stays on the empty room and the pill stays a
 * control. Tabs compress from 190px to 112px, then the strip scrolls under a 40px mask at its right
 * edge and a "+N" menu lists every tab. At the small breakpoint an inactive tab that still has a title
 * becomes the 44px chip instead; the active tab keeps its title and the 112px floor. No scrollbar, no
 * chevron buttons, no wheel hijacking.
 */
export function TabStrip({ api, leading, back, frame, overviewTrigger, onOverview }: { api: TabsApi; leading?: ReactNode; back?: StripBack; frame?: StripFrame; overviewTrigger: RefObject<HTMLButtonElement | null>; onOverview: () => void }) {
  const { state, dispatch } = api;
  const strip = useRef<HTMLDivElement>(null);
  // Shell 3j "Compressed", and the open question that makes it live: only while the window is at the small breakpoint.
  const narrow = useMediaQuery(`(max-width: ${design.breakpoints.small}px)`);
  const reduced = useMediaQuery('(prefers-reduced-motion: reduce)');
  const [edge, setEdge] = useState({ end: false, hidden: 0 });
  const [moveNote, setMoveNote] = useStripAnnouncement(strip);
  const [departures, setDepartures] = useState<TabDeparture[]>([]);
  const order = visibleTabs(state);
  // Closing tabs stay mounted for dur-base. The comparison is the id list: a draft keystroke must not restart a collapse.
  const signature = order.map(tab => tab.id).join('\0');
  const [seen, setSeen] = useState(signature);
  // Previous strip, updated after commit. A render (including a strict-mode replay) must keep seeing the same
  // "before", or the second pass thinks nothing left and the collapse never starts.
  const orderRef = useRef(order);
  const groupsRef = useRef(state.groups);
  const focusNeighbour = useRef(false);
  if (seen !== signature) {
    const gone = departuresFrom(orderRef.current, order, groupsRef.current);
    if (gone.length > 0) focusNeighbour.current = true;
    const next = mergeDepartures(departures, retainDepartures(reduced, gone), new Set(order.map(tab => tab.id)));
    setSeen(signature);
    if (departureIds(next) !== departureIds(departures)) setDepartures(next);
  }
  const shown = withDepartures(order, departures);
  const groups = groupsForDepartures(state.groups, departures);
  const departing = new Set(departures.map(item => item.tab.id));
  // The strip draws `state.tabs` as it stands (the reducer keeps pinned tabs first and each group one run), so what a
  // person sees, what the arrow keys walk and what ⌘1–9 count are one order. A departing tab is put back only so its
  // slot can shrink; it is not in that order.
  const pinned = shown.filter(tab => tab.pinned);
  const items = stripItems({ tabs: shown, groups });
  // The strip only re-measures when its shape changes, never on a draft keystroke.
  const shape = JSON.stringify([state.activeId, state.groups, departures.map(item => item.tab.id), state.tabs.map(t => [t.id, t.title, t.pinned, t.groupId, t.split?.panes.map(p => [p.id, p.title])])]);
  // A closing tab is inert and hidden from assistive tech for the whole collapse. The ref pins its live width, then
  // lets the transition take that width to zero. Doing it here, not in state, keeps a second render from skipping the start.
  const release = useCallback((el: HTMLDivElement | null) => {
    if (!el || el.dataset.measured) return;
    el.dataset.measured = 'true';
    const parent = el.parentElement;
    const slot = parent?.classList.contains('workspace-group-tab-slot') ? parent : null;
    releaseWidth(el);
    if (slot) releaseWidth(slot);
    el.dataset.collapsed = 'true';
  }, []);
  const item = (tab: Tab, inGroup = false) => departing.has(tab.id)
    // A departure's parent changes, so this copy remounts inside the collapsing slot. A tab that stays does not.
    ? <div key={tab.id} ref={release} className="workspace-tab-exit" data-exit-id={tab.id} inert aria-hidden="true"><TabItem api={api} tab={tab} order={order} inGroup={inGroup} narrow={narrow}/></div>
    : <TabItem key={tab.id} api={api} tab={tab} order={order} inGroup={inGroup} narrow={narrow}/>;

  useEffect(() => {
    const viewport = strip.current;
    if (!viewport) return;
    const tolerance = parseFloat(design.foundation['border-width']);
    const measure = () => {
      const bounds = viewport.getBoundingClientRect();
      const end = viewport.scrollWidth - viewport.clientWidth - viewport.scrollLeft > tolerance;
      // A tab counts as hidden when any part of it lies outside the strip's visible box.
      const hidden = Array.from(viewport.querySelectorAll<HTMLElement>('[role="tab"]')).filter(el => {
        if (el.closest('[inert]')) return false;
        const box = el.getBoundingClientRect();
        return box.left < bounds.left - tolerance || box.right > bounds.right + tolerance;
      }).length;
      setEdge(previous => (previous.end === end && previous.hidden === hidden ? previous : { end, hidden }));
    };
    const observer = new ResizeObserver(measure);
    observer.observe(viewport);
    Array.from(viewport.children).forEach(child => observer.observe(child));
    viewport.addEventListener('scroll', measure, { passive: true });
    measure();
    return () => { observer.disconnect(); viewport.removeEventListener('scroll', measure); };
  }, [shape]);

  useEffect(() => {
    if (reduced) {
      setDepartures(current => (current.length ? [] : current));
      return;
    }
    if (!departures.length) return;
    // The token, not a copied number: reduced motion sets dur-base to zero and this hold ends on that same frame.
    const hold = exitHoldMs(getComputedStyle(document.documentElement).getPropertyValue('--dur-base'));
    const ids = new Set(departures.map(item => item.tab.id));
    const timer = window.setTimeout(() => setDepartures(current => current.filter(item => !ids.has(item.tab.id))), hold);
    return () => window.clearTimeout(timer);
  }, [departures, reduced]);

  useLayoutEffect(() => {
    orderRef.current = order;
    groupsRef.current = state.groups;
  });

  useLayoutEffect(() => {
    if (!focusNeighbour.current) return;
    focusNeighbour.current = false;
    const root = strip.current;
    if (!root) return;
    const active = document.activeElement;
    const inExit = !!active?.closest('.workspace-tab-exit');
    const nowhere = !active || active === document.body || active === document.documentElement;
    // The close already selected the neighbour. Move focus there before paint, not after the width finishes.
    if (inExit || nowhere) root.querySelector<HTMLElement>('[role="tab"][aria-selected="true"]')?.focus();
  });

  useEffect(() => {
    const selected = strip.current?.querySelector<HTMLElement>('[aria-selected="true"]');
    selected?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' });
    const frame = requestAnimationFrame(() => selected?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' }));
    return () => cancelAnimationFrame(frame);
  }, [state.activeId, state.groups]);

  return (
    <div className="workspace-tabbar" data-tauri-drag-region onContextMenu={openStripBackgroundMenu}>
      {leading}
      {back && <div className="workspace-back-slot"><BackChip key={back.key} parentName={back.parentName} onBack={back.onBack} onDismiss={back.onDismiss}/></div>}
      <div className="workspace-tablist-owner" role="tablist" aria-label="Conversation tabs" aria-owns={order.flatMap(tabDomIds).join(" ")}/>
      <div ref={strip} className="workspace-tabstrip" aria-label="Conversation tabs" data-fade-end={edge.end || undefined}>
        {pinned.map(tab => item(tab))}
        {pinned.length > 0 && <span className="workspace-tab-divider" role="separator" aria-orientation="vertical"/>}
        {items.map(entry => {
          if (entry.kind === 'tab') return item(entry.tab);
          const { group, members } = entry;
          return (
            <GroupCapsule key={group.id} title={group.title} count={members.length} collapsed={group.collapsed} needsYou={members.some(t => stateOfMark(api.summaries[focusedPane(t).id]?.mark) === 'waiting')}
              onToggle={() => dispatch({ type: 'collapse-group', id: group.id })} labelProps={groupDragProps(api, group, setMoveNote)} wrapLabel={label => withGroupMenu(api, group, label)}>
              {members.map(tab => <MemberSlot key={tab.id} hidden={group.collapsed && tab.id !== state.activeId && !departing.has(tab.id)}>{item(tab, !group.collapsed)}</MemberSlot>)}
            </GroupCapsule>
          );
        })}
      </div>
      <span className="workspace-strip-note" role="status" aria-live="polite" aria-atomic="true"><span key={moveNote.revision}>{moveNote.text}</span></span>
      <div className="workspace-tab-actions">
        <IconButton className="workspace-tab-action" label="New tab" title={`New tab (${tabShortcuts.new})`} icon="plus" iconSize="sm" onClick={() => dispatch({ type: 'new' })}/>
        {edge.hidden > 0 && <DropdownMenu label="Tab actions" items={overflowItems(api)}><Button className="workspace-tab-more" aria-label="Tab actions">+{edge.hidden}<Icon name="chevron" size="micro" motion="disclosure"/></Button></DropdownMenu>}
        <StripMenu api={api} onOverview={onOverview}/>
        <IconButton ref={overviewTrigger} className="workspace-tab-action workspace-overview-trigger" label="All tabs" title={`All tabs (${overviewShortcut})`} icon="grid" iconSize="sm" onClick={onOverview}/>
      </div>
      {frame && framePillVisible(frame.count) && <div className="workspace-frame-slot">
        {/* The banner is first so the popover, later at the same layer, paints over it while both are open. */}
        <div className="workspace-frame-banner"><Banner item={frame.banner} onOpen={frame.onJump}/></div>
        <QueuePopover rows={frame.rows} acceptable={frame.acceptable} onJump={frame.onJump} onStart={frame.onStart} onAccept={frame.onAccept}>
          <FramePill count={frame.count} onOpen={frame.onOpen}/>
        </QueuePopover>
      </div>}
    </div>
  );
}
