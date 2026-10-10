import assert from 'node:assert/strict';
import test from 'node:test';
import { createWindowPlaceStore, type WindowPlaceSource, type WindowPlaceStorage } from './windowPlace.ts';
import type { PlacesState } from './shell/placesStore.ts';
import { windowPlaceStorageKey } from '../../lib/native/windowPlace.ts';

const A = 'pl_0123456789abcdef';
const B = 'pl_fedcba9876543210';
function fixture() {
  const values = new Map<string, string>();
  const storage: WindowPlaceStorage = { getItem: key => values.get(key) ?? null, setItem: (key, value) => { values.set(key, value); } };
  let state: Pick<PlacesState, 'status' | 'graph'> = { status: 'loading' };
  const listeners = new Set<() => void>();
  const places: WindowPlaceSource = {
    getState: () => state,
    subscribe: listener => { listeners.add(listener); return () => { listeners.delete(listener); }; },
  };
  const graph = (ids: string[], archived: string[] = []) => {
    state = { status: 'ready', graph: { places: ids.map(id => ({ id, archived: archived.includes(id) })) } as PlacesState['graph'] };
    for (const listener of [...listeners]) listener();
  };
  const unavailable = (status: 'offline' | 'error') => {
    state = { status };
    for (const listener of [...listeners]) listener();
  };
  return { storage, places, values, listeners, graph, unavailable };
}

test('every window label restores its own saved place on relaunch', () => {
  const f = fixture();
  for (const label of ['main', 'w-2']) {
    const store = createWindowPlaceStore({ ...f, label });
    store.setPlace(A);
    assert.equal(createWindowPlaceStore({ ...f, label }).getState(), A);
  }
});

test('boot destination wins over a saved place and is remembered', () => {
  const f = fixture();
  f.values.set(windowPlaceStorageKey('w-1'), A);
  const store = createWindowPlaceStore({ ...f, label: 'w-1', initialPlaceKey: B });
  assert.equal(store.getState(), B);
  assert.equal(createWindowPlaceStore({ ...f, label: 'w-1' }).getState(), B);
});

test('Now and root survive graph reads and restore', () => {
  const f = fixture();
  f.graph([]);
  const store = createWindowPlaceStore({ ...f, label: 'main' });
  assert.equal(store.getState(), 'now');
  store.setPlace('root');
  assert.equal(createWindowPlaceStore({ ...f, label: 'main' }).getState(), 'root');
});

test('a missing or archived place falls back only after a successful read and persists Now', () => {
  for (const archived of [false, true]) {
    const f = fixture();
    const store = createWindowPlaceStore({ ...f, label: 'w-1', initialPlaceKey: A });
    const seen: string[] = [];
    const off = store.subscribe(() => seen.push(store.getState()));
    assert.equal(store.getState(), A);
    f.graph([A]);
    assert.equal(store.getState(), A);
    f.graph(archived ? [A] : [], archived ? [A] : []);
    assert.equal(store.getState(), 'now');
    assert.deepEqual(seen, ['now']);
    assert.equal(createWindowPlaceStore({ ...f, label: 'w-1' }).getState(), 'now');
    off();
  }
});

test('two labels share graph truth but navigate and persist independently', () => {
  const f = fixture();
  f.graph([A, B]);
  const first = createWindowPlaceStore({ ...f, label: 'w-1', initialPlaceKey: A });
  const second = createWindowPlaceStore({ ...f, label: 'w-2', initialPlaceKey: A });
  const changes: string[] = [];
  const off = second.subscribe(() => changes.push(second.getState()));
  first.setPlace(B);
  assert.equal(second.getState(), A);
  assert.deepEqual(changes, []);
  assert.equal(createWindowPlaceStore({ ...f, label: 'w-1' }).getState(), B);
  assert.equal(createWindowPlaceStore({ ...f, label: 'w-2' }).getState(), A);
  off();
});

test('subscriptions release the graph once and refresh on resubscribe', () => {
  const f = fixture();
  const store = createWindowPlaceStore({ ...f, label: 'main', initialPlaceKey: A });
  const off = store.subscribe(() => {});
  const off2 = store.subscribe(() => {});
  assert.equal(f.listeners.size, 1);
  off(); off();
  assert.equal(f.listeners.size, 1);
  off2();
  assert.equal(f.listeners.size, 0);
  f.graph([]);
  const off3 = store.subscribe(() => {});
  assert.equal(store.getState(), 'now');
  off3();
});

test('invalid saved keys and unavailable storage start safely; malformed navigation is refused', () => {
  const f = fixture();
  f.values.set(windowPlaceStorageKey('main'), '../private');
  assert.equal(createWindowPlaceStore({ ...f, label: 'main' }).getState(), 'now');
  const store = createWindowPlaceStore({ ...f, label: 'main', storage: {
    getItem: () => { throw new Error('disabled'); }, setItem: () => { throw new Error('full'); },
  } });
  store.setPlace(A);
  assert.equal(store.getState(), A);
  assert.throws(() => store.setPlace('bad' as never));
  assert.equal(store.getState(), A);
});


test('failed graph reads keep the current place until a successful read proves it missing', () => {
  const f = fixture();
  const store = createWindowPlaceStore({ ...f, label: 'main', initialPlaceKey: A });
  const off = store.subscribe(() => {});
  f.unavailable('offline');
  assert.equal(store.getState(), A);
  f.unavailable('error');
  assert.equal(store.getState(), A);
  f.graph([]);
  assert.equal(store.getState(), 'now');
  off();
});
