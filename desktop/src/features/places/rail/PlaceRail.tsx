import { useState, type DragEvent, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react';
import { Button, ContextMenu } from '../../../components/ui';
import type { TintName } from '../components/PlaceSwatch';
import type { PlaceRowModel } from '../shell/contracts';
import { chatDragType, placeDragType, writeDrag } from '../place-actions';
import { readPlaceDrag } from '../dnd/placeDnd';
import type { RailSections } from '../shell/selectors';
import { railMenu } from '../menus/railMenu';
import { NowRow } from '../../shell/NowRow';
import { railShortcutVisible, shownPlaces, stepRail, type RailKey } from './railFocus';
import { RailRow } from './RailRow';
import { keyboardMoveTarget, movedAnnouncement, pinnedDropIndex } from './railDnd';
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
  /** Settings, and the development Design system row, drawn under All places. */
  foot?: ReactNode;
};

const NOW = 'now';
const ALL = 'all';
const primaryClick = (event: MouseEvent) => event.metaKey || event.ctrlKey;

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
  const [dragging, setDragging] = useState<string>();
  const [dropKind, setDropKind] = useState<'place' | 'chat'>();
  const [cursor, setCursor] = useState<string>();
  const [announcement, setAnnouncement] = useState('');
  const pinned = shownPlaces(sections?.pinned ?? []);
  const open = shownPlaces(sections?.open ?? []);
  const pinnedIds = pinned.map(place => place.id);
  const ids = [NOW, ...pinnedIds, ...open.map(place => place.id), ...(allPlaces ? [ALL] : [])];
  const activeId = now.active ? NOW : current && ids.includes(current) ? current : allPlaces?.active ? ALL : NOW;
  const stop = cursor && ids.includes(cursor) ? cursor : activeId;
  const slotOf = (id: string) => { const order = [...pinnedIds, ...open.map(place => place.id)]; const at = order.indexOf(id); return at >= 0 && at < 9 ? at + 1 : undefined; };
  const indexOfPinned = (id: string) => pinnedIds.indexOf(id);

  function menu(place: PlaceRowModel, section: 'pinned' | 'open') {
    return railMenu({ id: place.id, tint: place.tint, pinned: section === 'pinned' }, {
      goTo: actions.go, quickLook: actions.quickLook, newWindow: actions.newWindow, newWindowShortcut,
      pin: actions.pin, unpin: actions.unpin, startRename: actions.rename, setTint: actions.setTint,
      close: actions.close, closeOthers: actions.closeOthers,
      closeShortcut: place.id === current ? closeShortcut : undefined,
      closeOthersDisabled: !open.some(other => other.id !== place.id),
      moveUp: actions.pin && indexOfPinned(place.id) > 0 ? id => actions.pin?.(id, indexOfPinned(id) - 1) : undefined,
      moveDown: actions.pin && indexOfPinned(place.id) < pinnedIds.length - 1 ? id => actions.pin?.(id, indexOfPinned(id) + 1) : undefined,
    });
  }

  // A place dragged within or into Pinned is pinned at that slot; one dragged into Open is unpinned. A chat dropped on a row is filed there.
  const dropProps = (target: { section: 'pinned' | 'open'; index: number; placeId?: string }) => ({
    onDragOver: (event: DragEvent) => {
      const types = [...event.dataTransfer.types];
      const place = types.includes(placeDragType) && (target.section === 'pinned' ? !!actions.pin : !!actions.unpin);
      const chat = types.includes(chatDragType) && !!target.placeId && !!actions.fileChats;
      if (!place && !chat) return;
      event.preventDefault();
      // A row's drag must not also light the section's end slot.
      event.stopPropagation();
      event.dataTransfer.dropEffect = chat ? 'copy' : 'move';
      setDropKind(chat ? 'chat' : 'place');
      setOver(`${target.section}:${target.index}`);
    },
    onDragLeave: () => setOver(undefined),
    onDrop: (event: DragEvent) => {
      setOver(undefined);
      const payload = readPlaceDrag({ types: [...event.dataTransfer.types], getData: type => event.dataTransfer.getData(type) });
      if (!payload || (payload.kind !== 'place' && payload.kind !== 'chat')) return;
      event.preventDefault();
      // A row's drop must not bubble into the section's end slot and perform the same write twice.
      event.stopPropagation();
      setDragging(undefined);
      if (payload.kind === 'chat') { if (target.placeId) actions.fileChats?.(payload.ids, target.placeId); return; }
      for (const [offset, id] of payload.ids.entries()) {
        if (target.section === 'pinned') actions.pin?.(id, pinnedDropIndex(pinnedIds, id, target.index) + offset);
        else if (pinnedIds.includes(id)) actions.unpin?.(id);
      }
    },
  });

  const focusProps = (id: string) => ({ 'data-rail-item': id, tabIndex: id === stop ? 0 : -1, onFocus: () => setCursor(id) });

  function row(place: PlaceRowModel, section: 'pinned' | 'open', index: number) {
    const slot = slotOf(place.id);
    const slotKey = `${section}:${index}`;
    return <ContextMenu key={place.id} label={`${place.name} actions`} items={menu(place, section)}>
      <RailRow name={place.name} parentName={place.parentName} tint={place.tint} active={place.id === current}
        status={place.status} statusLabel={place.statusLabel} closedButBusy={place.closedButBusy}
        close={section === 'open' ? { onClose: () => actions.close(place.id), tabs: tabCount?.(place.id), shortcut: place.id === current ? closeShortcut : undefined } : undefined}
        containerProps={{ 'data-drop': dropKind === 'chat' && over === slotKey || undefined,
          className: `${dropKind === 'place' && over === slotKey ? 'rail-insertion' : ''} ${dragging === place.id ? 'rail-dragging' : ''}`,
          draggable: !!actions.pin, onDragStart: event => {
            writeDrag(event, { kind: 'place', ids: [place.id] });
            // The browser captures the ghost before React renders the dragged state.
            event.currentTarget.classList.add('rail-dragging');
            const box = event.currentTarget.getBoundingClientRect();
            event.dataTransfer.setDragImage(event.currentTarget, event.clientX - box.left, event.clientY - box.top);
            setDragging(place.id);
          },
          onDragEnd: () => { setDragging(undefined); setOver(undefined); },
          ...dropProps({ section, index, placeId: place.id }) }}
        onKeyDown={event => {
          if (event.key === ' ' && !event.altKey && !event.metaKey && !event.ctrlKey && actions.quickLook) { event.preventDefault(); actions.quickLook(place.id); }
          if (event.altKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown') && section === 'pinned' && actions.pin) {
            event.preventDefault();
            const next = keyboardMoveTarget(pinnedIds, place.id, event.key);
            if (next !== undefined) { actions.pin(place.id, next); setAnnouncement(movedAnnouncement(next, pinnedIds.length)); }
          }
        }}
        aria-keyshortcuts={slot ? slotShortcut(slot).replace('⌃', 'Control+').replace('Alt ', 'Alt+') : undefined}
        {...focusProps(place.id)}
        onClick={event => (primaryClick(event) && actions.newWindow ? actions.newWindow(place.id) : actions.go(place.id))}
        onAuxClick={event => { if (event.button === 1 && actions.newWindow) { event.preventDefault(); actions.newWindow(place.id); } }}/>
    </ContextMenu>;
  }

  function onKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    // Alt+arrows reorder a pin; they must not also move the cursor. Arrows stop at the ends (PL-045).
    // Home and End still jump, the same chords the rail used before the list stopped wrapping.
    if (event.altKey || event.metaKey || event.ctrlKey || event.shiftKey) return;
    const items = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('[data-rail-item]')];
    const index = items.findIndex(item => item === document.activeElement);
    if (index < 0) return;
    const next = event.key === 'Home' ? 0
      : event.key === 'End' ? items.length - 1
      : event.key === 'ArrowDown' || event.key === 'ArrowUp' ? stepRail(items.length, index, event.key as RailKey)
      : undefined;
    if (next === undefined) return;
    event.preventDefault();
    items[next]?.focus();
  }

  const showShortcut = allPlaces && railShortcutVisible(livePlaces);
  return <div className="rail-sections" onKeyDown={onKeyDown}>
    <span className="rail-announce" role="status" aria-live="polite">{announcement}</span>
    <nav className="rail-nav" aria-label="Places">
      <div className="rail-group">
        <NowRow now={now} primaryClick={primaryClick} {...focusProps(NOW)}/>
      </div>
      {(pinned.length > 0 || dragging) && <Section label="Pinned"><div className={`rail-rows rail-drop-section ${dropKind === 'place' && over === `pinned:${pinned.length}` ? 'rail-insertion-end' : ''}`} {...dropProps({ section: 'pinned', index: pinned.length })}>{pinned.map((place, index) => row(place, 'pinned', index))}</div></Section>}
      {(open.length > 0 || dragging) && <Section label="Open" action={open.length > 0 ? <Button variant="ghost" className="rail-section-action" onClick={actions.closeAll}>Close all</Button> : undefined}>
        <div className={`rail-rows rail-drop-section ${dropKind === 'place' && over === `open:${open.length}` ? 'rail-insertion-end' : ''}`} {...dropProps({ section: 'open', index: open.length })}>{open.map((place, index) => row(place, 'open', index))}</div>
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
