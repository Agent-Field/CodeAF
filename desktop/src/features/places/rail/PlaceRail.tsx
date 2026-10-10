import { useState, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react';
import { Button, ContextMenu, type MenuEntry } from '../../../components/ui';
import { useDropTarget, usePointerDrag, type DragPayload } from '../../../components/ui/usePointerDrag';
import { choosableTints, tintLabel, type TintName } from '../components/PlaceSwatch';
import type { PlaceRowModel } from '../shell/contracts';
import { dropPayloadFromPointer } from '../place-actions';
import type { RailSections } from '../shell/selectors';
import { NowRow } from '../../shell/NowRow';
import { railShortcutVisible, shownPlaces, stepRail, type RailKey } from './railFocus';
import { RailRow } from './RailRow';
import './rail-sections.css';

/** What a rail row can ask for. Each is wired by the shell; one that is absent has no menu entry and no gesture. */
export type PlaceRailActions = {
  go: (id: string) => void;
  newWindow?: (id: string) => void;
  quickLook?: (id: string) => void;
  pin?: (id: string, index?: number) => void;
  unpin?: (id: string) => void;
  rename?: (id: string) => void;
  setTint?: (id: string, tint: TintName) => void;
  close: (id: string) => void;
  closeAll: () => void;
  closeOthers: (id: string) => void;
  /** Chats dropped on a row are added to that place (Places 6e). */
  fileChats?: (chatIds: readonly string[], placeId: string) => void;
};

export type RailNow = {
  active: boolean;
  /** Unplaced chats (Places 8e, the "Not in any place" count). Zero draws nothing. */
  count?: number;
  status?: 'waiting' | 'failed';
  statusLabel?: string;
  shortcut: string;
  onGo: () => void;
  onNewWindow?: () => void;
  hover?: boolean;
};

export type PlaceRailSectionsProps = {
  now: RailNow;
  /** Pinned and Open; absent while the engine has no Places to read. */
  sections?: RailSections;
  /** The place this window shows (its row reads as the open one). */
  current?: string;
  /** True when the graph has places but none is pinned or open: the quiet line of 10a "Empty". */
  emptyHint?: boolean;
  /**
   * Non-archived places from a completed read. Undefined until that read.
   * Zero hides the All places shortcut (Places 8e); the row itself stays.
   */
  livePlaces?: number;
  notice?: { text: string; onRetry?: () => void };
  allPlaces?: { active: boolean; shortcut: string; onOpen: () => void; onOpenInNewTab?: () => void };
  actions: PlaceRailActions;
  tabCount?: (id: string) => number;
  slotShortcut: (index: number) => string;
  closeShortcut: string;
  newWindowShortcut: string;
  /** App pages and the theme, drawn under All places. */
  foot?: ReactNode;
};

const NOW = 'now';
const ALL = 'all';
const primaryClick = (event: MouseEvent) => event.metaKey || event.ctrlKey;

type RailTarget = { section: 'pinned' | 'open'; index: number; placeId?: string };

/** A place can land in Pinned when pinning exists, or in Open when unpinning does. A chat lands only on a row that can file it. */
function railAccepts(payload: DragPayload, target: RailTarget, actions: PlaceRailActions): boolean {
  const body = dropPayloadFromPointer(payload);
  if (!body) return false;
  if (body.kind === 'place') return target.section === 'pinned' ? !!actions.pin : !!actions.unpin;
  return !!target.placeId && !!actions.fileChats;
}

function railCommit(payload: DragPayload, target: RailTarget, actions: PlaceRailActions, pinnedIds: readonly string[]): boolean {
  const body = dropPayloadFromPointer(payload);
  if (!body || !railAccepts(payload, target, actions)) return false;
  if (body.kind === 'chat') {
    if (target.placeId) actions.fileChats?.(body.ids, target.placeId);
    return true;
  }
  for (const id of body.ids) {
    if (target.section === 'pinned') actions.pin?.(id, target.index);
    else if (pinnedIds.includes(id)) actions.unpin?.(id);
  }
  return true;
}

/** The empty end of a section. Dropping here appends. A row inside is a closer target and wins when it accepts. */
function RailSectionRows({ section, index, actions, pinnedIds, setOver, children }: {
  section: 'pinned' | 'open'; index: number; actions: PlaceRailActions; pinnedIds: readonly string[];
  setOver: (key: string | undefined) => void; children: ReactNode;
}) {
  const target: RailTarget = { section, index };
  const dropRef = useDropTarget({
    kind: 'rail-row',
    id: `${section}:end`,
    accepts: (payload) => railAccepts(payload, target, actions),
    hover: () => setOver(`${section}:${index}`),
    leave: () => setOver(undefined),
    drop: (_point, payload) => { setOver(undefined); return railCommit(payload, target, actions, pinnedIds); },
  });
  return <div className="rail-rows" ref={dropRef}>{children}</div>;
}

/** One place row. The pointer starts on the row, so a press on its button still picks the place up. */
function RailPlace({ place, section, index, actions, pinnedIds, over, setOver, menuItems, slot, slotShortcut, closeShortcut, current, tabCount, focus, onClick, onAuxClick }: {
  place: PlaceRowModel; section: 'pinned' | 'open'; index: number; actions: PlaceRailActions; pinnedIds: readonly string[];
  over?: string; setOver: (key: string | undefined) => void; menuItems: MenuEntry[]; slot?: number; slotShortcut: (index: number) => string;
  closeShortcut: string; current?: string; tabCount?: (id: string) => number;
  focus: { 'data-rail-item': string; tabIndex: number; onFocus: () => void };
  onClick: (event: MouseEvent<HTMLButtonElement>) => void;
  onAuxClick: (event: MouseEvent<HTMLButtonElement>) => void;
}) {
  const target: RailTarget = { section, index, placeId: place.id };
  const pointer = usePointerDrag(actions.pin ? { payload: { kind: 'place', id: place.id, ids: [place.id] } } : null);
  const dropRef = useDropTarget({
    kind: 'rail-row',
    id: `${section}:${index}:${place.id}`,
    accepts: (payload) => railAccepts(payload, target, actions),
    hover: () => setOver(`${section}:${index}`),
    leave: () => setOver(undefined),
    drop: (_point, payload) => { setOver(undefined); return railCommit(payload, target, actions, pinnedIds); },
  });
  return <ContextMenu label={`${place.name} actions`} items={menuItems}>
    <RailRow name={place.name} parentName={place.parentName} tint={place.tint} active={place.id === current}
      status={place.status} statusLabel={place.statusLabel} closedButBusy={place.closedButBusy}
      close={section === 'open' ? { onClose: () => actions.close(place.id), tabs: tabCount?.(place.id), shortcut: place.id === current ? closeShortcut : undefined } : undefined}
      containerProps={{ ref: dropRef, 'data-drop': over === `${section}:${index}` || undefined, ...pointer }}
      aria-keyshortcuts={slot ? slotShortcut(slot).replace('⌃', 'Control+').replace('Alt ', 'Alt+') : undefined}
      {...focus} onClick={onClick} onAuxClick={onAuxClick}/>
  </ContextMenu>;
}

function Section({ label, action, children }: { label: string; action?: ReactNode; children: ReactNode }) {
  return <section className="rail-group" aria-label={label}>
    <div className="rail-section-label"><span>{label}</span>{action}</div>
    {children}
  </section>;
}

/**
 * The rail's place list (Places 6a, 10a, 8e). Order is Now, then Pinned, then Open, then All places at the foot.
 * Headers exist only while that section has a row. Iteration 2 I2.1 removed the Inbox row: questions elsewhere
 * are the frame pill, so this list never draws one. Archived places are left out entirely.
 */
export function PlaceRailSections(props: PlaceRailSectionsProps) {
  const { now, sections, current, emptyHint, livePlaces, notice, allPlaces, actions, tabCount, slotShortcut, closeShortcut, newWindowShortcut, foot } = props;
  const [over, setOver] = useState<string>();
  const [cursor, setCursor] = useState<string>();
  const pinned = shownPlaces(sections?.pinned ?? []);
  const open = shownPlaces(sections?.open ?? []);
  const pinnedIds = pinned.map(place => place.id);
  const ids = [NOW, ...pinnedIds, ...open.map(place => place.id), ...(allPlaces ? [ALL] : [])];
  const activeId = now.active ? NOW : current && ids.includes(current) ? current : allPlaces?.active ? ALL : NOW;
  const stop = cursor && ids.includes(cursor) ? cursor : activeId;
  const slotOf = (id: string) => { const order = [...pinnedIds, ...open.map(place => place.id)]; const at = order.indexOf(id); return at >= 0 && at < 9 ? at + 1 : undefined; };

  function menu(place: PlaceRowModel, section: 'pinned' | 'open'): MenuEntry[] {
    const groups: MenuEntry[][] = [[], [], [], []];
    groups[0].push({ id: 'go', label: 'Go to', shortcut: '↵', onSelect: () => actions.go(place.id) });
    if (actions.quickLook) groups[0].push({ id: 'quick-look', label: 'Quick Look', onSelect: () => actions.quickLook?.(place.id) });
    if (actions.newWindow) groups[0].push({ id: 'new-window', label: 'Open in new window', shortcut: newWindowShortcut, onSelect: () => actions.newWindow?.(place.id) });
    if (section === 'pinned' && actions.unpin) {
      groups[1].push({ id: 'unpin', label: 'Unpin', onSelect: () => actions.unpin?.(place.id) });
      const at = pinnedIds.indexOf(place.id);
      if (actions.pin && at > 0) groups[1].push({ id: 'up', label: 'Move up', onSelect: () => actions.pin?.(place.id, at - 1) });
      if (actions.pin && at < pinnedIds.length - 1) groups[1].push({ id: 'down', label: 'Move down', onSelect: () => actions.pin?.(place.id, at + 1) });
    } else if (actions.pin) groups[1].push({ id: 'pin', label: 'Pin', onSelect: () => actions.pin?.(place.id) });
    if (actions.rename) groups[1].push({ id: 'rename', label: 'Rename…', onSelect: () => actions.rename?.(place.id) });
    if (actions.setTint) groups[1].push({ kind: 'submenu', id: 'tint', label: 'Tint', items: choosableTints.map(tint => ({ id: `tint-${tint}`, label: tintLabel[tint], checked: tint === place.tint, onSelect: () => actions.setTint?.(place.id, tint) })) });
    groups[2].push({ id: 'close', label: 'Close', shortcut: place.id === current ? closeShortcut : undefined, onSelect: () => actions.close(place.id) });
    groups[2].push({ id: 'close-others', label: 'Close all others', disabled: !open.some(other => other.id !== place.id), onSelect: () => actions.closeOthers(place.id) });
    const entries: MenuEntry[] = [];
    groups.filter(group => group.length).forEach((group, index) => { if (index) entries.push({ kind: 'separator', id: `sep-${index}` }); entries.push(...group); });
    return entries;
  }

  const focusProps = (id: string) => ({ 'data-rail-item': id, tabIndex: id === stop ? 0 : -1, onFocus: () => setCursor(id) });

  function row(place: PlaceRowModel, section: 'pinned' | 'open', index: number) {
    return <RailPlace key={place.id} place={place} section={section} index={index} actions={actions} pinnedIds={pinnedIds} over={over} setOver={setOver}
      menuItems={menu(place, section)} slot={slotOf(place.id)} slotShortcut={slotShortcut} closeShortcut={closeShortcut} current={current} tabCount={tabCount}
      focus={focusProps(place.id)}
      onClick={event => (primaryClick(event) && actions.newWindow ? actions.newWindow(place.id) : actions.go(place.id))}
      onAuxClick={event => { if (event.button === 1 && actions.newWindow) { event.preventDefault(); actions.newWindow(place.id); } }}/>;
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key !== 'ArrowDown' && event.key !== 'ArrowUp') return;
    if (event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return;
    const items = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[data-rail-item]')];
    const index = items.findIndex(item => item === document.activeElement);
    if (index < 0) return;
    event.preventDefault();
    const next = stepRail(items.length, index, event.key as RailKey);
    items[next]?.focus();
  }

  const showShortcut = allPlaces && railShortcutVisible(livePlaces);
  return <div className="rail-sections" onKeyDown={onKeyDown}>
    <nav className="rail-nav" aria-label="Places">
      <div className="rail-group">
        <NowRow now={now} primaryClick={primaryClick} {...focusProps(NOW)}/>
      </div>
      {pinned.length > 0 && <Section label="Pinned"><RailSectionRows section="pinned" index={pinned.length} actions={actions} pinnedIds={pinnedIds} setOver={setOver}>{pinned.map((place, index) => row(place, 'pinned', index))}</RailSectionRows></Section>}
      {open.length > 0 && <Section label="Open" action={<Button variant="ghost" className="rail-section-action" onClick={actions.closeAll}>Close all</Button>}>
        <RailSectionRows section="open" index={open.length} actions={actions} pinnedIds={pinnedIds} setOver={setOver}>{open.map((place, index) => row(place, 'open', index))}</RailSectionRows>
      </Section>}
      {emptyHint && pinned.length === 0 && open.length === 0 && <p className="rail-hint">Places you open show here. Pin the ones you live in.</p>}
      {notice && <p className="rail-hint" role="status">{notice.text}{notice.onRetry && <> · <Button variant="ghost" className="rail-section-action" onClick={notice.onRetry}>Retry</Button></>}</p>}
    </nav>
    {(allPlaces || foot) && <div className="rail-foot">
      {allPlaces && <RailRow name="All places" icon="allPlaces" active={allPlaces.active} meta={showShortcut ? allPlaces.shortcut : undefined}
        {...focusProps(ALL)}
        onClick={event => (primaryClick(event) && allPlaces.onOpenInNewTab ? allPlaces.onOpenInNewTab() : allPlaces.onOpen())}
        onAuxClick={event => { if (event.button === 1 && allPlaces.onOpenInNewTab) { event.preventDefault(); allPlaces.onOpenInNewTab(); } }}/>}
      {foot}
    </div>}
  </div>;
}
