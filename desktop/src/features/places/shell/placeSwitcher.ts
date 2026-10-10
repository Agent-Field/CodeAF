// The Home tab's place menu while the rail is put away (Places 9c). Pure, so a node test can pin the list
// without mounting the rail. Inbox is not an entry: questions elsewhere are the frame pill.
import { placeShortcuts } from '../../../design/keyboard.ts';
import type { MenuEntry } from '../../../components/ui/Menu.tsx';
import type { PlaceRailProps } from '../../shell/PlaceRail.tsx';
import type { PlacesShell } from './PlacesShell.tsx';

/** Now, the rail's places with their slot keys, then All places. */
export function placeSwitcher(shell: PlacesShell, rail: PlaceRailProps): { items: MenuEntry[]; alert?: string } {
  const sections = rail.sections ?? { pinned: [], open: [] };
  const order = [...sections.pinned, ...sections.open];
  const items: MenuEntry[] = [];
  items.push({ id: 'now', label: 'Now', icon: 'now', shortcut: placeShortcuts.slot(0), checked: shell.place === 'now', onSelect: rail.now.onGo });
  if (order.length) items.push({ kind: 'separator', id: 'places' });
  order.forEach((place, index) => items.push({ id: place.id, label: place.parentName ? `${place.name} · ${place.parentName}` : place.name, checked: place.id === shell.place,
    shortcut: index < 9 ? placeShortcuts.slot(index + 1) : undefined, onSelect: () => rail.actions.go(place.id) }));
  if (rail.allPlaces) { items.push({ kind: 'separator', id: 'all' }); items.push({ id: 'all-places', label: 'All places', icon: 'allPlaces', shortcut: placeShortcuts.allPlaces, onSelect: rail.allPlaces.onOpen }); }
  const elsewhere = order.find(place => place.id !== shell.place && place.status === 'waiting');
  return { items, alert: elsewhere?.statusLabel };
}
