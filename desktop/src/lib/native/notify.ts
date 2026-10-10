import type { AttentionItem } from '../../features/world/types.ts';
import type { WorldClient } from '../../features/world/worldClient.ts';
import type { AttentionItem as NativeItem } from '../../design/nativeControls.ts';

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
type Source = Pick<WorldClient, 'attention' | 'rows' | 'lastPlaces' | 'cursor' | 'subscribe'>;
const needsYou = (item: AttentionItem) => item.kind === 'question' || item.kind === 'approval';

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
   const places = source.lastPlaces();
   const cursor = source.cursor();
   // A disk-only attention row can stand for several questions, so the badge uses the engine's counts.
   const count = rows.reduce((total, row) => total + (row.archived ? 0 : row.needsYou), 0);
   const fresh = items.filter(item => !seen.has(item.id));
   for (const item of items) seen.add(item.id);
   if (count !== lastBadge) {
    await port.badge(count, cursor.seq, cursor.epoch).catch(() => undefined);
    lastBadge = count;
   }
   const focused = await port.focused();
   const eligible = fresh.filter(item => (needsYou(item) && item.blocking !== false) || item.kind === 'failed');
   if (!focused && eligible.length) {
    // Cache the request itself, so simultaneous questions cannot open multiple permission prompts.
    permission ??= port.permission(false).then(granted => granted || port.permission(true)).catch(() => false);
    if (!await permission || stopped || await port.focused()) return;
   }
   const active = new Set(source.attention().map(item => item.id));
   const announced = new Set((focused ? [] : eligible).filter(item => active.has(item.id)).map(item => item.id));
   // The native book also needs the full pending set to keep old notification clicks accurate.
   const pending: NativeItem[] = items.filter(item => active.has(item.id) && (needsYou(item) || item.kind === 'failed')).map(item => {
    const placeId = places?.members?.find(member => member.chatId === item.chatId)?.placeId;
    const placeName = places?.nodes.find(place => place.id === placeId)?.name;
    return {
     id: item.id, chatId: item.chatId, kind: item.kind === 'failed' ? 'failed' : 'needsYou',
     chatTitle: item.title ?? rows.find(row => row.chatId === item.chatId)?.title ?? '', text: item.head ?? '',
     ...(placeId ? { placeId, placeName } : {}), silent: !announced.has(item.id),
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
  import('../../design/nativeControls.ts'), import('../../features/world/worldClient.ts'),
 ]).then(([core, windows, controls, world]) => {
  if (disposed || !core.isTauri()) return;
  const native = controls.nativeControls();
  const controller = createAttentionNotifications(world.worldClient, {
   focused: async () => (await native.listWindows()).some(window => window.focused) || await windows.getCurrentWindow().isFocused(),
   permission: async ask => (await native.notificationPermission(ask)).state === 'granted',
   post: (items, seq, epoch) => native.notifyAttention(items, seq, epoch),
   badge: (count, seq, epoch) => native.setBadge(count, seq, epoch),
  });
  controller.start(); stop = controller.stop;
 }).catch(() => undefined);
 return () => { disposed = true; stop?.(); };
}
