// Boot owns the window's address and label. This store derives its current place from that key and the real graph,
// remembers navigation for this label alone, and never treats an unread graph as evidence that a place was deleted.
import { useSyncExternalStore } from 'react';
import { isPlaceKey, type PlaceKey } from '../../design/nativeControls.ts';
import { windowPlace, windowPlaceStorageKey } from '../../lib/native/windowPlace.ts';
import { placesStore, type PlacesState } from './shell/placesStore.ts';

export type WindowPlaceSource = {
  getState(): Pick<PlacesState, 'status' | 'graph'>;
  subscribe(listener: () => void): () => void;
};
export type WindowPlaceStorage = Pick<Storage, 'getItem' | 'setItem'>;
export type WindowPlaceOptions = {
  label: string;
  /** The key already read by boot, including an explicit window_open destination. */
  initialPlaceKey?: PlaceKey;
  storage?: WindowPlaceStorage;
  places?: WindowPlaceSource;
};

function browserStorage(): WindowPlaceStorage | undefined {
  try { return typeof localStorage === 'undefined' ? undefined : localStorage; } catch { return undefined; }
}

export function createWindowPlaceStore(options: WindowPlaceOptions) {
  const source = options.places ?? placesStore;
  const storage = options.storage ?? browserStorage();
  const storageKey = windowPlaceStorageKey(options.label);
  let saved: unknown;
  try { saved = storage?.getItem(storageKey); } catch { /* Navigation still works when storage is unavailable. */ }
  let current: PlaceKey = isPlaceKey(options.initialPlaceKey) ? options.initialPlaceKey : isPlaceKey(saved) ? saved : 'now';
  const listeners = new Set<() => void>();
  let unsubscribe: (() => void) | undefined;

  const remember = () => {
    try { storage?.setItem(storageKey, current); } catch { /* Forgetting a place is not worth an interruption. */ }
  };
  const resolve = (key: PlaceKey): PlaceKey => {
    if (key === 'now' || key === 'root') return key;
    const state = source.getState();
    if (state.status !== 'ready' || !state.graph) return key;
    return state.graph.places.some(place => place.id === key && !place.archived) ? key : 'now';
  };
  const setPlace = (key: PlaceKey) => {
    if (!isPlaceKey(key)) throw new Error('That is not a place codeaf knows');
    const next = resolve(key);
    if (next === current) return;
    current = next;
    remember();
    for (const listener of [...listeners]) listener();
  };
  current = resolve(current);
  remember();

  return {
    label: options.label,
    getState: () => current,
    setPlace,
    subscribe(listener: () => void): () => void {
      listeners.add(listener);
      if (listeners.size === 1) {
        unsubscribe = source.subscribe(() => setPlace(current));
        // A change while there were no readers must be applied before the first reader renders again.
        setPlace(current);
      }
      let active = true;
      return () => {
        if (!active) return;
        active = false;
        listeners.delete(listener);
        if (listeners.size === 0) { unsubscribe?.(); unsubscribe = undefined; }
      };
    },
  };
}

export type WindowPlaceStore = ReturnType<typeof createWindowPlaceStore>;
let defaultStore: WindowPlaceStore | undefined;

/** The boot key is consumed once; React reads and subscribes to the same store for this window. */
export function useWindowPlace(store?: WindowPlaceStore): PlaceKey {
  if (!store) {
    if (!defaultStore) {
      const boot = windowPlace();
      defaultStore = createWindowPlaceStore({ label: boot.label, initialPlaceKey: boot.placeKey });
    }
    store = defaultStore;
  }
  return useSyncExternalStore(store.subscribe, store.getState, store.getState);
}
