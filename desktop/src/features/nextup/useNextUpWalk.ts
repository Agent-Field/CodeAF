// A walk belongs to the window, so it survives the workspace remount at a place switch.
// The feed removes vanished questions in one publication; no empty intermediate card is shown.
import { useEffect, useRef, useSyncExternalStore } from 'react';
import { registerShortcuts, shortcutLayer } from '../../design/keyboard.ts';
import type { Pane } from '../tabs/model.ts';
import type { ScrollSpot } from '../tabs/scroll/scrollMemory.ts';
import type { EngineAnswer } from '../chat/engine-client.ts';
import type { AttentionItem } from '../chat/world-client.ts';
import { worldStore } from '../chat/world-store.ts';
import { nextUpQueue, type NextUpProgress } from './model.ts';

export type WalkOrigin = { place: string; tabId: string; paneId: string; label: string; conversation: string; draft?: string; route?: Pane['route']; scroll?: Map<string, ScrollSpot> };
export type WalkState = {
 phase: 'idle' | 'walking' | 'clear' | 'returning';
 origin?: WalkOrigin;
 item?: AttentionItem;
 progress?: NextUpProgress;
 answered: number;
};

export function createNextUpWalk() {
 let state: WalkState = { phase: 'idle', answered: 0 };
 let keys: string[] = [];
 let total = 0;
 const known = new Map<string, AttentionItem>();
 const answered = new Set<string>();
 const listeners = new Set<() => void>();
 const publish = (next: WalkState) => { state = next; listeners.forEach(listener => listener()); };
 function current(items: readonly AttentionItem[]) {
  const pending = new Map(items.filter(item => !item.decidedBy && !answered.has(item.key)).map(item => [item.key, item]));
  keys = keys.filter(key => pending.has(key));
  const item = pending.get(keys[0]);
  publish({ ...state, phase: item ? 'walking' : 'clear', item, progress: item ? { index: total - keys.length + 1, total } : undefined, answered: answered.size });
 }
 return {
  getSnapshot: () => state,
  subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  start(origin: WalkOrigin, items: readonly AttentionItem[], itemKey?: string) {
   const queue = nextUpQueue({ items: items.filter(item => !item.decidedBy && (item.session !== origin.conversation || item.key === itemKey)) }).items;
   // A stale notification must not open a different question in its place.
   if (!queue.length || (itemKey && !queue.some(item => item.key === itemKey))) return;
   if (state.phase === 'idle') { answered.clear(); known.clear(); state = { phase: 'walking', origin, answered: 0 }; }
   keys = queue.filter(item => !answered.has(item.key)).map(item => item.key);
   if (itemKey) keys = [itemKey, ...keys.filter(key => key !== itemKey)];
   for (const item of queue) known.set(item.key, item);
   total = keys.length + answered.size;
   current(items);
  },
  update(items: readonly AttentionItem[]) {
   if (state.phase !== 'walking') return;
   const live = items.find(item => item.key === state.item?.key && !item.decidedBy);
   if (!live) current(items);
  },
  acknowledge(session: string, answer: Pick<EngineAnswer, 'kind' | 'id'>, items: readonly AttentionItem[]) {
   if (state.phase === 'idle' || state.phase === 'returning') return;
   const item = [...known.values()].find(item => item.session === session && item.kind === answer.kind && item.id === answer.id);
   if (!item || answered.has(item.key)) return;
   answered.add(item.key);
   current(items);
  },
  skip(items: readonly AttentionItem[]) {
   if (state.phase !== 'walking' || !keys.length) return;
   keys.push(keys.shift()!);
   current(items);
  },
  exit() { if (state.origin && state.phase !== 'idle') publish({ ...state, phase: 'returning', item: undefined, progress: undefined }); },
  returned() { keys = []; known.clear(); answered.clear(); publish({ phase: 'idle', answered: 0 }); },
 };
}

export const nextUpWalk = createNextUpWalk();
export const useNextUpWalkState = () => useSyncExternalStore(nextUpWalk.subscribe, nextUpWalk.getSnapshot, nextUpWalk.getSnapshot);

/** The workspace supplies its canonical navigation, which also sets the frame tint and rail selection. */
export function useNextUpWalk(options: {
 enabled: boolean;
 origin: () => WalkOrigin;
 place: string;
 goTo: (place: string) => Promise<void>;
 open: (item: AttentionItem) => void;
 restore: (origin: WalkOrigin) => void;
 warn: (reason: unknown) => void;
}) {
 const latest = useRef(options);
 latest.current = options;
 const state = useNextUpWalkState();
 useEffect(() => options.enabled ? registerShortcuts(shortcutLayer.surface + 1, shortcut => {
  if (shortcut.id === 'next-up') { nextUpWalk.start(latest.current.origin(), worldStore.getState().items); return true; }
  if (shortcut.id === 'back' && nextUpWalk.getSnapshot().phase !== 'idle') { nextUpWalk.exit(); return true; }
  return false;
 }) : undefined, [options.enabled]);
 useEffect(() => {
  if (!options.enabled) return;
  const start = (event: Event) => {
   const detail = (event as CustomEvent<{ itemKey?: string; itemId?: string }>).detail;
   nextUpWalk.start(latest.current.origin(), worldStore.getState().items, detail?.itemKey ?? detail?.itemId);
  };
  const escape = (event: KeyboardEvent) => {
   if (event.key !== 'Escape' || event.defaultPrevented || event.isComposing || nextUpWalk.getSnapshot().phase === 'idle') return;
   // Modal overlays keep their own dismissal before the walk returns.
   if (document.querySelector('dialog[open], [role="menu"]')) return;
   event.preventDefault(); nextUpWalk.exit();
  };
  window.addEventListener('codeaf:next-up-start', start);
  window.addEventListener('codeaf:focus-attention', start);
  window.addEventListener('keydown', escape);
  const stop = worldStore.subscribe(() => nextUpWalk.update(worldStore.getState().items));
  nextUpWalk.update(worldStore.getState().items);
  return () => { stop(); window.removeEventListener('codeaf:next-up-start', start); window.removeEventListener('codeaf:focus-attention', start); window.removeEventListener('keydown', escape); };
 }, [options.enabled]);
 const itemKey = state.item?.key;
 const landed = useRef<string | undefined>(undefined);
 useEffect(() => {
  if (!options.enabled) return;
  if (state.phase !== 'walking') landed.current = undefined;
  const origin = state.origin;
  if (state.phase === 'returning' && origin) {
   if (options.place !== origin.place) { void options.goTo(origin.place).catch(options.warn); return; }
   options.restore(origin); nextUpWalk.returned(); return;
  }
  const item = state.item;
  if (state.phase !== 'walking' || !item) return;
  const place = item.placeIds?.includes(options.place) ? options.place : item.placeIds?.[0] ?? 'now';
  if (options.place !== place) { void options.goTo(place).catch(options.warn); return; }
  const destination = `${options.place}:${item.key}`;
  // Strict Mode replays effects; one arrival must still open only one tab.
  if (landed.current === destination) return;
  landed.current = destination;
  options.open(item);
 }, [options.enabled, options.place, state.phase, itemKey]);
 return state;
}
