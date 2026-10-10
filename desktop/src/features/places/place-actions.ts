import type { MenuEntry } from '../../components/ui';
import { decideEntries, type DecideChange } from './PlaceMenuDecide';
import type { PlaceDecide } from './wire';
import { tintLabel, type TintName } from './components/PlaceSwatch';

/** Everything a Home can ask its owner to do. EVERY member is optional on purpose: a verb the owner has not wired is left off the menu and
 * off the page entirely, because a control that does nothing is worse than no control. The root wires these to the Places client
 * (run/places-routes-api.md) and to the native shell; this feature never imports either. A callback may return a promise; if it throws
 * (or rejects) the Home shows the error's sentence and nothing else changes. */
export type PlaceActions = {
  /** Navigation. */
  goTo?: (placeId: string) => void | Promise<void>;
  goToInNewWindow?: (placeId: string) => void | Promise<void>;
  /** Space on a tile: the owner draws a read-only Home sheet (HomeQuickLook) for this id. */
  quickLook?: (placeId: string) => void;
  openChat?: (chatId: string) => void | Promise<void>;
  openChatInNewTab?: (chatId: string) => void | Promise<void>;

  /** Writes. Each maps to exactly one published route. */
  /** `tint` is absent when the person chose none: the store then inherits (inside a place) or picks the least-used (top level). */
  create?: (draft: { name: string; tint?: TintName; parent?: string }) => void | Promise<void>;
  rename?: (placeId: string, name: string) => void | Promise<void>;
  setTint?: (placeId: string, tint: TintName) => void | Promise<void>;
  pin?: (placeId: string) => void | Promise<void>;
  unpin?: (placeId: string) => void | Promise<void>;
  /** "Always ask me" and "Decision confidence…": one PUT per choice, with Undo. */
  setDecide?: (placeId: string, change: DecideChange) => void | Promise<void>;
  archive?: (placeId: string) => void | Promise<void>;
  restore?: (placeId: string) => void | Promise<void>;
  /** The Home asks for the preview, shows it inline, and calls this only on the person's second click. */
  loadDeletePreview?: (placeId: string) => Promise<{ children: number; chatsHere: number; wouldBeUnplaced: readonly string[] }>;
  remove?: (placeId: string) => void | Promise<void>;
  /** Opens the owner's chooser ("Merge into…", "Add to another place…"): the Home has no place picker of its own (the Go to palette does). */
  chooseMergeTarget?: (placeId: string) => void;
  chooseAnotherParent?: (placeId: string) => void;
  /** Drag and drop onto a tile: add (default) or move (⌥). `ids` are chat ids or place ids. */
  file?: (drop: DropPayload, targetPlaceId: string, mode: 'add' | 'move') => void | Promise<void>;
  removeChat?: (chatId: string, fromPlaceId: string) => void | Promise<void>;
  chooseChatPlace?: (chatId: string) => void;

  /** Empty-place affordances and the root's engine-made suggestion. */
  addSources?: (placeId: string) => void;
  removeSource?: (placeId: string, sourceId: string) => void | Promise<void>;
  writeInstructions?: (placeId: string) => void;
  acceptSuggestion?: () => void | Promise<void>;
  /** "Not now" on the untouched-place suggestion: the engine hides that place's offer for 30 days, in every window. */
  snoozeStale?: (placeId: string) => void | Promise<void>;
  /** First launch: the native folder picker that turns a repo or folder into a place. Absent where the shell has none. */
  openFolderAsPlace?: () => void;
  retry?: () => void;
};

/** What a drag carries. Chats and places use their own type so a tile can tell them apart while the drag is still over it
 * (the browser hides the data until the drop, so the Home also keeps the dragged item in state). */
export type DropPayload = { kind: 'chat' | 'place'; ids: readonly string[] };
export const chatDragType = 'application/x-codeaf-chat';
export const placeDragType = 'application/x-codeaf-place';

export function writeDrag(event: { dataTransfer: DataTransfer }, payload: DropPayload): void {
  event.dataTransfer.setData(payload.kind === 'chat' ? chatDragType : placeDragType, JSON.stringify(payload.ids));
  // A plain-text copy lets a drop outside the app (or a test) see something sensible, never a hidden id format.
  event.dataTransfer.setData('text/plain', payload.ids.join('\n'));
  event.dataTransfer.effectAllowed = 'copyMove';
}

export function readDrag(event: { dataTransfer: DataTransfer }): DropPayload | undefined {
  for (const [kind, type] of [['chat', chatDragType], ['place', placeDragType]] as const) {
    const raw = event.dataTransfer.getData(type);
    if (!raw) continue;
    try {
      const ids: unknown = JSON.parse(raw);
      if (Array.isArray(ids) && ids.every(id => typeof id === 'string')) return { kind, ids };
    } catch { /* A foreign drag with the same type name is ignored, never trusted. */ }
  }
  return undefined;
}

/** ⌥ moves instead of adds (Places 8g "Organizing"). The tile drag owns the rule so a tile and any other surface cannot disagree. */
export { tileDropMode as dropMode } from './home/tileDnd';

/** A tile accepts a drop when the owner wired `file`, and never from itself. A place dropped on its own child would be a cycle: the
 * store refuses that with a sentence the Home shows, so it is not pre-judged here. */
export function canDropOn(payload: DropPayload | undefined, targetPlaceId: string, actions: PlaceActions): boolean {
  if (!payload || !actions.file) return false;
  return !(payload.kind === 'place' && payload.ids.includes(targetPlaceId));
}

export type MenuPlace = { id: string; name: string; tint: TintName; pinned?: boolean; archived?: boolean;
  /** How the place decides, as the engine said it; absent draws neither decision entry. */
  decide?: PlaceDecide };
export type MenuContext = {
  /** Offline or loading: every write is dropped from the menu, navigation stays. */
  readOnly?: boolean;
  /** The Home being viewed is this place: Go to and Quick Look make no sense for it. */
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

/** Places 8f draws the five squares in this order. Graphite is never among them: it means no tint was chosen. */
const menuTintOrder = ['tide', 'rose', 'sage', 'sand', 'iris'] as const;
const isMenuTint = (id: string): id is typeof menuTintOrder[number] => (menuTintOrder as readonly string[]).includes(id);

/** The place menu of Places 8f, in the design's order. A verb without a handler is absent. Separators only ever sit between two real
 * entries. Tint is the inline swatch row under the word, not a flyout. */
export function placeMenu(place: MenuPlace, actions: PlaceActions, context: MenuContext = {}): MenuEntry[] {
  const writes = !context.readOnly;
  const groups: MenuEntry[][] = [[], [], [], [], []];
  if (!context.current) {
    if (actions.goTo) groups[0].push(action('go', 'Go to', () => void actions.goTo?.(place.id), { shortcut: '↵' }));
    if (actions.quickLook) groups[0].push(action('quick-look', 'Quick Look', () => actions.quickLook?.(place.id), { shortcut: 'Space' }));
    if (actions.goToInNewWindow) groups[0].push(action('new-window', 'Open in new window', () => void actions.goToInNewWindow?.(place.id), { shortcut: context.newWindowHint ?? '⌘↵' }));
  }
  if (writes) {
    if (context.canRename && context.startRename) groups[1].push(action('rename', 'Rename', () => context.startRename?.(place.id)));
    if (actions.setTint) groups[1].push({
      kind: 'swatches', id: 'tint', label: 'Tint', icon: 'palette',
      options: menuTintOrder.map(tint => ({ id: tint, label: tintLabel[tint] })),
      selected: isMenuTint(place.tint) ? place.tint : undefined,
      onSelect: id => { if (isMenuTint(id)) void actions.setTint?.(place.id, id); },
    });
    if (actions.chooseAnotherParent) groups[1].push(action('another-parent', 'Add to another place…', () => actions.chooseAnotherParent?.(place.id)));
    if (actions.chooseMergeTarget) groups[1].push(action('merge', 'Merge into…', () => actions.chooseMergeTarget?.(place.id)));
    if (!place.archived) groups[2].push(...decideEntries(place.id, place.decide, actions.setDecide));
    if (!place.archived) {
      if (place.pinned && actions.unpin) groups[3].push(action('unpin', 'Unpin from rail', () => void actions.unpin?.(place.id)));
      else if (!place.pinned && actions.pin) groups[3].push(action('pin', 'Pin to rail', () => void actions.pin?.(place.id)));
      if (actions.archive) groups[3].push(action('archive', 'Archive', () => void actions.archive?.(place.id)));
    } else if (actions.restore) groups[3].push(action('restore', 'Restore', () => void actions.restore?.(place.id)));
    if (actions.remove && context.startDelete) groups[4].push(action('delete', 'Delete place…', () => context.startDelete?.(place.id), { danger: true }));
  }
  const entries: MenuEntry[] = [];
  groups.filter(group => group.length).forEach((group, index) => {
    if (index > 0) entries.push({ kind: 'separator', id: `sep-${index}` });
    entries.push(...group);
  });
  return entries;
}

/** The chat row menu: open, open in a new tab, and (inside a place) take it out of this place. */
export function chatMenu(chatId: string, actions: PlaceActions, context: { readOnly?: boolean; inPlaceId?: string } = {}): MenuEntry[] {
  const groups: MenuEntry[][] = [[], []];
  if (actions.openChat) groups[0].push(action('open', 'Open', () => void actions.openChat?.(chatId), { shortcut: '↵' }));
  if (actions.openChatInNewTab) groups[0].push(action('open-tab', 'Open in new tab', () => void actions.openChatInNewTab?.(chatId)));
  if (!context.readOnly) {
    if (actions.chooseChatPlace) groups[1].push(action('place', 'Add to a place…', () => actions.chooseChatPlace?.(chatId)));
    if (actions.removeChat && context.inPlaceId) groups[1].push(action('remove', 'Remove from this place', () => void actions.removeChat?.(chatId, context.inPlaceId as string)));
  }
  const entries: MenuEntry[] = [];
  groups.filter(group => group.length).forEach((group, index) => {
    if (index > 0) entries.push({ kind: 'separator', id: `sep-${index}` });
    entries.push(...group);
  });
  return entries;
}

/** The ⋯ menu of the place being viewed (the Home heading). The root and Now have none: they are not places that can be renamed or deleted. */
export function homeMenu(place: MenuPlace, actions: PlaceActions, context: MenuContext = {}): MenuEntry[] {
  return placeMenu(place, actions, { ...context, current: true });
}
