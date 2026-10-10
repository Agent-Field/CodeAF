import test from 'node:test';
import assert from 'node:assert/strict';
import { createPlacesSnapshotStore, EMPTY_PLACES } from './usePlaces.ts';

const node = (id: string) => ({ id, name: id }) as any;
const rail = (ids: string[]) => ({ pinned: ids.map(node), open: [], openWindowHours: 24 }) as any;

function fakeSource(graph?: any) {
  const listeners = new Set<() => void>();
  const calls: unknown[][] = [];
  const source: any = {
    state: { graph },
    getState: () => source.state,
    subscribe: (l: () => void) => { listeners.add(l); return () => listeners.delete(l); },
    client: { pinPlace: (...a: unknown[]) => { calls.push(a); return Promise.resolve('ok'); } },
  };
  return { source, calls, push: (g: any) => { source.state = { graph: g }; listeners.forEach(l => l()); } };
}

test('an empty store yields empty lists and no roll-ups', () => {
  const { source } = fakeSource();
  const store = createPlacesSnapshotStore(source);
  const off = store.subscribe(() => {});
  const s = store.getState();
  assert.equal(s, EMPTY_PLACES);
  assert.deepEqual([s.nodes, s.members, s.rail.pinned, s.rail.open], [[], [], [], []]);
  assert.equal(s.rollups, undefined);
  off();
});

test('an out-of-order generation is ignored', () => {
  const { source } = fakeSource();
  const store = createPlacesSnapshotStore(source);
  store.ingest({ generation: 5, nodes: [node('a')], rail: rail(['a']) });
  store.ingest({ generation: 3, nodes: [node('old')], rail: rail([]) });
  assert.equal(store.getState().generation, 5);
  assert.deepEqual(store.getState().nodes.map(n => n.id), ['a']);
});

test('a stale graph read cannot overwrite a newer world record, and a newer one lands with roll-ups', () => {
  const fake = fakeSource();
  const store = createPlacesSnapshotStore(fake.source);
  const off = store.subscribe(() => {});
  store.ingest({ generation: 7, nodes: [node('a')], rail: rail([]) });
  fake.push({ generation: 6, nodes: [node('b')], rail: rail([]), now: { chats: 1 }, totals: { places: 1 } });
  assert.deepEqual(store.getState().nodes.map(n => n.id), ['a']);
  fake.push({ generation: 8, nodes: [node('c')], rail: rail([]), now: { chats: 2 }, totals: { places: 2 } });
  assert.deepEqual(store.getState().nodes.map(n => n.id), ['c']);
  assert.deepEqual(store.getState().rollups?.now, { chats: 2 });
  off();
});

test('mutations pass straight to the client', async () => {
  const fake = fakeSource();
  const store = createPlacesSnapshotStore(fake.source);
  assert.equal(await store.mutations.pinPlace('a', 1), 'ok');
  assert.deepEqual(fake.calls, [['a', 1]]);
});
