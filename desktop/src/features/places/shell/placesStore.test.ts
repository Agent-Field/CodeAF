import assert from 'node:assert/strict';
import test from 'node:test';
import type { PlacesClient } from '../client.ts';
import { worldStore } from '../../chat/world-store.ts';
import { createPlacesStore } from './placesStore.ts';

const wait = () => new Promise(resolve => setTimeout(resolve, 20));

test('a world status change at the same cursor does not read the graph again', async () => {
  let seq = 0;
  const listeners = new Set<() => void>();
  let reads = 0;
  const client = { async graph() { reads += 1; return { places: [] }; } } as unknown as PlacesClient;
  const world = {
    getState: () => ({ status: 'connecting', seq, rows: [], items: [] }),
    subscribe(listener: () => void) { listeners.add(listener); return () => { listeners.delete(listener); }; },
  } as unknown as Pick<typeof worldStore, 'subscribe' | 'getState'>;
  const store = createPlacesStore({ client, world, settleMs: 0 });
  const stop = store.subscribe(() => {});
  await wait();
  assert.equal(reads, 1);
  for (const listener of listeners) listener();
  await wait();
  assert.equal(reads, 1, 'connecting, with the cursor unmoved, is not a new record');
  seq = 1;
  for (const listener of listeners) listener();
  await wait();
  assert.equal(reads, 2);
  stop();
});
