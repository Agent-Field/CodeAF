// The window-level wiring of Places: the rail's model, the keys, the window tint, and the attention the engine-wide
// world stream reports, handed to the native notifications and the dock badge. App calls these once.

import { useEffect, useMemo, useSyncExternalStore } from 'react';
import { placeShortcuts, registerShortcuts, shortcutLayer, tabShortcuts } from '../../../design/keyboard';
import type { AttentionItem } from '../../../design/nativeControls';
import { worldStore } from '../../chat/world-store';
import { parseWorkspace, workspaceKey } from '../../tabs/model';
import type { PlaceRailActions, PlaceRailProps } from '../../shell/PlaceRail';
import type { MenuEntry } from '../../../components/ui';
import type { PlacesShell } from './PlacesShell';
import { railOrder, railSections, rollupStatus } from './selectors';
import { requestWorkspace } from './workspaceBus';
import { requestOpenKind } from '../../shell/shellState';

/** How many tabs a place has open, from its saved tab set (the rail's close hint). */
function tabCount(id: string): number {
  try { return parseWorkspace(localStorage.getItem(workspaceKey(id)))?.tabs.length ?? 0; } catch { return 0; }
}

const failure = (shell: PlacesShell, job: () => Promise<unknown>) => () => { void job().catch(shell.warn); };

/** The rail's actions: each is one shell verb, and a refusal is said in a toast (the rail has no page to say it on). */
export function railActions(shell: PlacesShell): PlaceRailActions {
  const name = (id: string) => shell.index?.byId.get(id)?.name;
  const writable = shell.places.status === 'ready';
  return {
    go: id => failure(shell, () => shell.goTo(id))(),
    newWindow: id => failure(shell, () => shell.goToInNewWindow(id))(),
    quickLook: id => shell.quickLook(id),
    pin: writable ? (id, index) => failure(shell, () => shell.write(index === undefined ? `Pinned “${name(id)}” to the rail` : `Moved “${name(id)}” in Pinned`, () => shell.client.pinPlace(id, index), { subject: name(id) }))() : undefined,
    unpin: writable ? id => failure(shell, () => shell.write(`Unpinned “${name(id)}”`, () => shell.client.unpinPlace(id), { subject: name(id) }))() : undefined,
    rename: writable ? id => shell.openDialog({ kind: 'rename', placeId: id }) : undefined,
    setTint: writable ? (id, tint) => failure(shell, () => shell.write(`Changed the tint of “${name(id)}”`, () => shell.client.updatePlace(id, { tint }), { subject: name(id) }))() : undefined,
    close: id => shell.closePlace(id),
    closeAll: () => shell.closeOpen(),
    closeOthers: id => shell.closeOpen(id),
    fileChats: writable ? (ids, placeId) => failure(shell, () => shell.write(`Added ${ids.length === 1 ? 'a chat' : `${ids.length} chats`} to “${name(placeId)}”`, () => shell.client.addChats(placeId, [...ids]), { subject: name(placeId) }))() : undefined,
  };
}

type RailInputs = Pick<PlaceRailProps, 'inert' | 'peeking' | 'onToggle' | 'appItems'> & {
  /** True while the workspace page is showing (the rail's place rows then read as open). */
  onWorkspace: boolean;
  /** The focused Home tab's place (`root` for All places). */
  activeHome?: string;
  onEnterWorkspace: () => void;
  /** Questions waiting on the person, from the world stream, and whether the Inbox tab is the one showing. */
  inbox: { count: number; active: boolean };
};

export function usePlaceRail(shell: PlacesShell, inputs: RailInputs): PlaceRailProps {
  const { graph, status } = shell.places;
  const sections = useMemo(() => (graph ? railSections(graph, shell.closed, shell.place) : undefined), [graph, shell.closed, shell.place]);
  const enter = <T extends unknown[]>(fn: (...args: T) => void) => (...args: T) => { inputs.onEnterWorkspace(); fn(...args); };
  const actions = railActions(shell);
  const allPlacesActive = inputs.onWorkspace && inputs.activeHome === 'root';
  const notice = status === 'offline' ? { text: 'Can’t reach the engine', onRetry: () => void shell.refresh() }
    : status === 'error' ? { text: shell.places.error ?? 'Places could not be read', onRetry: () => void shell.refresh() } : undefined;
  const { inbox, onWorkspace: _onWorkspace, activeHome: _activeHome, onEnterWorkspace: _enter, ...rest } = inputs;
  return {
    ...rest,
    inbox: { ...inbox, onOpen: enter(() => requestOpenKind('inbox')) },
    now: {
      active: inputs.onWorkspace && shell.place === 'now' && !allPlacesActive, shortcut: placeShortcuts.slot(0),
      status: rollupStatus(graph?.now.status), statusLabel: graph && graph.now.status.needsYou > 0 ? `${graph.now.status.needsYou} need${graph.now.status.needsYou === 1 ? 's' : ''} you in Now` : undefined,
      onGo: enter(() => void shell.goTo('now')), onNewWindow: () => void shell.goToInNewWindow('now').catch(shell.warn),
    },
    sections,
    current: inputs.onWorkspace && !allPlacesActive && shell.place !== 'now' ? shell.place : undefined,
    emptyHint: !!graph && graph.places.some(place => !place.archived),
    notice,
    // All places exists wherever the engine has a Places door; a bridge without one draws no row rather than a dead one.
    allPlaces: status === 'unavailable' ? undefined : { active: allPlacesActive, shortcut: placeShortcuts.allPlaces, onOpen: enter(() => shell.openAllPlaces()), onOpenInNewTab: enter(() => shell.openAllPlaces(true)) },
    actions: { ...actions, go: enter(actions.go) },
    tabCount, slotShortcut: placeShortcuts.slot, closeShortcut: placeShortcuts.close, newWindowShortcut: placeShortcuts.openInNewWindow,
  };
}

/** The Places keys (Interactions "Shortcuts"). They live in the app layer, so a surface that uses the same chord first keeps it. */
export function usePlaceKeys(shell: PlacesShell, onEnterWorkspace: () => void) {
  const graph = shell.places.graph;
  const order = useMemo(() => (graph ? railOrder(railSections(graph, shell.closed, shell.place)) : []), [graph, shell.closed, shell.place]);
  useEffect(() => registerShortcuts(shortcutLayer.app, shortcut => {
    switch (shortcut.id) {
      case 'goto': shell.openChooser({ kind: 'go' }); return true;
      case 'all-places': onEnterWorkspace(); shell.openAllPlaces(); return true;
      case 'place-home':
        if (shell.place === 'now') { shell.warn(new Error('Now has no Home. Go to a place to see its Home.')); return true; }
        onEnterWorkspace();
        requestWorkspace({ type: 'home-ensure', place: shell.place, title: shell.index?.byId.get(shell.place)?.name ?? 'Home', focus: true });
        return true;
      case 'place-jump': {
        const id = shortcut.index === 0 ? 'now' : order[(shortcut.index ?? 1) - 1];
        if (!id) return false;
        onEnterWorkspace();
        void shell.goTo(id).catch(shell.warn);
        return true;
      }
      case 'close-place':
        if (shell.place === 'now') shell.warn(new Error(`Now is always open. Close its tabs with ${tabShortcuts.close}.`));
        else shell.closePlace(shell.place);
        return true;
      case 'new-window': void shell.goToInNewWindow('now').catch(shell.warn); return true;
      case 'undo': void shell.undoLast().catch(shell.warn); return true;
      default: return false;
    }
  }), [shell, order, onEnterWorkspace]);
}

// The frame tint lives in useWindowTint: Now and the root are graphite, and the swap is the attribute before paint.
export { useWindowTint } from '../useWindowTint';

/**
 * What needs the person, from the engine-wide world stream, for the rail's Inbox count. The system notifications and
 * the dock badge are posted by the workspace (tabs/closing/useBackground.ts), which also knows the failures: posting a
 * second, failure-less list from here made Rust forget the failures and announce them again.
 */
export function useAttentionNotices() {
  const world = useSyncExternalStore(worldStore.subscribe, worldStore.getState);
  // Every world attention item is a question a conversation is stopped on (its `kind` is the question's own kind:
  // consent, choice…). The stream reports no "failed now" item, so failures never count here.
  const items = useMemo<AttentionItem[]>(() => world.items.map(item => ({ id: item.key, kind: 'needsYou' as const, chatTitle: item.title || 'Untitled chat', text: item.text })), [world.items]);
  return { items, status: world.status };
}

/** The Home tab's place switcher while the rail is put away (Places 9c): Inbox, Now, the rail's places with their slot keys, All places. */
export function placeSwitcher(shell: PlacesShell, rail: PlaceRailProps): { items: MenuEntry[]; alert?: string } {
  const sections = rail.sections ?? { pinned: [], open: [] };
  const order = [...sections.pinned, ...sections.open];
  const items: MenuEntry[] = [];
  if (rail.inbox) items.push({ id: 'inbox', label: rail.inbox.count ? `Inbox · ${rail.inbox.count}` : 'Inbox', icon: 'inbox', onSelect: rail.inbox.onOpen });
  items.push({ id: 'now', label: 'Now', icon: 'now', shortcut: placeShortcuts.slot(0), checked: shell.place === 'now', onSelect: rail.now.onGo });
  if (order.length) items.push({ kind: 'separator', id: 'places' });
  order.forEach((place, index) => items.push({ id: place.id, label: place.parentName ? `${place.name} · ${place.parentName}` : place.name, checked: place.id === shell.place,
    shortcut: index < 9 ? placeShortcuts.slot(index + 1) : undefined, onSelect: () => rail.actions.go(place.id) }));
  if (rail.allPlaces) { items.push({ kind: 'separator', id: 'all' }); items.push({ id: 'all-places', label: 'All places', icon: 'allPlaces', shortcut: placeShortcuts.allPlaces, onSelect: rail.allPlaces.onOpen }); }
  const elsewhere = order.find(place => place.id !== shell.place && place.status === 'waiting');
  return { items, alert: elsewhere?.statusLabel };
}
