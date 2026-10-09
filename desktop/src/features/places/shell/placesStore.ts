// The window's one reading of the place graph. The rail, the Go to chooser, every Home pane and the window tint read
// it through usePlaces(); none of them fetch the graph themselves and none of them run a timer.
//
// It is refreshed by three things only: the first subscriber, a change on the engine-wide world stream (a chat started,
// finished or began waiting, which moves the roll-ups), and the window coming back into focus (another window may have
// written). Every write this window makes refreshes it too, through `afterWrite`. The world stream carries no `places`
// record yet, so a write made in ANOTHER window reaches this one on focus or on the next world change: that is a stated
// gap (run/places-shell.md), not a poll in disguise.

import { useSyncExternalStore } from 'react';
import { worldStore } from '../../chat/world-store.ts';
import { createPlacesClient, PlacesError, type PlacesClient, type PlacesGraph } from '../client.ts';

export type PlacesStatus = 'idle' | 'loading' | 'ready' | 'offline' | 'unavailable' | 'error';
export type PlacesState = {
  status: PlacesStatus;
  /** The last good graph, archived places included. Kept while offline so the rail stays readable. */
  graph?: PlacesGraph;
  /** The engine's sentence when the last read failed. */
  error?: string;
  /** Moves on every successful read, so a Home pane knows to read its digest again. */
  version: number;
};

type Options = {
  client?: PlacesClient;
  /** Debounce for world-stream changes; one read per burst of presence updates. */
  settleMs?: number;
  world?: Pick<typeof worldStore, 'subscribe' | 'getState'>;
};

export function createPlacesStore(options: Options = {}) {
  const client = options.client ?? createPlacesClient();
  const settle = options.settleMs ?? 600;
  const world = options.world ?? worldStore;
  let state: PlacesState = { status: 'idle', version: 0 };
  const listeners = new Set<() => void>();
  let refs = 0;
  let inflight: Promise<void> | undefined;
  let again = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  let unworld: (() => void) | undefined;
  let lastSeq = -1;

  const set = (next: PlacesState) => { state = next; for (const listener of [...listeners]) listener(); };

  async function read(): Promise<void> {
    if (inflight) { again = true; return inflight; }
    // Only the first read says "loading": a retry after a failure keeps the failure on screen until it has an answer,
    // so the rail's notice and its Retry do not blink out under the pointer on every focus or world change.
    if (state.status === 'idle') set({ ...state, status: 'loading' });
    inflight = (async () => {
      try {
        const graph = await client.graph({ archived: true });
        set({ status: 'ready', graph, version: state.version + 1 });
      } catch (failure) {
        const error = failure instanceof Error ? failure.message : 'Places could not be read.';
        // A bridge without the Places routes answers 404: Places are then absent, not broken.
        const status: PlacesStatus = failure instanceof PlacesError && failure.unreachable ? 'offline' : failure instanceof PlacesError && failure.status === 404 ? 'unavailable' : 'error';
        if (status !== state.status || error !== state.error) set({ ...state, status, error });
      } finally {
        inflight = undefined;
      }
      if (again) { again = false; await read(); }
    })();
    return inflight;
  }

  const onWorld = () => {
    const seq = world.getState().seq;
    if (seq === lastSeq) return;
    lastSeq = seq;
    clearTimeout(timer);
    timer = setTimeout(() => void read(), settle);
  };
  const onFocus = () => { if (typeof document === 'undefined' || document.visibilityState !== 'hidden') void read(); };

  function start() {
    void read();
    unworld = world.subscribe(onWorld);
    if (typeof window !== 'undefined') { window.addEventListener('focus', onFocus); document.addEventListener('visibilitychange', onFocus); }
  }
  function stop() {
    unworld?.(); unworld = undefined;
    clearTimeout(timer);
    if (typeof window !== 'undefined') { window.removeEventListener('focus', onFocus); document.removeEventListener('visibilitychange', onFocus); }
  }

  return {
    client,
    getState: () => state,
    subscribe(listener: () => void): () => void {
      listeners.add(listener);
      if (++refs === 1) start();
      let done = false;
      return () => {
        if (done) return;
        done = true;
        listeners.delete(listener);
        if (--refs === 0) stop();
      };
    },
    /** Read again now: Retry, or after a write. Resolves when the graph is current. */
    refresh: read,
  };
}

export type PlacesStore = ReturnType<typeof createPlacesStore>;

/** The window's single store and client. */
export const placesStore = createPlacesStore();

export function usePlaces(store: PlacesStore = placesStore): PlacesState {
  return useSyncExternalStore(store.subscribe, store.getState);
}
