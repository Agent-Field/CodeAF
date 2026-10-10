import type { WorldClient } from '../../features/world/worldClient.ts';
import type { AttentionItem as NativeItem } from '../../design/nativeControls.ts';
import { shouldNotify, totalNeedsYou, notificationAttention } from '../../features/nextup/notifyFilter.ts';
import { groupNotifications, type NotifyDirectory } from '../../features/places/notifyGroup.ts';

export const focusAttentionEvent = 'codeaf:focus-attention';

/** Rust focuses the destination window before its activation is claimed. */
export function dispatchAttentionFocus(itemId: string, target: EventTarget = window): void {
 target.dispatchEvent(new CustomEvent(focusAttentionEvent, { detail: { itemId } }));
}
export type NotificationPort = {
 focused(): Promise<boolean>;
 permission(ask: boolean): Promise<boolean>;
 post(items: readonly NativeItem[], seq: number, epoch?: string): Promise<unknown>;
 badge(count: number, seq: number, epoch?: string): Promise<unknown>;
};
type Source = Pick<WorldClient, 'attention' | 'cursor' | 'subscribe'> & { lastPlaces(): NotifyDirectory | undefined | Promise<NotifyDirectory | undefined> } & { rows(): readonly { chatId: string; needsYou: number; archived: boolean; title?: string }[] };

/** Native delivery consumes the same world store as the views, including chats with no open tab. */
export function createAttentionNotifications(source: Source, port: NotificationPort) {
 const seen = new Set<string>();
 let permission: Promise<boolean> | undefined;
 let queue = Promise.resolve();
 let stopped = false;
 let lastBadge: number | undefined;
 let disconnect: (() => void) | undefined;

 function update(): Promise<void> {
  if (!source.cursor().epoch) return queue;
  queue = queue.then(async () => {
   if (stopped) return;
   const items = [...new Map(source.attention().map(item => [item.id, item])).values()];
   const rows = source.rows();
   // Capture the cursor before any directory read so a newer feed cannot label an older list.
   const cursor = source.cursor();
   const places = await source.lastPlaces();
   // A disk-only attention row can stand for several questions, so the badge uses the engine's counts.
   const count = rows.reduce((total, row) => total + (row.archived ? 0 : row.needsYou), 0);
   const fresh = items.filter(item => !seen.has(item.id));
   for (const item of items) seen.add(item.id);
   if (count !== lastBadge) {
    await port.badge(count, cursor.seq, cursor.epoch).catch(() => undefined);
    lastBadge = count;
   }
   const focused = await port.focused();
   const eligible = fresh.filter(item => shouldNotify(item));
   if (!focused && eligible.length) {
    // Cache the request itself, so simultaneous questions cannot open multiple permission prompts.
    permission ??= port.permission(false).then(granted => granted || port.permission(true)).catch(() => false);
    if (!await permission || stopped || await port.focused()) return;
   }
   const active = new Set(source.attention().map(item => item.id));
   const announced = new Set((focused ? [] : eligible).filter(item => active.has(item.id)).map(item => item.id));
   // The native book also needs the full pending set to keep old notification clicks accurate.
   // The thread is the place group: one id and title per place, Now when the chat is unplaced.
   const threads = new Map(groupNotifications(items, places).map(notice => [notice.id, notice.thread]));
   const pending: NativeItem[] = items.filter(item => active.has(item.id) && shouldNotify(item)).map(item => {
    const thread = threads.get(item.id);
    return {
     id: item.id, chatId: item.chatId, kind: item.kind === 'failed' ? 'failed' : 'needsYou',
     chatTitle: item.title ?? rows.find(row => row.chatId === item.chatId)?.title ?? '', text: item.head ?? '',
     ...(thread ? { placeId: thread.id, ...(thread.title ? { placeName: thread.title } : {}) } : {}),
     silent: !announced.has(item.id),
    };
   });
   if (!stopped) await port.post(pending, cursor.seq, cursor.epoch);
  }).catch(() => undefined);
  return queue;
 }
 return {
  update,
  start() { if (!disconnect && !stopped) { disconnect = source.subscribe(() => void update()); void update(); } },
  stop() { stopped = true; disconnect?.(); disconnect = undefined; },
 };
}

/** Browser previews have no OS notification delivery and never request notification permission. */
export function connectAttentionNotifications(): () => void {
 let disposed = false;
 let stop: (() => void) | undefined;
 void Promise.all([
  import('@tauri-apps/api/core'), import('@tauri-apps/api/window'),
  import('../../design/nativeControls.ts'), import('../../features/chat/world-store.ts'), import('../../features/places/client.ts'),
 ]).then(([core, windows, controls, world, placesClient]) => {
  if (disposed || !core.isTauri()) return;
  const native = controls.nativeControls();
  // The native adapter reads the same engine feed as Next up, preserving its blocking metadata.
  const directoryClient = placesClient.createPlacesClient();
  const source: Source = {
   attention: () => notificationAttention(world.worldStore.getState().items, world.worldStore.getState().rows),
   rows: () => {
    const state = world.worldStore.getState();
    return [{ chatId: '', needsYou: totalNeedsYou(state.items, state.rows), archived: false }];
   },
   lastPlaces: async () => {
    const state = world.worldStore.getState();
    const nodes = state.items.flatMap(item => (item.placeIds ?? []).map((id, index) => ({ id, name: item.placeNames?.[index] ?? '' })));
    const members = state.items.flatMap(item => (item.placeIds ?? []).map(placeId => ({ placeId, chatId: item.session })));
    // Failures have no question metadata, so their actual filing comes from the Places door.
    const failures = state.rows.filter(row => !row.archived && (row.unseenFailed ?? row.failed) > 0 && row.failure?.at);
    await Promise.all(failures.map(async row => {
     const answer = await directoryClient.chatPlaces(row.session).catch(() => undefined);
     for (const place of answer?.places ?? []) {
      nodes.push({ id: place.id, name: place.name });
      members.push({ chatId: row.session, placeId: place.id });
     }
    }));
    return { nodes, members };
   },
   cursor: () => { const { seq, epoch } = world.worldStore.getState(); return { seq, epoch }; },
   subscribe: world.worldStore.subscribe,
  };
  const controller = createAttentionNotifications(source, {
   focused: async () => (await native.listWindows()).some(window => window.focused) || await windows.getCurrentWindow().isFocused(),
   permission: async ask => (await native.notificationPermission(ask)).state === 'granted',
   post: (items, seq, epoch) => native.notifyAttention(items, seq, epoch),
   badge: (count, seq, epoch) => native.setBadge(count, seq, epoch),
  });
  controller.start(); stop = controller.stop;
 }).catch(() => undefined);
 return () => { disposed = true; stop?.(); };
}
