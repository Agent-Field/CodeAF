import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ComponentProps, type ReactNode, type RefObject } from 'react';
import { Button, DropdownMenu, Icon, IconButton } from '../../components/ui';
import { Switcher } from '../places/rail/Switcher';
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
import { useGroupDrag } from './hosts/dragHost';
import { overflowItems, withGroupMenu } from './hosts/menuHost';
import { openStripBackgroundMenu, StripMenu } from './hosts/stripMenu';
import { navigate, stateOfMark, TabItem, tabDomId, tabDomIds } from './TabItem';
import { focusedPane, stripItems, visibleTabs, type Tab, type TabGroup } from './model';
import { departureIds, departuresFrom, exitHoldMs, groupsForDepartures, mergeDepartures, retainDepartures, withDepartures, type TabDeparture } from './tabExit';
import { HomeTab } from '../places/home/HomeTab';
import { isPlaceHome } from './reducers/home';
import { useLongPress } from '../shell/useLongPress';
import '../shell/touch.css';
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

/** One group capsule. The hook has to live in a component, not in the strip's map. */
function GroupBlock({ api, group, announce, children, ...capsule }: {
  api: TabsApi; group: TabGroup; announce?: (message: string) => void; children: ReactNode;
} & Omit<ComponentProps<typeof GroupCapsule>, 'labelProps' | 'children'>) {
  const labelProps = useGroupDrag(api, group, announce);
  return <GroupCapsule {...capsule} labelProps={labelProps}>{children}</GroupCapsule>;
}

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
  // A finger hold on a tab or a group label opens the menu that control already has. No prop on TabItem or the capsule: the listener finds them.
  useLongPress();
  const { state, dispatch } = api;
  const strip = useRef<HTMLDivElement>(null);
  const homeFocus = useRef<string | null>(null);
  // Shell 3j "Compressed", and the open question that makes it live: only while the window is at the small breakpoint.
  const narrow = useMediaQuery(`(max-width: ${design.breakpoints.small}px)`);
  const reduced = useMediaQuery('(prefers-reduced-motion: reduce)');
  const [edge, setEdge] = useState({ end: false, hidden: 0 });
  const [moveNote, setMoveNote] = useStripAnnouncement(strip);
  const [departures, setDepartures] = useState<TabDeparture[]>([]);
  const order = visibleTabs(state);
  // Closing compares the full tab list, not the visible one. A collapsed group's hidden members leave the
  // visible order without leaving the document; treating that as a close dropped the pill whenever the
  // selected tab stood outside the group. A draft keystroke does not change this id list, so it does not
  // restart a collapse.
  const signature = state.tabs.map(tab => tab.id).join('\0');
  const [seen, setSeen] = useState(signature);
  // Previous full list, updated after commit. A render (including a strict-mode replay) must keep seeing the
  // same "before", or the second pass thinks nothing left and the collapse never starts.
  const tabsRef = useRef(state.tabs);
  const groupsRef = useRef(state.groups);
  const focusNeighbour = useRef(false);
  if (seen !== signature) {
    const gone = departuresFrom(tabsRef.current, state.tabs, groupsRef.current);
    // Only a close hands focus to an existing neighbour. A tab replaced by a new one (Continue from History) selects a tab the strip
    // never held, whose pane takes the keyboard itself; focusing its tab button here would make the composer stand aside.
    if (gone.length > 0 && tabsRef.current.some(tab => tab.id === state.activeId)) focusNeighbour.current = true;
    const next = mergeDepartures(departures, retainDepartures(reduced, gone), new Set(state.tabs.map(tab => tab.id)));
    setSeen(signature);
    if (departureIds(next) !== departureIds(departures)) setDepartures(next);
  }
  // Every tab is drawn, hidden members included, so a collapsed group whose members are all hidden
  // still shows its capsule. Those members stay in the drawing model after the close motion ends, even
  // when the active tab is outside the group.
  const shown = withDepartures(state.tabs, departures);
  const groups = groupsForDepartures(state.groups, departures);
  const departing = new Set(departures.map(item => item.tab.id));
  // Arrow keys and ⌘1–9 walk `order` (hidden members of a collapsed group are not stops). The drawn strip
  // still includes every member, so the pill "Label N" stays when none of those tabs is selected. A
  // departing tab is put back only so its slot can shrink; it is not in that keyboard order.
  const home = shown.find(isPlaceHome);
  const pinned = shown.filter(tab => tab.pinned && !isPlaceHome(tab));
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
      // The mask is on whenever the strip overflows, not only while tabs remain past the right edge: revealing the
      // newest tab scrolls to the end, and the strip must still read as one that scrolls under a mask.
      const end = viewport.scrollWidth - viewport.clientWidth > tolerance;
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
    tabsRef.current = state.tabs;
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
    // The selected control is the title button. The chip around it also holds the close slot, and
    // scrolling the button leaves that slot past the strip, so the active tab is not fully in view.
    const chip = selected?.closest<HTMLElement>('.workspace-tab') ?? selected;
    const reveal = () => chip?.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: 'instant' });
    reveal();
    const frame = requestAnimationFrame(reveal);
    return () => cancelAnimationFrame(frame);
  }, [state.activeId, state.groups]);

  return (
    <div className="workspace-tabbar" data-tauri-drag-region onContextMenu={openStripBackgroundMenu}>
      {leading}
      {back && <div className="workspace-back-slot"><BackChip key={back.key} parentName={back.parentName} onBack={back.onBack} onDismiss={back.onDismiss}/></div>}
      <div className="workspace-tablist-owner" role="tablist" aria-label="Conversation tabs" aria-owns={order.flatMap(tabDomIds).join(" ")}/>
      <div ref={strip} className="workspace-tabstrip" aria-label="Conversation tabs" data-fade-end={edge.end || undefined}>
        {home && <HomeTab key={home.id} name={home.title} tint={api.placeTint ?? 'graphite'} active={state.activeId === home.id} id={tabDomId(home)} menu={api.placeMenu}
          onSelect={() => dispatch({ type: 'select', id: home.id })} onKeyDown={navigate(api, order, home, homeFocus)}
          switcher={!!api.placeSwitcher} needsYou={api.placeSwitcher?.alert}
          wrapSelect={api.placeSwitcher ? select => <Switcher items={api.placeSwitcher!.items}>{select}</Switcher> : undefined}/>}
        {pinned.map(tab => item(tab))}
        {pinned.length > 0 && <span className="workspace-tab-divider" role="separator" aria-orientation="vertical"/>}
        {items.map(entry => {
          if (entry.kind === 'tab') return item(entry.tab);
          const { group, members } = entry;
          return (
            <GroupBlock key={group.id} api={api} group={group} announce={setMoveNote} title={group.title} count={members.length} collapsed={group.collapsed} needsYou={members.some(t => stateOfMark(api.summaries[focusedPane(t).id]?.mark) === 'waiting')}
              onToggle={() => dispatch({ type: 'collapse-group', id: group.id })} wrapLabel={label => withGroupMenu(api, group, label)}>
              {members.map(tab => <MemberSlot key={tab.id} hidden={group.collapsed && tab.id !== state.activeId && !departing.has(tab.id)}>{item(tab, !group.collapsed)}</MemberSlot>)}
            </GroupBlock>
          );
        })}
      </div>
      {/* The node stays mounted so a repeated move replaces the text (the key) and is read again. Until there are words it stays out of the accessibility tree, so an empty announcement is not a second status on the page. */}
      <span className="workspace-strip-note" role="status" aria-live="polite" aria-atomic="true" aria-hidden={moveNote.text ? undefined : true}><span key={moveNote.revision}>{moveNote.text}</span></span>
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
