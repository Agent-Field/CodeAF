import { useState, type DragEvent, type MouseEvent, type ReactNode } from 'react';
import { Button, ContextMenu, NavigationItem, ThemeSelect, type IconName, type MenuEntry } from '../../components/ui';
import type { TintName } from '../places/components/PlaceSwatch';
import type { PlaceRowModel } from '../places/shell/contracts';
import { chatDragType, placeDragType, writeDrag } from '../places/place-actions';
import { readPlaceDrag } from '../places/dnd/placeDnd';
import type { RailSections } from '../places/shell/selectors';
import { RailToggle } from './RailToggle';
import { NowRow } from './NowRow';
import { RailRow } from '../places/rail/RailRow';
import { railMenu } from '../places/menus/railMenu';
import { useRailNavigation } from '../places/rail/useRailNavigation';
import './rail.css';
import './place-rail.css';

/** One of the window's other pages, drawn quietly under All places. `hover` forces the hover fill, for the specimen. */
export type RailItem = { label: string; icon: IconName; active: boolean; onSelect: () => void; hover?: boolean };

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
  /** Chats dropped on a row are added to that place (Places 6e "Drop a file, folder, URL or tab onto a place in the rail"). */
  fileChats?: (chatIds: readonly string[], placeId: string) => void;
};

export type PlaceRailProps = {
  /** True when the rail is not on screen: its controls leave the tab order. */
  inert: boolean; peeking: boolean;
  onToggle: () => void;
  now: { active: boolean; count?: number; status?: 'waiting' | 'failed'; statusLabel?: string; shortcut: string; onGo: () => void; onNewWindow?: () => void };
  /** Pinned and Open; absent while the engine has no Places to read. */
  sections?: RailSections;
  /** The place this window shows (its row reads as the open one). */
  current?: string;
  /** True when the graph has places but none is pinned or open: the one quiet line of 10a "Empty". */
  emptyHint?: boolean;
  /** One muted line when Places cannot be read, with Retry when retrying can help. */
  notice?: { text: string; onRetry?: () => void };
  allPlaces?: { active: boolean; shortcut: string; onOpen: () => void; onOpenInNewTab?: () => void };
  actions: PlaceRailActions;
  /** The open tab count of a place in this window, for the close hint ("Close Marketing · 4 tabs"). */
  tabCount?: (id: string) => number;
  slotShortcut: (index: number) => string;
  closeShortcut: string;
  newWindowShortcut: string;
  /** The window's other pages (Activity, Settings, Design system), quiet under All places. */
  appItems: readonly RailItem[];
};

const primaryClick = (event: MouseEvent) => event.metaKey || event.ctrlKey;

function Section({ label, action, children }: { label: string; action?: ReactNode; children: ReactNode }) {
  return <section className="rail-group" aria-label={label}>
    <div className="rail-section-label"><span>{label}</span>{action}</div>
    {children}
  </section>;
}

/**
 * The rail of Places (6a, 8e, 10a; helper PRAIL): Now; Pinned, the places the person lives in, in their order;
 * Open, every other place they went to, newest first; and All places at the foot. There is no Inbox row.
 * Two marks per row and never more: the tint square on the left is identity, the dot on the right is status
 * (amber needs you, red failed; running draws nothing). On first launch, with no places at all, there is no
 * Places heading of any kind — Now, and All places when the engine can open it.
 */
export function PlaceRail(props: PlaceRailProps) {
  const { inert, peeking, onToggle, now, sections, current, emptyHint, notice, allPlaces, actions, tabCount, slotShortcut, closeShortcut, newWindowShortcut, appItems } = props;
  const [over, setOver] = useState<string>();
  const [dragging, setDragging] = useState<string>();
  const [dropKind, setDropKind] = useState<'place' | 'chat'>();
  const navigation = useRailNavigation(`${current}:${now.active}:${allPlaces?.active}:${sections?.pinned.map(p => p.id)}:${sections?.open.map(p => p.id)}`);
  const pinnedIds = sections?.pinned.map(place => place.id) ?? [];
  const slotOf = (id: string) => { const order = [...pinnedIds, ...(sections?.open.map(place => place.id) ?? [])]; const at = order.indexOf(id); return at >= 0 && at < 9 ? at + 1 : undefined; };

  const indexOfPinned = (id: string) => pinnedIds.indexOf(id);

  function menu(place: PlaceRowModel, section: 'pinned' | 'open'): MenuEntry[] {
    return railMenu({ ...place, pinned: section === 'pinned' }, {
      goTo: actions.go, quickLook: actions.quickLook, newWindow: actions.newWindow, newWindowShortcut,
      pin: actions.pin, unpin: actions.unpin, startRename: actions.rename, setTint: actions.setTint,
      close: actions.close, closeOthers: actions.closeOthers,
      closeShortcut: place.id === current ? closeShortcut : undefined,
      closeOthersDisabled: !sections?.open.some(other => other.id !== place.id),
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
        if (target.section === 'pinned') actions.pin?.(id, target.index + offset);
        else if (pinnedIds.includes(id)) actions.unpin?.(id);
      }
    },
  });

  function row(place: PlaceRowModel, section: 'pinned' | 'open', index: number) {
    const slot = slotOf(place.id);
    return <ContextMenu key={place.id} label={`${place.name} actions`} items={menu(place, section)}>
      <RailRow name={place.name} parentName={place.parentName} tint={place.tint} active={place.id === current}
        status={place.status} statusLabel={place.statusLabel} closedButBusy={place.closedButBusy}
        close={section === 'open' ? { onClose: () => actions.close(place.id), tabs: tabCount?.(place.id), shortcut: place.id === current ? closeShortcut : undefined } : undefined}
        containerProps={{ 'data-drop': dropKind === 'chat' && over === `${section}:${index}` || undefined,
          className: `${dropKind === 'place' && over === `${section}:${index}` ? 'rail-insertion' : ''} ${dragging === place.id ? 'rail-dragging' : ''}`,
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
            const next = index + (event.key === 'ArrowUp' ? -1 : 1);
            if (next >= 0 && next < pinnedIds.length) actions.pin(place.id, next);
          }
        }}
        aria-keyshortcuts={slot ? slotShortcut(slot).replace('⌃', 'Control+').replace('Alt ', 'Alt+') : undefined}
        onClick={event => (primaryClick(event) && actions.newWindow ? actions.newWindow(place.id) : actions.go(place.id))}
        onAuxClick={event => { if (event.button === 1 && actions.newWindow) { event.preventDefault(); actions.newWindow(place.id); } }}/>
    </ContextMenu>;
  }

  const pinned = sections?.pinned ?? [];
  const open = sections?.open ?? [];
  return <aside {...navigation} className="sidebar rail place-rail" aria-label="Main navigation" inert={inert}>
    <div className="rail-head" data-tauri-drag-region>
      <RailToggle placement="rail" collapsed={peeking} onClick={onToggle}/>
    </div>
    <nav className="rail-nav" aria-label="Places">
      <div className="rail-group">
        <NowRow now={now} primaryClick={primaryClick}/>
      </div>
      {(pinned.length > 0 || dragging) && <Section label="Pinned"><div className={`rail-rows rail-drop-section ${dropKind === 'place' && over === `pinned:${pinned.length}` ? 'rail-insertion-end' : ''}`} {...dropProps({ section: 'pinned', index: pinned.length })}>{pinned.map((place, index) => row(place, 'pinned', index))}</div></Section>}
      {(open.length > 0 || dragging) && <Section label="Open" action={open.length > 0 ? <Button variant="ghost" className="rail-section-action" onClick={actions.closeAll}>Close all</Button> : undefined}>
        <div className={`rail-rows rail-drop-section ${dropKind === 'place' && over === `open:${open.length}` ? 'rail-insertion-end' : ''}`} {...dropProps({ section: 'open', index: open.length })}>{open.map((place, index) => row(place, 'open', index))}</div>
      </Section>}
      {emptyHint && !pinned.length && !open.length && <p className="rail-hint">Places you open show here. Pin the ones you live in.</p>}
      {notice && <p className="rail-hint" role="status">{notice.text}{notice.onRetry && <> · <Button variant="ghost" className="rail-section-action" onClick={notice.onRetry}>Retry</Button></>}</p>}
    </nav>
    <div className="rail-foot">
      {allPlaces && <NavigationItem icon="allPlaces" active={allPlaces.active} onClick={event => (primaryClick(event) && allPlaces.onOpenInNewTab ? allPlaces.onOpenInNewTab() : allPlaces.onOpen())}
        onAuxClick={event => { if (event.button === 1 && allPlaces.onOpenInNewTab) { event.preventDefault(); allPlaces.onOpenInNewTab(); } }}
        trail={<span className="rail-meta">{allPlaces.shortcut}</span>}>All places</NavigationItem>}
      {appItems.length > 0 && <nav className="rail-nav rail-app" aria-label="App">{appItems.map(item => <NavigationItem key={item.label} icon={item.icon} active={item.active} data-hover={item.hover || undefined} onClick={item.onSelect}>{item.label}</NavigationItem>)}</nav>}
      {appItems.length > 0 && <div className="sidebar-bottom"><ThemeSelect/></div>}
    </div>
  </aside>;
}
