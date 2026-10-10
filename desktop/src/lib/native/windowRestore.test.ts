import assert from 'node:assert/strict';
import { test } from 'node:test';
import {
  WINDOWS_STORE_KEY, MAX_RESTORED_WINDOWS, reconcileWindows, windowsToRestore, recordWindows, restoreWindows,
  localWindowListStore, type WindowListStore,
} from './windowRestore.ts';

const P1 = 'p-0123456789ab';
const P2 = 'p-ba9876543210';

function memory(initial?: unknown): WindowListStore & { data: Map<string, unknown> } {
  const data = new Map<string, unknown>();
  if (initial !== undefined) data.set(WINDOWS_STORE_KEY, initial);
  return { data, read: (k) => data.get(k), write: (k, v) => void data.set(k, v) };
}

test('reconcile drops malformed rows, unknown places and duplicate labels', () => {
  const rows = reconcileWindows([
    { label: 'main', placeKey: 'now' },
    { label: 'w-2', placeKey: P1 },
    { label: 'w-2', placeKey: P2 },
    { label: 'w-3', placeKey: 'nonsense' },
    { label: 'evil', placeKey: P1 },
    null, 7, { label: 'w-4' },
  ]);
  assert.deepEqual(rows, [{ label: 'main', placeKey: 'now' }, { label: 'w-2', placeKey: P1 }]);
});

test('reconcile of a non-list is empty and the list is capped', () => {
  assert.deepEqual(reconcileWindows('x'), []);
  const many = Array.from({ length: 40 }, (_, i) => ({ label: `w-${i}`, placeKey: 'now' }));
  assert.equal(reconcileWindows(many).length, MAX_RESTORED_WINDOWS);
});

test('restore skips main and windows already open', () => {
  const saved = [{ label: 'main', placeKey: P1 }, { label: 'w-2', placeKey: P1 }, { label: 'w-5', placeKey: 'now' }];
  assert.deepEqual(windowsToRestore(saved, ['main', 'w-5']), [{ label: 'w-2', placeKey: P1 }]);
});

test('relaunch reopens the other windows on their places, surviving one failure', async () => {
  const store = memory([{ label: 'main', placeKey: 'now' }, { label: 'w-2', placeKey: P1 }, { label: 'w-3', placeKey: P2 }, { label: 'w-4', placeKey: 'now' }]);
  const calls: string[] = [];
  const opened = await restoreWindows({
    store,
    open: async (key) => { calls.push(key); if (key === P2) throw new Error('no'); return 'w-9'; },
  });
  assert.deepEqual(calls, [P1, P2, 'now']);
  assert.deepEqual(opened.map((r) => r.label), ['w-2', 'w-4']);
});

test('record stores the cleaned list under "windows", empty included', () => {
  const store = memory();
  recordWindows(store, [{ label: 'main', placeKey: 'now' }, { label: 'w-2', placeKey: 'bad' }]);
  assert.deepEqual(store.data.get('windows'), [{ label: 'main', placeKey: 'now' }]);
  recordWindows(store, []);
  assert.deepEqual(store.data.get('windows'), []);
});

test('local store round-trips and ignores corrupt text', () => {
  const m = new Map<string, string>();
  const storage = { getItem: (k: string) => m.get(k) ?? null, setItem: (k: string, v: string) => void m.set(k, v) };
  const store = localWindowListStore(storage);
  store.write('windows', [{ label: 'main', placeKey: 'now' }]);
  assert.deepEqual(store.read('windows'), [{ label: 'main', placeKey: 'now' }]);
  m.set('codeaf.desktop.workspace.windows', '{oops');
  assert.equal(store.read('windows'), undefined);
});
