import { useSyncExternalStore } from 'react';
import { isTauri } from '@tauri-apps/api/core';
import { ScrollMemory } from './scrollMemory';
import { browserNamespace, nativeNamespace, storageKeyFor, touchNamespace } from './scrollStorage';

// The one live memory of THIS window, persisted under a window-local key (scrollStorage.ts). A browser knows its namespace at once; a native
// window must ask the shell for its label, so the memory is undefined for a moment and restores wait for it.

const SAVE_DELAY_MS = 300;

let memory: ScrollMemory | undefined;
let key = '';
let saveTimer: ReturnType<typeof setTimeout> | undefined;
const listeners = new Set<() => void>();

function open(namespace: string) {
  key = storageKeyFor(namespace);
  let text: string | null = null;
  try { text = localStorage.getItem(key); touchNamespace(localStorage, namespace); } catch { /* Storage can be blocked; the memory then lives for this load only. */ }
  memory = ScrollMemory.parse(text);
  listeners.forEach(listener => listener());
}

function save() {
  saveTimer = undefined;
  if (!memory) return;
  try { localStorage.setItem(key, memory.serialize()); } catch { /* Storage can be full or blocked; the position is a convenience. */ }
}

export function scheduleSave() {
  if (saveTimer === undefined) saveTimer = setTimeout(save, SAVE_DELAY_MS);
}

if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', () => { if (saveTimer !== undefined) { clearTimeout(saveTimer); save(); } });
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
