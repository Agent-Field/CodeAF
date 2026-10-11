// The window-level wiring of Places: the rail's model, the keys, and the window tint. App calls these once.
// Questions that need the person are the strip's frame pill, not a rail row.

import { useMemo } from 'react';
import { placeShortcuts } from '../../../design/keyboard';
import type { PlaceRailActions, PlaceRailProps } from '../../shell/PlaceRail';
import type { PlacesShell } from './PlacesShell';
import { requestWorkspace } from './workspaceBus';
import { railSections, rollupStatus } from './selectors';

const failure = (shell: PlacesShell, job: () => Promise<unknown>) => () => { void job().catch(shell.warn); };

/** The rail's actions: each is one shell verb, and a refusal is said in a toast (the rail has no page to say it on). */
export function railActions(shell: PlacesShell): PlaceRailActions {
  const name = (id: string) => shell.index?.byId.get(id)?.name;
  const writable = shell.places.status === 'ready';
  return {
    go: id => failure(shell, () => shell.goTo(id))(),
    // Owner decision (11 Oct): places are libraries. A click opens the place's Home as a tab; Focus filters the strip to it.
    focus: id => shell.toggleFocus(id),
    addTab: (tabId, placeId) => { requestWorkspace({ type: 'tab-place', id: tabId, place: placeId }); },
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

type RailInputs = Pick<PlaceRailProps, 'inert' | 'peeking' | 'onToggle' | 'settings' | 'designSystem'> & {
  /** True while the workspace page is showing (the rail's place rows then read as open). */
  onWorkspace: boolean;
  /** The focused Home tab's place (`root` for All places). */
  activeHome?: string;
  onEnterWorkspace: () => void;
};

export function usePlaceRail(shell: PlacesShell, inputs: RailInputs): PlaceRailProps {
  const { graph, status } = shell.places;
  const sections = useMemo(() => (graph ? railSections(graph, shell.closed, shell.place) : undefined), [graph, shell.closed, shell.place]);
  const enter = <T extends unknown[]>(fn: (...args: T) => void) => (...args: T) => { inputs.onEnterWorkspace(); fn(...args); };
  const actions = railActions(shell);
  const allPlacesActive = inputs.onWorkspace && inputs.activeHome === 'root';
  const notice = status === 'offline' ? { text: 'Can’t reach the engine', onRetry: () => void shell.refresh() }
    : status === 'error' ? { text: shell.places.error ?? 'Places could not be read', onRetry: () => void shell.refresh() } : undefined;
  const { onWorkspace: _onWorkspace, activeHome: _activeHome, onEnterWorkspace: _enter, ...rest } = inputs;
  return {
    ...rest,
    now: {
      active: inputs.onWorkspace && shell.place === 'now' && !allPlacesActive, shortcut: placeShortcuts.slot(0),
      // Places 8e draws this as the unplaced-chat count ("Not in any place"), not how many are running.
      count: graph?.now.chats, status: rollupStatus(graph?.now.status), statusLabel: graph && graph.now.status.needsYou > 0 ? `${graph.now.status.needsYou} need${graph.now.status.needsYou === 1 ? 's' : ''} you in Now` : undefined,
      onGo: enter(() => void shell.goTo('now').catch(shell.warn)),
      // Without native windows the modified click must take the same workspace entry path as a plain click.
      onNewWindow: shell.native.desktop ? () => void shell.goToInNewWindow('now').catch(shell.warn) : undefined,
    },
    sections,
    current: inputs.onWorkspace && !allPlacesActive && shell.place !== 'now' ? shell.place : undefined,
    focused: shell.focus,
    emptyHint: !!graph && graph.places.some(place => !place.archived),
    livePlaces: graph ? graph.places.filter(place => !place.archived).length : undefined,
    notice,
    // All places exists wherever the engine has a Places door; a bridge without one draws no row rather than a dead one.
    allPlaces: status === 'unavailable' ? undefined : { active: allPlacesActive, shortcut: placeShortcuts.allPlaces, onOpen: enter(() => shell.openAllPlaces()), onOpenInNewTab: enter(() => shell.openAllPlaces(true)) },
    actions: { ...actions, go: enter(actions.go), focus: enter(actions.focus!) },
    slotShortcut: placeShortcuts.slot, closeShortcut: placeShortcuts.close, newWindowShortcut: placeShortcuts.openInNewWindow,
  };
}

// The Places keys live in features/places/usePlaceKeys.ts; App still imports them from here.
export { usePlaceKeys } from '../usePlaceKeys';

// The frame tint lives in useWindowTint: Now and the root are graphite, and the swap is the attribute before paint.
export { useWindowTint } from '../useWindowTint';

// The collapsed-rail menu is pure and tested on its own. App still imports it from here.
export { placeSwitcher } from './placeSwitcher.ts';
