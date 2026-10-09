import test from 'node:test';
import assert from 'node:assert/strict';
import { createPreviewStore } from './previewStore.ts';

test('a card swaps in only when a neighbour was already open', () => {
  const store = createPreviewStore(10);
  assert.equal(store.isWarm(), false);
  store.open('a');
  assert.deepEqual(store.get(), { id: 'a', swapped: false });
  store.open('b');
  assert.deepEqual(store.get(), { id: 'b', swapped: true });
  store.close('a');
  assert.equal(store.get()?.id, 'b');
  store.close('b');
  assert.equal(store.isWarm(), false);
});

test('two workspaces never share warm state', () => {
  const first = createPreviewStore(10);
  const second = createPreviewStore(10);
  first.open('a');
  assert.equal(second.isWarm(), false);
  second.open('a');
  assert.equal(second.get()?.swapped, false);
  first.dispose();
  assert.equal(first.isWarm(), false);
  assert.equal(second.isWarm(), true);
});
