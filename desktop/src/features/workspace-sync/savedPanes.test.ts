import test from 'node:test';
import assert from 'node:assert/strict';
import { persistKey, savedPaneIds, writePersisted } from './windowStore.ts';

const tab = (id: string, extra: object = {}) => ({ id, kind: 'conversation', title: id, draft: '', pinned: false, ...extra });
const memory = (): Storage => {
  const map = new Map<string, string>();
  return { get length() { return map.size; }, key: i => [...map.keys()][i] ?? null, getItem: k => map.get(k) ?? null, setItem: (k, v) => { map.set(k, v); }, removeItem: k => { map.delete(k); }, clear: () => map.clear() };
};
const save = (storage: Storage, key: string, writer: string, tabs: object[], closed: object[] = []) =>
  writePersisted(storage, key as never, writer, { base: { schema: 1, tabs, groups: [], closed, nextNumber: 2 } as never, revision: 1, pending: [], local: { recentIds: [], focus: {}, scroll: {} } });

test('a window keeps the panes it saved under other places, including split panes and the closed ring, and never another window’s', () => {
  const storage = memory();
  save(storage, 'now', 'win-a', [tab('now-1')]);
  save(storage, 'pl_00000000000000aa', 'win-a', [tab('r1'), tab('s1', { split: { layout: '1x2', focus: 0, panes: [tab('s1a'), tab('s1b')] } })], [tab('gone')]);
  save(storage, 'pl_00000000000000aa', 'win-b', [tab('theirs')]);
  assert.deepEqual([...savedPaneIds(storage, 'win-a', 'now')].sort(), ['gone', 'r1', 's1', 's1a', 's1b']);
  assert.deepEqual(savedPaneIds(storage, 'win-a', 'pl_00000000000000aa' as never), ['now-1']);
  assert.equal(persistKey('now', 'win-a').endsWith(':win-a'), true);
});

test('an absent or unreadable store keeps nothing extra', () => {
  assert.deepEqual(savedPaneIds(undefined, 'win-a', 'now'), []);
  const storage = memory();
  storage.setItem(persistKey('pl_00000000000000aa' as never, 'win-a'), '{broken');
  assert.deepEqual(savedPaneIds(storage, 'win-a', 'now'), []);
});
