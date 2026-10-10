// The place graph as the rest of the desktop reads it: nodes, memberships, the rail and the roll-ups, in one snapshot.
//
// Two things feed it and neither may move it backwards: the graph the shell store reads from the Places routes, and
// `places` records when the world stream carries them. Both go through applyPlacesRecord, so an out-of-order
// generation is dropped in one place. The snapshot starts empty and stays empty until the engine says otherwise
// (the emptiness law): no list is ever filled in to look populated.

import { useSyncExternalStore } from 'react';
import { applyPlacesRecord } from './wire.ts';
import type { Membership, NowView, PlaceNode, PlacesGraph, PlacesRecord, PlacesTotals, Rail } from './wire.ts';
import { placesStore, type PlacesStore } from './shell/placesStore.ts';

export type PlacesSnapshot = {
  generation: number;
  nodes: PlaceNode[];
  members: Membership[];
  rail: Rail;
  /** Roll-ups from the last full read; absent until the engine has answered once. */
  rollups?: { now: NowView; totals: PlacesTotals };
};

const EMPTY_RAIL: Rail = { pinned: [], open: [], openWindowHours: 0 };
export const EMPTY_PLACES: PlacesSnapshot = { generation: 0, nodes: [], members: [], rail: EMPTY_RAIL };

/** The writes a surface may ask for, passed straight to the client: it owns generations, validation and refusals. */
const MUTATIONS = ['createPlace', 'updatePlace', 'setParents', 'archivePlace', 'restorePlace', 'deletePlace', 'mergePlace',
  'pinPlace', 'unpinPlace', 'addChats', 'removeChats', 'railOp'] as const;
export type PlacesMutations = Pick<PlacesStore['client'], typeof MUTATIONS[number]>;

type Source = Pick<PlacesStore, 'subscribe' | 'getState' | 'client'>;

export function createPlacesSnapshotStore(source: Source = placesStore) {
  let snapshot = EMPTY_PLACES;
  const listeners = new Set<() => void>();
  let unsource: (() => void) | undefined;
  let lastGraph: PlacesGraph | undefined;

  const set = (next: PlacesSnapshot) => { if (next !== snapshot) { snapshot = next; for (const l of [...listeners]) l(); } };

  /** A places record from the world stream. A generation older than the snapshot's changes nothing. */
  const ingest = (record: PlacesRecord) => set(applyPlacesRecord(snapshot, record));

  const onSource = () => {
    const graph = source.getState().graph;
    if (!graph || graph === lastGraph) return;
    lastGraph = graph;
    const next = applyPlacesRecord(snapshot, { generation: graph.generation, nodes: graph.nodes ?? [], rail: graph.rail });
    set({ ...next, rollups: { now: graph.now, totals: graph.totals } });
  };

  const mutations = Object.fromEntries(MUTATIONS.map(name => [name, (...args: unknown[]) => (source.client[name] as (...a: unknown[]) => unknown)(...args)])) as PlacesMutations;

  return {
    getState: () => snapshot,
    ingest,
    mutations,
    subscribe(listener: () => void): () => void {
      listeners.add(listener);
      if (listeners.size === 1) { unsource = source.subscribe(onSource); onSource(); }
      return () => {
        listeners.delete(listener);
        if (listeners.size === 0) { unsource?.(); unsource = undefined; }
      };
    },
  };
}

export type PlacesSnapshotStore = ReturnType<typeof createPlacesSnapshotStore>;
export const placesSnapshotStore = createPlacesSnapshotStore();

/** The latest places snapshot, and the thin write passthroughs. */
export function usePlaces(store: PlacesSnapshotStore = placesSnapshotStore) {
  const places = useSyncExternalStore(store.subscribe, store.getState);
  return { ...places, mutations: store.mutations };
}
