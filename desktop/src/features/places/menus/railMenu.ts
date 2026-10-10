// The right-click menu of a rail place row (Interactions P-IX-8): Go to · Quick Look · Pin/Unpin · Rename · Tint · Close · Close all others.
import type { MenuEntry } from '../../../components/ui/index.ts';
import type { TintName } from '../components/PlaceSwatch.tsx';

/** The rail row the menu is for. `pinned` decides the label of the Pin row. */
export type RailMenuRow = { id: string; tint: TintName; pinned: boolean };

/**
 * What a rail row can do. EVERY member is optional on purpose: a verb the host has not wired is left off the menu,
 * because a row that does nothing is worse than no row.
 */
export type RailMenuActions = {
  goTo?: (placeId: string) => void;
  quickLook?: (placeId: string) => void;
  newWindow?: (placeId: string) => void;
  newWindowShortcut?: string;
  pin?: (placeId: string) => void;
  unpin?: (placeId: string) => void;
  /** Starts the inline rename on the row; the name itself is written by the host. */
  startRename?: (placeId: string) => void;
  setTint?: (placeId: string, tint: TintName) => void;
  /** Takes the place off the Open list. Work that is still running keeps its row, as the × does. */
  close?: (placeId: string) => void;
  closeOthers?: (keepPlaceId: string) => void;
  closeShortcut?: string;
  closeOthersDisabled?: boolean;
  moveUp?: (placeId: string) => void;
  moveDown?: (placeId: string) => void;
};

/** The five squares of the tint row, in the design's order. Graphite means no tint was chosen, so it is never offered. */
const tintOrder = ['tide', 'rose', 'sage', 'sand', 'iris'] as const;
/** Spelled here, not imported, so this module loads under plain node tests; PlaceSwatch's table is the same words. */
const tintLabel: Record<typeof tintOrder[number], string> = { tide: 'Tide', rose: 'Rose', sage: 'Sage', sand: 'Sand', iris: 'Iris' };
const isTint = (id: string): id is typeof tintOrder[number] => (tintOrder as readonly string[]).includes(id);

/** The key chords that open a context menu from the keyboard: the context-menu key and Shift+F10. */
export const opensRailMenu = (event: { key: string; shiftKey: boolean }): boolean =>
  event.key === 'ContextMenu' || (event.key === 'F10' && event.shiftKey);

const act = (id: string, label: string, onSelect: () => void, extra: { shortcut?: string } = {}): Extract<MenuEntry, { kind?: 'action' }> => ({ id, label, onSelect, ...extra });

/** Groups are separated only when both neighbours are real, so a missing handler never leaves a stray line. */
export function railMenu(row: RailMenuRow, actions: RailMenuActions): MenuEntry[] {
  const groups: MenuEntry[][] = [[], [], []];
  if (actions.goTo) groups[0].push(act('go', 'Go to', () => actions.goTo?.(row.id), { shortcut: '↵' }));
  if (actions.quickLook) groups[0].push(act('quick-look', 'Quick Look', () => actions.quickLook?.(row.id), { shortcut: 'Space' }));
  if (actions.newWindow) groups[0].push(act('new-window', 'Open in new window', () => actions.newWindow?.(row.id), { shortcut: actions.newWindowShortcut }));
  if (row.pinned ? actions.unpin : actions.pin) {
    groups[1].push(row.pinned
      ? act('unpin', 'Unpin', () => actions.unpin?.(row.id))
      : act('pin', 'Pin', () => actions.pin?.(row.id)));
  }
  if (row.pinned && actions.moveUp) groups[1].push(act('up', 'Move up', () => actions.moveUp?.(row.id)));
  if (row.pinned && actions.moveDown) groups[1].push(act('down', 'Move down', () => actions.moveDown?.(row.id)));
  if (actions.startRename) groups[1].push(act('rename', 'Rename', () => actions.startRename?.(row.id)));
  if (actions.setTint) groups[1].push({
    kind: 'swatches', id: 'tint', label: 'Tint', icon: 'palette',
    options: tintOrder.map(tint => ({ id: tint, label: tintLabel[tint] })),
    selected: isTint(row.tint) ? row.tint : undefined,
    onSelect: id => { if (isTint(id)) actions.setTint?.(row.id, id); },
  });
  if (actions.close) groups[2].push(act('close', 'Close', () => actions.close?.(row.id), { shortcut: actions.closeShortcut }));
  if (actions.closeOthers) groups[2].push({ ...act('close-others', 'Close all others', () => actions.closeOthers?.(row.id)), disabled: actions.closeOthersDisabled });
  const entries: MenuEntry[] = [];
  groups.filter(group => group.length).forEach((group, index) => {
    if (index > 0) entries.push({ kind: 'separator', id: `sep-${index}` });
    entries.push(...group);
  });
  return entries;
}
