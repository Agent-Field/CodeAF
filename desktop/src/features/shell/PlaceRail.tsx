import { useState, type DragEvent, type MouseEvent, type ReactNode } from 'react';
import { Button, ContextMenu, IconButton, NavigationItem, ThemeSelect, type IconName, type MenuEntry } from '../../components/ui';
import { choosableTints, PlaceSwatch, tintLabel, type TintName } from '../places/components/PlaceSwatch';
import type { PlaceRowModel } from '../places/shell/contracts';
import { chatDragType, placeDragType, readDrag, writeDrag } from '../places/place-actions';
import type { RailSections } from '../places/shell/selectors';
import { RailToggle } from './RailToggle';
import { PlaceDot as Dot } from '../places/PlaceDot';
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
  inert: boolean; paletteOpen: boolean; peeking: boolean;
  onToggle: () => void; onSearch: () => void;
  /** Inbox: absent where nothing backs it (no row, never a dead one). */
  inbox?: { count: number; active: boolean; onOpen: () => void };
  now: { active: boolean; status?: 'waiting' | 'failed'; statusLabel?: string; shortcut: string; onGo: () => void; onNewWindow?: () => void };
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
 * The rail of Places (6a, 10a; helper PRAIL): Inbox and Now; Pinned, the places the person lives in, in their order;
 * Open, every other place they went to, newest first; and All places at the foot. Two marks per row and never more:
 * the tint square on the left is identity, the dot on the right is status (amber needs you, red failed; running
 * draws nothing). On first launch, with no places at all, there is no Places heading of any kind.
 */
export function PlaceRail(props: PlaceRailProps) {
  const { inert, peeking, onToggle, inbox, now, sections, current, emptyHint, notice, allPlaces, actions, tabCount, slotShortcut, closeShortcut, newWindowShortcut, appItems } = props;
  const [over, setOver] = useState<string>();
  const pinnedIds = sections?.pinned.map(place => place.id) ?? [];
  const slotOf = (id: string) => { const order = [...pinnedIds, ...(sections?.open.map(place => place.id) ?? [])]; const at = order.indexOf(id); return at >= 0 && at < 9 ? at + 1 : undefined; };

  function menu(place: PlaceRowModel, section: 'pinned' | 'open'): MenuEntry[] {
    const groups: MenuEntry[][] = [[], [], [], []];
    groups[0].push({ id: 'go', label: 'Go to', shortcut: '↵', onSelect: () => actions.go(place.id) });
    if (actions.quickLook) groups[0].push({ id: 'quick-look', label: 'Quick Look', onSelect: () => actions.quickLook?.(place.id) });
    if (actions.newWindow) groups[0].push({ id: 'new-window', label: 'Open in new window', shortcut: newWindowShortcut, onSelect: () => actions.newWindow?.(place.id) });
    if (section === 'pinned' && actions.unpin) {
      groups[1].push({ id: 'unpin', label: 'Unpin', onSelect: () => actions.unpin?.(place.id) });
      const at = pinnedIds.indexOf(place.id);
      // Keyboard reordering, the same thing a drag within Pinned does.
      if (actions.pin && at > 0) groups[1].push({ id: 'up', label: 'Move up', onSelect: () => actions.pin?.(place.id, at - 1) });
      if (actions.pin && at < pinnedIds.length - 1) groups[1].push({ id: 'down', label: 'Move down', onSelect: () => actions.pin?.(place.id, at + 1) });
    } else if (actions.pin) groups[1].push({ id: 'pin', label: 'Pin', onSelect: () => actions.pin?.(place.id) });
    if (actions.rename) groups[1].push({ id: 'rename', label: 'Rename…', onSelect: () => actions.rename?.(place.id) });
    if (actions.setTint) groups[1].push({ kind: 'submenu', id: 'tint', label: 'Tint', items: choosableTints.map(tint => ({ id: `tint-${tint}`, label: tintLabel[tint], checked: tint === place.tint, onSelect: () => actions.setTint?.(place.id, tint) })) });
    groups[2].push({ id: 'close', label: 'Close', shortcut: place.id === current ? closeShortcut : undefined, onSelect: () => actions.close(place.id) });
    groups[2].push({ id: 'close-others', label: 'Close all others', disabled: !(sections?.open.some(other => other.id !== place.id)), onSelect: () => actions.closeOthers(place.id) });
    const entries: MenuEntry[] = [];
    groups.filter(group => group.length).forEach((group, index) => { if (index) entries.push({ kind: 'separator', id: `sep-${index}` }); entries.push(...group); });
    return entries;
  }

  // A place dragged within or into Pinned is pinned at that slot; one dragged into Open is unpinned. A chat dropped on a row is filed there.
  const dropProps = (target: { section: 'pinned' | 'open'; index: number; placeId?: string }) => ({
    onDragOver: (event: DragEvent) => {
      const types = [...event.dataTransfer.types];
      const place = types.includes(placeDragType) && (target.section === 'pinned' ? !!actions.pin : !!actions.unpin);
      const chat = types.includes(chatDragType) && !!target.placeId && !!actions.fileChats;
      if (!place && !chat) return;
      event.preventDefault();
      event.dataTransfer.dropEffect = chat ? 'copy' : 'move';
      setOver(`${target.section}:${target.index}`);
    },
    onDragLeave: () => setOver(undefined),
    onDrop: (event: DragEvent) => {
      setOver(undefined);
      const payload = readDrag(event);
      if (!payload) return;
      event.preventDefault();
      if (payload.kind === 'chat') { if (target.placeId) actions.fileChats?.(payload.ids, target.placeId); return; }
      for (const id of payload.ids) {
        if (target.section === 'pinned') actions.pin?.(id, target.index);
        else if (pinnedIds.includes(id)) actions.unpin?.(id);
      }
    },
  });

  function row(place: PlaceRowModel, section: 'pinned' | 'open', index: number) {
    const tabs = tabCount?.(place.id) ?? 0;
    const slot = slotOf(place.id);
    return <ContextMenu key={place.id} label={`${place.name} actions`} items={menu(place, section)}>
      <div className="rail-row" data-busy-closed={place.closedButBusy || undefined} data-drop={over === `${section}:${index}` || undefined}
        draggable={!!actions.pin} onDragStart={event => writeDrag(event, { kind: 'place', ids: [place.id] })} {...dropProps({ section, index, placeId: place.id })}>
        <NavigationItem className="rail-place" active={place.id === current} lead={<PlaceSwatch tint={place.tint} role="rail"/>}
          trail={<Dot status={place.status} label={place.statusLabel}/>}
          aria-keyshortcuts={slot ? slotShortcut(slot).replace('⌃', 'Control+').replace('Alt ', 'Alt+') : undefined}
          onClick={event => (primaryClick(event) && actions.newWindow ? actions.newWindow(place.id) : actions.go(place.id))}
          onAuxClick={event => { if (event.button === 1 && actions.newWindow) { event.preventDefault(); actions.newWindow(place.id); } }}>
          <span className="rail-place-name">{place.name}{place.parentName && <span className="rail-place-path"> · {place.parentName}</span>}</span>
          {place.closedButBusy && <span className="rail-place-path rail-place-closed">closed · still running</span>}
        </NavigationItem>
        {section === 'open' && !place.closedButBusy && <IconButton className="rail-row-close" icon="close" iconSize="micro" label={`Close ${place.name}`}
          title={`Close ${place.name}${tabs ? ` · ${tabs} ${tabs === 1 ? 'tab' : 'tabs'}` : ''}${place.id === current ? ` (${closeShortcut})` : ''}`} onClick={() => actions.close(place.id)}/>}
      </div>
    </ContextMenu>;
  }

  const pinned = sections?.pinned ?? [];
  const open = sections?.open ?? [];
  return <aside className="sidebar rail place-rail" aria-label="Main navigation" inert={inert}>
    <div className="rail-head" data-tauri-drag-region>
      <RailToggle placement="rail" collapsed={peeking} onClick={onToggle}/>
    </div>
    <nav className="rail-nav" aria-label="Places">
      <div className="rail-group">
        {inbox && <NavigationItem icon="inbox" active={inbox.active} onClick={inbox.onOpen} onContextMenu={event => event.preventDefault()} trail={inbox.count > 0 ? <Dot status="waiting" label={`${inbox.count} ${inbox.count === 1 ? 'needs' : 'need'} you`}/> : undefined}>Inbox</NavigationItem>}
        {/* Inbox and Now draw no menu (Interactions, right-click "—"). Swallowing the event keeps the webview's own menu off those rows. */}
        <NavigationItem icon="now" active={now.active} aria-keyshortcuts={now.shortcut} onContextMenu={event => event.preventDefault()} onClick={event => (primaryClick(event) && now.onNewWindow ? now.onNewWindow() : now.onGo())}
          onAuxClick={event => { if (event.button === 1 && now.onNewWindow) { event.preventDefault(); now.onNewWindow(); } }}
          trail={<Dot status={now.status} label={now.statusLabel}/>}>Now</NavigationItem>
      </div>
      {pinned.length > 0 && <Section label="Pinned"><div className="rail-rows" {...dropProps({ section: 'pinned', index: pinned.length })}>{pinned.map((place, index) => row(place, 'pinned', index))}</div></Section>}
      {open.length > 0 && <Section label="Open" action={<Button variant="ghost" className="rail-section-action" onClick={actions.closeAll}>Close all</Button>}>
        <div className="rail-rows" {...dropProps({ section: 'open', index: open.length })}>{open.map((place, index) => row(place, 'open', index))}</div>
      </Section>}
      {emptyHint && !pinned.length && !open.length && <p className="rail-hint">Places you open show here. Pin the ones you live in.</p>}
      {notice && <p className="rail-hint" role="status">{notice.text}{notice.onRetry && <> · <Button variant="ghost" className="rail-section-action" onClick={notice.onRetry}>Retry</Button></>}</p>}
    </nav>
    <div className="rail-foot">
      {allPlaces && <NavigationItem icon="allPlaces" active={allPlaces.active} onClick={event => (primaryClick(event) && allPlaces.onOpenInNewTab ? allPlaces.onOpenInNewTab() : allPlaces.onOpen())}
        trail={<span className="rail-meta">{allPlaces.shortcut}</span>}>All places</NavigationItem>}
      {appItems.length > 0 && <nav className="rail-nav rail-app" aria-label="App">{appItems.map(item => <NavigationItem key={item.label} icon={item.icon} active={item.active} data-hover={item.hover || undefined} onClick={item.onSelect}>{item.label}</NavigationItem>)}</nav>}
      {appItems.length > 0 && <div className="sidebar-bottom"><ThemeSelect/></div>}
    </div>
  </aside>;
}
