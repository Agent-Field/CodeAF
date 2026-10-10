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

/**
 * Browser previews have no OS notification delivery and never request notification permission.
 * The list is the chat world feed's, the same one the Inbox reads. The newer world client
 * rejects that feed (it wants chatId and a numeric needs-you count), so posting from it
 * never named a question or a failure.
 */
export function connectAttentionNotifications(): () => void {
 let disposed = false;
 let stop: (() => void) | undefined;
 void Promise.all([
  import('@tauri-apps/api/core'),
  import('../../design/nativeControls.ts'),
  import('../../features/chat/world-store.ts'),
  import('../../features/tabs/closing/signals.ts'),
  import('../../features/tabs/closing/failedSeen.ts'),
 ]).then(([core, controls, store, signals, seen]) => {
  if (disposed || !core.isTauri()) return;
  const native = controls.nativeControls();
  let last = '';
  const post = () => {
   const state = store.worldStore.getState();
   if (state.status !== 'live' || !state.epoch || !(state.seq > 0)) return;
   const items = signals.attentionSignals(state, seen.readFailedSeen(), Date.now());
   const count = state.rows.reduce((total, row) => total + (!row.archived && row.needsYou ? 1 : 0), 0);
   const key = `${state.epoch}:${state.seq}:${count}:${items.map(item => item.id).join('\n')}`;
   if (key === last) return;
   last = key;
   // Rust announces only while no window is focused. The call still names the reading it came from.
   void native.notifyAttention(items, state.seq, state.epoch).catch(() => undefined);
   void native.setBadge(count, state.seq, state.epoch).catch(() => undefined);
  };
  const unsubscribe = store.worldStore.subscribe(post);
  post();
  stop = unsubscribe;
 }).catch(() => undefined);
 return () => { disposed = true; stop?.(); };
}
