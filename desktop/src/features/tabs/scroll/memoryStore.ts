import { useSyncExternalStore } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { namedScrollMemoryKey, publicScrollName, ScrollMemory, type ScrollSpot } from './scrollMemory';
import { browserNamespace, nativeNamespace, storageKeyFor, touchNamespace } from './scrollStorage';
import { createScrollSaveThrottle } from './saveThrottle';

// The one live memory of THIS window, persisted under a window-local key (scrollStorage.ts). A browser knows its namespace at once; a native
// window must ask the shell for its label, so the memory is undefined for a moment and restores wait for it.

let memory: ScrollMemory | undefined;
let key = '';
const listeners = new Set<() => void>();

function open(namespace: string) {
  key = storageKeyFor(namespace);
  let text: string | null = null;
  try { text = localStorage.getItem(key); touchNamespace(localStorage, namespace); } catch { /* Storage can be blocked; the memory then lives for this load only. */ }
  memory = ScrollMemory.parse(text);
  listeners.forEach(listener => listener());
}

function save() {
  if (!memory) return;
  try { localStorage.setItem(key, memory.serialize()); } catch { /* Storage can be full or blocked; the position is a convenience. */ }
}

const saveThrottle = createScrollSaveThrottle(save);
export const scheduleSave = saveThrottle.schedule;

if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', saveThrottle.flush);
  const fallback = () => open(browserNamespace(typeof sessionStorage === 'undefined' ? undefined : sessionStorage));
  if (isTauri()) {
    // The shell names each window ("main", "w-2"); that label survives a relaunch, so positions do too.
    import('../../../design/nativeControls')
      .then(({ createNativeControls }) => createNativeControls().currentWindow())
      .then(context => open(nativeNamespace(context.label)), fallback);
  } else fallback();
}

const subscribe = (listener: () => void) => { listeners.add(listener); return () => { listeners.delete(listener); }; };

/** The window's memory, or undefined while a native window is still learning its label. */
export const useScrollMemory = () => useSyncExternalStore(subscribe, () => memory);

/**
 * Reconciles the memory with the panes that exist: `alive` are in the strip; `retained`, when the workspace passes it, are the panes of its closed
 * ring (Reopen). Without it, panes that left are kept in a bounded ring of their own. An empty `alive` is ignored so a transient empty list never wipes the memory.
 */
export function pruneScrollMemory(alive: ReadonlySet<string>, retained?: ReadonlySet<string>): void {
  if (memory && alive.size > 0 && memory.prune(alive, retained)) scheduleSave();
}

/** For diagnostics and tests. */
export const scrollMemorySize = () => memory?.size ?? 0;

/**
 * Named scrollers of one pane, in the shape a focus step stores.
 * An empty list means this pane has nothing remembered yet: the caller omits it so a later note is not wiped.
 */
export function namedScrollSpots(paneId: string): { key: string; top: number; left: number; end: boolean }[] {
  if (!memory) return [];
  const spots: { key: string; top: number; left: number; end: boolean }[] = [];
  for (const [raw, spot] of memory.get(paneId)) {
    const key = publicScrollName(raw);
    if (key) spots.push({ key, top: spot.top, left: spot.left, end: spot.end });
  }
  return spots;
}

/**
 * Puts a focus step's spots back before the pane mounts. A place switch prunes every pane that is not on the
 * strip being shown, so the step is the only copy left; writing here is what lets the restore loop see them.
 */
export function restoreNamedScroll(paneId: string, spots: readonly { key: string; top: number; left: number; end: boolean }[]): void {
  if (!memory || spots.length === 0) return;
  for (const spot of spots) {
    const key = namedScrollMemoryKey(spot.key);
    if (!key) continue;
    const next: ScrollSpot = { top: spot.top, left: spot.left, end: spot.end };
    memory.set(paneId, key, next);
  }
  scheduleSave();
}
