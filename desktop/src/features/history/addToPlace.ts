import { useCallback } from 'react';
import type { MenuEntry } from '../../components/ui/Menu';
import { chatIdFromSessionFile } from '../places/client.ts';
import { pickPlace } from '../places/palette/pickPlace.ts';
import { usePlaces } from '../places/usePlaces.ts';
import type { PlaceView } from '../places/wire.ts';
import type { HistoryItem } from './types.ts';

/** The submenu lists ten places and then "All places…" (Interactions History row "Add to place"). */
export const ADD_TO_PLACE_LIMIT = 10;

/** Pinned places first, then the rest by last opened, newest first. Archived places are never offered. */
export function placesToOffer(nodes: readonly PlaceView[]): PlaceView[] {
  const live = nodes.filter(place => !place.archived);
  const recent = (place: PlaceView) => place.lastOpenedAt ?? '';
  const pinned = live.filter(place => place.pinned);
  const rest = live.filter(place => !place.pinned).sort((a, b) => recent(b).localeCompare(recent(a)));
  return [...pinned, ...rest].slice(0, ADD_TO_PLACE_LIMIT);
}

/**
 * The "Add to place ▸" entry for one row, or nothing. With no places there is nothing to add to, so the item is absent
 * rather than present and empty (the emptiness law). `add` posts one membership; `all` opens the ⌘P place switcher.
 */
export function addToPlaceEntry(item: HistoryItem, nodes: readonly PlaceView[], add: (placeId: string, chatId: string) => void, all: (chatId: string) => void): MenuEntry | undefined {
  const chatId = chatIdFromSessionFile(item.sessionFile);
  const offered = placesToOffer(nodes);
  if (!chatId || offered.length === 0) return undefined;
  return {
    kind: 'submenu', id: 'add-to-place', label: 'Add to place', icon: 'folder',
    items: [
      ...offered.map<MenuEntry>(place => ({ id: `place-${place.id}`, label: place.name, onSelect: () => add(place.id, chatId) })),
      { kind: 'separator', id: 'sep-all-places' },
      { id: 'all-places', label: 'All places…', onSelect: () => all(chatId) },
    ],
  };
}

/** Builds the entry from the live places snapshot; the membership goes through the client, which owns generations and refusals. */
export function useAddToPlace(): (item: HistoryItem) => MenuEntry | undefined {
  const { nodes, mutations } = usePlaces();
  return useCallback((item: HistoryItem) => addToPlaceEntry(item, nodes,
    (placeId, chatId) => { void mutations.addChats(placeId, [chatId]).catch(() => undefined); },
    chatId => { void pickPlace({ title: 'Add to a place' }).then(id => { if (id) return mutations.addChats(id, [chatId]); }).catch(() => undefined); }), [nodes, mutations]);
}
