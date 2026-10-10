import type { MenuEntry } from '../../components/ui';
import type { DecideChange } from './PlaceMenuDecide';
import type { TintName } from './components/PlaceSwatch';
export { homeMenu, placeMenu, type MenuContext, type MenuPlace } from './menus/placeMenu';

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

/** ⌥ moves instead of adds (Places 8g "Organizing"). */
export const dropMode = (event: { altKey: boolean }): 'add' | 'move' => (event.altKey ? 'move' : 'add');

/** A tile accepts a drop when the owner wired `file`, and never from itself. A place dropped on its own child would be a cycle: the
 * store refuses that with a sentence the Home shows, so it is not pre-judged here. */
export function canDropOn(payload: DropPayload | undefined, targetPlaceId: string, actions: PlaceActions): boolean {
  if (!payload || !actions.file) return false;
  return !(payload.kind === 'place' && payload.ids.includes(targetPlaceId));
}

const action = (id: string, label: string, onSelect: () => void, extra: Partial<Extract<MenuEntry, { kind?: 'action' }>> = {}): MenuEntry => ({ id, label, onSelect, ...extra });

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
