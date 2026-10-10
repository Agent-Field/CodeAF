import type { MenuEntry } from '../../../components/ui';
import { decideEntries } from '../PlaceMenuDecide.ts';
import type { PlaceActions } from '../place-actions.ts';
import type { PlaceDecide, Tint } from '../wire.ts';

/** A place the menu can describe. `decide` is absent when the engine did not say how the place decides, and those two rows stay off. */
export type MenuPlace = {
  id: string;
  name: string;
  tint: Tint;
  pinned?: boolean;
  archived?: boolean;
  decide?: PlaceDecide;
};

export type MenuContext = {
  /** Offline or loading: every write is dropped from the menu, navigation stays. */
  readOnly?: boolean;
  /**
   * This window is already showing the place (the Home heading ⋯, and the Home tab).
   * Go to and Quick Look are left off. Open in new window stays: another window is still a different place.
   */
  current?: boolean;
  /** Rename happens in place on the page or the tile; the Home supplies how. */
  startRename?: (placeId: string) => void;
  startDelete?: (placeId: string) => void;
  /** Whether the place can be renamed on this page (the heading and a tile can; a search result cannot). */
  canRename?: boolean;
  /** `⌘`-style labels come from the host (src/design/keyboard.ts); the Home passes them through. */
  newWindowHint?: string;
};

const action = (id: string, label: string, onSelect: () => void, extra: Partial<Extract<MenuEntry, { kind?: 'action' }>> = {}): MenuEntry => ({ id, label, onSelect, ...extra });

/** Places 8f draws the five squares in this order. Graphite is never among them: it means no tint was chosen. The words match tintLabel without loading the swatch component. */
const menuTintOrder = ['tide', 'rose', 'sage', 'sand', 'iris'] as const;
const menuTintLabel: Record<(typeof menuTintOrder)[number], string> = { tide: 'Tide', rose: 'Rose', sage: 'Sage', sand: 'Sand', iris: 'Iris' };
const isMenuTint = (id: string): id is typeof menuTintOrder[number] => (menuTintOrder as readonly string[]).includes(id);

/** Rows the drawing gives no glyph still keep the 14px column, so their words line up with Go to and Archive. */
const aligned = (entry: MenuEntry): MenuEntry => {
  if (entry.kind === 'separator' || entry.kind === 'swatches' || entry.icon) return entry;
  return { ...entry, iconSlot: true };
};

/**
 * The place menu of Places 8f, in the drawing's order, for a tile, the Home heading ⋯ and the Home tab.
 * A verb without a handler is absent. Separators only ever sit between two real entries.
 * The drawing's two rules are after Open in new window, and after Pin (before Archive).
 * Tint is the inline swatch row under the word, not a flyout.
 * Delete place… is danger-coloured words with no glyph.
 */
export function placeMenu(place: MenuPlace, actions: PlaceActions, context: MenuContext = {}): MenuEntry[] {
  const writes = !context.readOnly;
  const groups: MenuEntry[][] = [[], [], [], []];
  // Navigation. Go to and Quick Look describe a place you are not already in.
  if (!context.current && actions.goTo) groups[0].push(action('go', 'Go to', () => void actions.goTo?.(place.id), { icon: 'arrow', shortcut: '↵' }));
  if (!context.current && actions.quickLook) groups[0].push(action('quick-look', 'Quick Look', () => actions.quickLook?.(place.id), { icon: 'eye', shortcut: 'Space' }));
  if (actions.goToInNewWindow) groups[0].push(action('new-window', 'Open in new window', () => void actions.goToInNewWindow?.(place.id), { icon: 'appWindow', shortcut: context.newWindowHint ?? '⌘↵' }));
  if (writes) {
    if (context.canRename && context.startRename) groups[1].push(action('rename', 'Rename', () => context.startRename?.(place.id), { icon: 'pencil' }));
    if (actions.setTint) groups[1].push({
      kind: 'swatches', id: 'tint', label: 'Tint', icon: 'palette',
      options: menuTintOrder.map(tint => ({ id: tint, label: menuTintLabel[tint] })),
      selected: isMenuTint(place.tint) ? place.tint : undefined,
      onSelect: id => { if (isMenuTint(id)) void actions.setTint?.(place.id, id); },
    });
    // Add to another place… is the keyboard path for the same second parent a plain drop adds (PL-224).
    if (actions.chooseAnotherParent) groups[1].push(action('another-parent', 'Add to another place…', () => actions.chooseAnotherParent?.(place.id), { icon: 'folderInput' }));
    // Merge was decided after 8f: it sits after Add to another place… and before Pin to rail.
    if (actions.chooseMergeTarget) groups[1].push(aligned(action('merge', 'Merge into…', () => actions.chooseMergeTarget?.(place.id))));
    if (!place.archived) {
      if (place.pinned && actions.unpin) groups[1].push(action('unpin', 'Unpin', () => void actions.unpin?.(place.id), { icon: 'pin' }));
      else if (!place.pinned && actions.pin) groups[1].push(action('pin', 'Pin to rail', () => void actions.pin?.(place.id), { icon: 'pin' }));
    }
    // Always ask me and Decision confidence… are not in the 8f drawing. They keep their own band, after Pin and before Archive.
    if (!place.archived) groups[2].push(...decideEntries(place.id, place.decide, actions.setDecide).map(aligned));
    if (!place.archived) {
      if (actions.archive) groups[3].push(action('archive', 'Archive', () => void actions.archive?.(place.id), { icon: 'archive' }));
    } else if (actions.restore) groups[3].push(aligned(action('restore', 'Restore', () => void actions.restore?.(place.id))));
    if (actions.remove && context.startDelete) groups[3].push(aligned(action('delete', 'Delete place…', () => context.startDelete?.(place.id), { danger: true })));
  }
  const entries: MenuEntry[] = [];
  groups.filter(group => group.length).forEach((group, index) => {
    if (index > 0) entries.push({ kind: 'separator', id: `sep-${index}` });
    entries.push(...group);
  });
  return entries;
}

/** The ⋯ beside the name of the place this window is showing. The Home tab uses the same builder with `current` set, and does not wire Archive or Delete. */
export function homeMenu(place: MenuPlace, actions: PlaceActions, context: MenuContext = {}): MenuEntry[] {
  return placeMenu(place, actions, { ...context, current: true });
}
